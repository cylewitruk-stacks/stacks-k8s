package participantworkload

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// configuration captures seed inputs and resolves immutable private configuration.
func (r *Reconciler) configuration(
	ctx context.Context,
	root *api.StacksNetwork,
	p *api.StacksNetworkParticipant,
	state *api.ParticipantRuntimeStatus,
) (bool, error) {
	policy := p.Status.Admission.PolicyDigest
	configPurpose := "config-" + strings.TrimPrefix(policy, "sha256:")
	configPin := state.ConfigRef
	if state.PolicyDigest != policy {
		configPin = nil
	}
	for _, item := range []struct {
		purpose     string
		pin         *common.Binding
		destination **common.Binding
	}{
		{"rpc-control", state.RPCSecretRef, &state.RPCSecretRef},
		{"rpc-actor", state.ActorRPCSecretRef, &state.ActorRPCSecretRef},
		{configPurpose, configPin, &state.ConfigRef},
	} {
		ref, err := r.emptySecret(ctx, p, item.purpose, item.pin)
		if err != nil {
			return false, err
		}
		*item.destination = ref
	}
	if observerEnabled(p) {
		ref, err := r.emptySecret(ctx, p, "rpc-observer", state.ObserverRPCSecretRef)
		if err != nil {
			return false, err
		}
		state.ObserverRPCSecretRef = ref
	}
	state.PolicyDigest = policy
	report := &corev1.ConfigMap{
		ObjectMeta: objectMeta(p, "report-"+strings.TrimPrefix(policy, "sha256:"), api.RoleSupport),
	}
	if err := r.createOwned(ctx, p, report); err != nil {
		return false, err
	}
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(report), report); err != nil {
		return false, err
	}
	in := BitcoinConfigInput{
		Namespace:          p.Namespace,
		ParticipantUID:     p.UID,
		PolicyDigest:       policy,
		Config:             *state.ConfigRef,
		ControlCredentials: *state.RPCSecretRef,
		ActorCredentials:   *state.ActorRPCSecretRef,
		Report:             objectref.ConfigMap(report),
	}
	if observerEnabled(p) {
		in.ObserverCredentials = state.ObserverRPCSecretRef
	}
	in.Customization = p.Status.Admission.Configuration.BitcoinNode.Config
	if in.Customization != nil {
		if ref := in.Customization.SecretRef; ref != nil {
			pin, ok := admittedBinding(p, common.KindSecret, ref.Name)
			if !ok {
				return false, fmt.Errorf("custom configuration Secret was not admitted")
			}
			if err := r.privateMetadata(ctx, p.Namespace, pin, ""); err != nil {
				return false, err
			}
			in.Custom = &PrivateInput{Binding: pin, Key: ref.Key}
		}
		var err error
		in.Services, err = r.customServiceHosts(ctx, root, p, in.Customization.ServiceRefs)
		if err != nil {
			return false, err
		}
	}
	if raw := report.Data["input.json"]; raw != "" {
		var captured BitcoinConfigInput
		if len(raw) > 1024*1024 || json.Unmarshal([]byte(raw), &captured) != nil {
			return false, fmt.Errorf("invalid resolver input snapshot")
		}
		in.Seeds = captured.Seeds
		if digest(in) != digest(captured) {
			return false, fmt.Errorf("resolver input identity changed")
		}
	} else {
		seeds, err := peerSeeds(root, p)
		if err != nil {
			return false, err
		}
		in.Seeds = seeds
		data, _ := json.Marshal(in)
		base := report.DeepCopy()
		report.Data = map[string]string{"input.json": string(data)}
		if err := r.Client.Patch(
			ctx,
			report,
			client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}),
		); err != nil {
			return false, err
		}
	}
	if raw := report.Data["report.json"]; raw != "" {
		var result BitcoinConfigReport
		if len(raw) > 4096 || json.Unmarshal([]byte(raw), &result) != nil || result.InputDigest != digest(in) ||
			!strings.HasPrefix(result.ConfigDigest, "sha256:") {
			return false, fmt.Errorf("invalid Bitcoin configuration report")
		}
		if !result.Verified && in.Customization != nil &&
			ptr.Deref(in.Customization.Compatibility, common.CompatibilityManaged) != common.CompatibilityUnverified {
			return false, fmt.Errorf("missing Bitcoin configuration agreement")
		}
		state.ConfigurationDigest = digest(in)
		state.ConfigRef.Fingerprint = result.ConfigDigest
		return true, nil
	}
	return false, r.provisionResolver(ctx, p, in)
}

// emptySecret inspects metadata only; private data is generated and read in the Job.
func (r *Reconciler) emptySecret(
	ctx context.Context,
	p *api.StacksNetworkParticipant,
	purpose string,
	pin *common.Binding,
) (*common.Binding, error) {
	metadata := &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: common.KindSecret},
	}
	key := client.ObjectKey{Namespace: p.Namespace, Name: Name(p, purpose)}
	err := r.Reader.Get(ctx, key, metadata)
	if apierrors.IsNotFound(err) && pin == nil {
		empty := &corev1.Secret{ObjectMeta: objectMeta(p, purpose, api.RoleSupport)}
		if err := r.Client.Create(ctx, empty); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		err = r.Reader.Get(ctx, key, metadata)
	}
	if err != nil {
		return nil, err
	}
	if !owned(metadata, p) || metadata.DeletionTimestamp != nil || metadata.UID == "" {
		return nil, fmt.Errorf("RPC/config Secret ownership unavailable")
	}
	if pin != nil && (pin.Name != metadata.Name || pin.UID != metadata.UID) {
		return nil, fmt.Errorf("RPC/config Secret identity changed")
	}
	ref, err := objectref.SecretMetadata(metadata)
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

// peerSeeds resolves allocated peer identities without readiness cycles.
func peerSeeds(root *api.StacksNetwork, p *api.StacksNetworkParticipant) ([]string, error) {
	node := p.Status.Admission.Configuration.BitcoinNode
	selected := map[string]api.InstanceIdentity{}
	for _, entry := range root.Spec.Participants {
		if entry.Kind != api.ParticipantBitcoinNode || entry.Name == p.Spec.ParticipantName {
			continue
		}
		for _, identity := range root.Status.Identities {
			if identity.Name == entry.Name && !identity.Removing && identity.UID != "" {
				selected[entry.Name] = identity
			}
		}
	}
	names := make([]string, 0, len(selected))
	if node.Peers != nil && node.Peers.NodeRefs != nil {
		for _, ref := range *node.Peers.NodeRefs {
			if ref.Name == p.Spec.ParticipantName {
				continue
			}
			if _, ok := selected[ref.Name]; !ok {
				return nil, fmt.Errorf("peer %s is not allocated", ref.Name)
			}
			names = append(names, ref.Name)
		}
	} else {
		for name := range selected {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	seeds := make([]string, 0, len(names))
	for _, name := range names {
		seeds = append(
			seeds,
			foundation.RuntimeName(
				string(root.UID),
				string(selected[name].UID),
				string(api.ParticipantBitcoinNode),
				name,
				common.EndpointP2P,
			)+"."+root.Namespace+".svc",
		)
	}
	return seeds, nil
}
