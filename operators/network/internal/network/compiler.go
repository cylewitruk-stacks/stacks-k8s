// Package network compiles aggregate topology into focused leaf resources.
package network

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	operatorlabels "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/labels"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
)

const defaultDependencyImage = "busybox:1.36.1@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662"

// DesiredTopology is the deterministic set of leaf objects for one network.
type DesiredTopology struct {
	BitcoinNodes []networkv1alpha1.BitcoinNode
	StacksNodes  []networkv1alpha1.StacksNode
	Signers      []networkv1alpha1.StacksSigner
}

// Objects returns every desired child in stable kind/name order.
func (d DesiredTopology) Objects() []object {
	result := make([]object, 0, len(d.BitcoinNodes)+len(d.StacksNodes)+len(d.Signers))
	for index := range d.BitcoinNodes {
		result = append(result, object{kind: "BitcoinNode", name: d.BitcoinNodes[index].Name, value: &d.BitcoinNodes[index]})
	}
	for index := range d.StacksNodes {
		result = append(result, object{kind: "StacksNode", name: d.StacksNodes[index].Name, value: &d.StacksNodes[index]})
	}
	for index := range d.Signers {
		result = append(result, object{kind: "StacksSigner", name: d.Signers[index].Name, value: &d.Signers[index]})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].kind != result[j].kind {
			return result[i].kind < result[j].kind
		}
		return result[i].name < result[j].name
	})
	return result
}

type object struct {
	kind  string
	name  string
	value any
}

// Compile validates and expands one aggregate network into leaf CRs.
func Compile(network *networkv1alpha1.StacksNetwork) (DesiredTopology, error) {
	if network == nil {
		return DesiredTopology{}, fmt.Errorf("StacksNetwork is required")
	}
	if errors := validation.IsDNS1123Label(network.Name); len(errors) > 0 {
		return DesiredTopology{}, fmt.Errorf("StacksNetwork name %q must be a DNS label: %s", network.Name, strings.Join(errors, "; "))
	}
	if err := validateDefaults(network.Spec.Defaults); err != nil {
		return DesiredTopology{}, err
	}
	bitcoinTemplates := sortedCopy(network.Spec.BitcoinNodes, func(value networkv1alpha1.BitcoinNodeTemplate) string { return value.Name })
	stacksTemplates := sortedCopy(network.Spec.StacksNodes, func(value networkv1alpha1.StacksNodeTemplate) string { return value.Name })
	signerTemplates := sortedCopy(network.Spec.Signers, func(value networkv1alpha1.StacksSignerTemplate) string { return value.Name })
	services := map[string]string{}
	kinds := map[string]string{}
	register := func(name, kind string) error {
		if previous, exists := kinds[name]; exists {
			return fmt.Errorf("actor %q is declared as both %s and %s", name, previous, kind)
		}
		kinds[name] = kind
		services[name] = naming.Child(network.Name, name)
		return nil
	}
	for _, actor := range bitcoinTemplates {
		if err := register(actor.Name, "BitcoinNode"); err != nil {
			return DesiredTopology{}, err
		}
	}
	for _, actor := range stacksTemplates {
		if err := register(actor.Name, "StacksNode"); err != nil {
			return DesiredTopology{}, err
		}
	}
	for _, actor := range signerTemplates {
		if err := register(actor.Name, "StacksSigner"); err != nil {
			return DesiredTopology{}, err
		}
	}

	bitcoin := map[string]networkv1alpha1.BitcoinNodeTemplate{}
	for _, actor := range bitcoinTemplates {
		bitcoin[actor.Name] = actor
	}
	stacks := map[string]networkv1alpha1.StacksNodeTemplate{}
	for _, actor := range stacksTemplates {
		stacks[actor.Name] = actor
	}
	signerByNode := map[string]networkv1alpha1.StacksSignerTemplate{}
	signerByIndex := map[int32]string{}
	for _, signer := range signerTemplates {
		node, exists := stacks[signer.NodeRef]
		if !exists || node.Role != networkv1alpha1.StacksNodeSigner {
			return DesiredTopology{}, fmt.Errorf("signer %q references missing or non-signer Stacks node %q", signer.Name, signer.NodeRef)
		}
		if previous, exists := signerByNode[signer.NodeRef]; exists {
			return DesiredTopology{}, fmt.Errorf("signers %q and %q reference the same Stacks node %q", previous.Name, signer.Name, signer.NodeRef)
		}
		if err := validateConfig(signer.Name, "signer", signer.Config); err != nil {
			return DesiredTopology{}, err
		}
		if signer.Config.SecretRef == nil {
			return DesiredTopology{}, fmt.Errorf("signer %q configuration must use a Secret reference", signer.Name)
		}
		if signer.Weight < 1 || signer.Weight > 9_007_199_254_740_991 {
			return DesiredTopology{}, fmt.Errorf("signer %q weight must be within the JSON safe positive-integer range", signer.Name)
		}
		if previous, exists := signerByIndex[signer.Index]; exists {
			return DesiredTopology{}, fmt.Errorf("signers %q and %q use the same index %d", previous, signer.Name, signer.Index)
		}
		signerByNode[signer.NodeRef] = signer
		signerByIndex[signer.Index] = signer.Name
	}

	pull := network.Spec.Defaults.ImagePullPolicy
	if pull == "" {
		pull = corev1.PullIfNotPresent
	}
	dependencyImage := network.Spec.Defaults.DependencyImage
	if dependencyImage == "" {
		dependencyImage = defaultDependencyImage
	}
	result := DesiredTopology{}
	for _, actor := range bitcoinTemplates {
		image, err := resolveImage(actor.Name, "BitcoinNode", actor.Image, network.Spec.Defaults.BitcoinImage)
		if err != nil {
			return DesiredTopology{}, err
		}
		if err := validateConfig(actor.Name, "bitcoin", actor.Config); err != nil {
			return DesiredTopology{}, err
		}
		for _, peer := range actor.PeerRefs {
			if peer == actor.Name {
				return DesiredTopology{}, fmt.Errorf("Bitcoin actor %q cannot peer with itself", actor.Name)
			}
			if _, exists := bitcoin[peer]; !exists {
				return DesiredTopology{}, fmt.Errorf("Bitcoin actor %q references unknown peer %q", actor.Name, peer)
			}
		}
		peers := make([]string, len(actor.PeerRefs))
		for index, peer := range actor.PeerRefs {
			peers[index] = services[peer]
		}
		sort.Strings(peers)
		result.BitcoinNodes = append(result.BitcoinNodes, networkv1alpha1.BitcoinNode{
			TypeMeta:   metav1.TypeMeta{APIVersion: networkv1alpha1.GroupVersion.String(), Kind: "BitcoinNode"},
			ObjectMeta: metav1.ObjectMeta{Name: services[actor.Name], Namespace: network.Namespace, Labels: operatorlabels.ForActor(network.Name, actor.Name, operatorlabels.Bitcoin)},
			Spec: networkv1alpha1.BitcoinNodeSpec{NetworkRef: ref(network.Name), ActorName: actor.Name, Role: actor.Role,
				Image: image, ImagePullPolicy: pull,
				ImagePullSecrets: references(network.Spec.Defaults.ImagePullSecrets), Config: actor.Config, PeerRefs: peers,
				RPCPort: 18443, P2PPort: 18444, DependencyImage: dependencyImage, Workload: mergeWorkload(network.Spec.Defaults.Workload, actor.Workload), Container: actor.Container,
				Suspended: network.Spec.Suspended || actor.Suspended},
		})
	}
	for _, actor := range stacksTemplates {
		image, err := resolveImage(actor.Name, "StacksNode", actor.Image, network.Spec.Defaults.StacksNodeImage)
		if err != nil {
			return DesiredTopology{}, err
		}
		if _, exists := bitcoin[actor.BitcoinNodeRef]; !exists {
			return DesiredTopology{}, fmt.Errorf("Stacks actor %q references unknown Bitcoin node %q", actor.Name, actor.BitcoinNodeRef)
		}
		if actor.Role == networkv1alpha1.StacksNodeMiner && actor.Config.SecretRef == nil {
			return DesiredTopology{}, fmt.Errorf("miner %q configuration must use a Secret reference", actor.Name)
		}
		if err := validateConfig(actor.Name, "stacks", actor.Config); err != nil {
			return DesiredTopology{}, err
		}
		requiredServices := []string{actor.BitcoinNodeRef}
		if signer, exists := signerByNode[actor.Name]; exists {
			requiredServices = append(requiredServices, signer.Name)
		}
		serviceMap, err := serviceBindings(services, requiredServices, actor.ServiceRefs)
		if err != nil {
			return DesiredTopology{}, fmt.Errorf("Stacks actor %q: %w", actor.Name, err)
		}
		spec := networkv1alpha1.StacksNodeSpec{NetworkRef: ref(network.Name), ActorName: actor.Name, Role: actor.Role,
			Image: image, ImagePullPolicy: pull,
			ImagePullSecrets: references(network.Spec.Defaults.ImagePullSecrets), BitcoinNodeRef: ref(services[actor.BitcoinNodeRef]),
			Config: actor.Config, ServiceMap: serviceMap, Genesis: network.Spec.Genesis, DependencyImage: dependencyImage,
			Workload: mergeWorkload(network.Spec.Defaults.Workload, actor.Workload), Container: actor.Container,
			Suspended: network.Spec.Suspended || actor.Suspended}
		if signer, exists := signerByNode[actor.Name]; exists {
			spec.SignerRef = pointer(ref(services[signer.Name]))
			spec.SignerIndex = pointer(signer.Index)
		}
		result.StacksNodes = append(result.StacksNodes, networkv1alpha1.StacksNode{
			TypeMeta:   metav1.TypeMeta{APIVersion: networkv1alpha1.GroupVersion.String(), Kind: "StacksNode"},
			ObjectMeta: metav1.ObjectMeta{Name: services[actor.Name], Namespace: network.Namespace, Labels: operatorlabels.ForActor(network.Name, actor.Name, operatorlabels.StacksNode)},
			Spec:       spec,
		})
	}
	for _, actor := range signerTemplates {
		image, err := resolveImage(actor.Name, "StacksSigner", actor.Image, network.Spec.Defaults.StacksSignerImage)
		if err != nil {
			return DesiredTopology{}, err
		}
		serviceMap, err := serviceBindings(services, []string{actor.NodeRef}, actor.ServiceRefs)
		if err != nil {
			return DesiredTopology{}, fmt.Errorf("signer %q: %w", actor.Name, err)
		}
		result.Signers = append(result.Signers, networkv1alpha1.StacksSigner{
			TypeMeta:   metav1.TypeMeta{APIVersion: networkv1alpha1.GroupVersion.String(), Kind: "StacksSigner"},
			ObjectMeta: metav1.ObjectMeta{Name: services[actor.Name], Namespace: network.Namespace, Labels: operatorlabels.ForActor(network.Name, actor.Name, operatorlabels.StacksSigner)},
			Spec: networkv1alpha1.StacksSignerSpec{NetworkRef: ref(network.Name), ActorName: actor.Name,
				Image: image, ImagePullPolicy: pull,
				ImagePullSecrets: references(network.Spec.Defaults.ImagePullSecrets), NodeRef: ref(services[actor.NodeRef]), Index: actor.Index, Weight: actor.Weight,
				PublicKey: actor.PublicKey, Config: actor.Config, ServiceMap: serviceMap, DependencyImage: dependencyImage,
				Workload: mergeWorkload(network.Spec.Defaults.Workload, actor.Workload), Container: actor.Container,
				Suspended: network.Spec.Suspended || actor.Suspended},
		})
	}
	return result, nil
}

func serviceBindings(services map[string]string, required, explicit []string) (map[string]string, error) {
	names := make(map[string]struct{}, len(required)+len(explicit))
	for _, name := range append(append([]string(nil), required...), explicit...) {
		if _, exists := services[name]; !exists {
			return nil, fmt.Errorf("service reference %q does not name a declared actor", name)
		}
		names[name] = struct{}{}
	}
	bindings := make(map[string]string, len(names))
	for name := range names {
		bindings[name] = services[name]
	}
	return bindings, nil
}

func validateDefaults(defaults networkv1alpha1.NetworkDefaults) error {
	switch defaults.ImagePullPolicy {
	case "", corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever:
		return nil
	default:
		return fmt.Errorf("unsupported imagePullPolicy %q", defaults.ImagePullPolicy)
	}
}

func resolveImage(actor, kind, override, fallback string) (string, error) {
	image := valueOr(override, fallback)
	if image == "" {
		return "", fmt.Errorf("%s %q requires an image or its corresponding spec.defaults image", kind, actor)
	}
	return image, nil
}

func validateConfig(actor, role string, source networkv1alpha1.ConfigSource) error {
	count := 0
	if source.Generated != nil {
		count++
	}
	if source.Inline != nil {
		count++
	}
	if source.ConfigMapRef != nil {
		count++
	}
	if source.SecretRef != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("actor %q requires exactly one configuration source", actor)
	}
	if source.Generated != nil {
		var expected string
		switch role {
		case "bitcoin":
			expected = "bitcoin-regtest/v1"
		case "stacks":
			expected = "nakamoto-regtest-node/v1"
		case "signer":
			return fmt.Errorf("actor %q cannot use generated signer configuration", actor)
		default:
			return fmt.Errorf("actor %q has unknown configuration role %q", actor, role)
		}
		if source.Generated.Profile != expected {
			return fmt.Errorf("actor %q profile %q is invalid for %s", actor, source.Generated.Profile, role)
		}
	}
	return nil
}

func ref(name string) networkv1alpha1.LocalObjectReference {
	return networkv1alpha1.LocalObjectReference{Name: name}
}
func pointer[T any](value T) *T { return &value }
func valueOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
func references(values []corev1.LocalObjectReference) []networkv1alpha1.LocalObjectReference {
	result := make([]networkv1alpha1.LocalObjectReference, len(values))
	for index, value := range values {
		result[index].Name = value.Name
	}
	return result
}
func copyMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
func mergeWorkload(base networkv1alpha1.WorkloadSpec, override *networkv1alpha1.WorkloadSpec) networkv1alpha1.WorkloadSpec {
	result := base
	if override == nil {
		return result
	}
	if override.Storage != nil {
		result.Storage = override.Storage
	}
	if override.Resources != nil {
		result.Resources = override.Resources
	}
	if override.NodeSelector != nil {
		result.NodeSelector = override.NodeSelector
	}
	if override.Tolerations != nil {
		result.Tolerations = override.Tolerations
	}
	if override.TerminationGracePeriodSeconds != nil {
		result.TerminationGracePeriodSeconds = override.TerminationGracePeriodSeconds
	}
	return result
}

func sortedCopy[T any](values []T, name func(T) string) []T {
	result := append([]T(nil), values...)
	sort.Slice(result, func(i, j int) bool { return name(result[i]) < name(result[j]) })
	return result
}
