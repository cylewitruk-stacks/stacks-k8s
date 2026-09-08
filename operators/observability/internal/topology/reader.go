// Package topology verifies admitted topology identity through direct API reads.
package topology

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	observationv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/canonical"
)

// AddNetworkTypes registers the unstructured topology types needed for cached watches.
func AddNetworkTypes(scheme *runtime.Scheme) {
	scheme.AddKnownTypeWithName(networkGVK, &unstructured.Unstructured{})
	listGVK := networkGVK
	listGVK.Kind = "StacksNetworkList"
	scheme.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
}

// NetworkObject returns a typed watch target without coupling the operator Go modules.
func NetworkObject() client.Object {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(networkGVK)
	return object
}

const (
	managedByLabel         = "app.kubernetes.io/managed-by"
	networkLabel           = "network.stacks.org/network"
	actorLabel             = "network.stacks.org/actor"
	actorKindLabel         = "network.stacks.org/actor-kind"
	actorResourceLabel     = "network.stacks.org/actor-resource"
	configDigestAnnotation = "network.stacks.org/config-digest"
)

var (
	networkGVK    = schema.GroupVersionKind{Group: "network.stacks.org", Version: "v1alpha1", Kind: "StacksNetwork"}
	leafResources = []struct {
		kind string
		gvk  schema.GroupVersionKind
	}{
		{kind: "BitcoinNode", gvk: schema.GroupVersionKind{Group: "network.stacks.org", Version: "v1alpha1", Kind: "BitcoinNodeList"}},
		{kind: "StacksNode", gvk: schema.GroupVersionKind{Group: "network.stacks.org", Version: "v1alpha1", Kind: "StacksNodeList"}},
		{kind: "StacksSigner", gvk: schema.GroupVersionKind{Group: "network.stacks.org", Version: "v1alpha1", Kind: "StacksSignerList"}},
	}
)

// NotReadyError means the topology has not published a complete inventory yet.
type NotReadyError struct{ Reason string }

func (e *NotReadyError) Error() string { return e.Reason }

// InconclusiveError means identity could not be established safely.
type InconclusiveError struct{ Reason string }

func (e *InconclusiveError) Error() string { return e.Reason }

// IsNotReady reports whether an observation should remain pending.
func IsNotReady(err error) bool {
	var target *NotReadyError
	return errors.As(err, &target)
}

// IsInconclusive reports whether an observation should terminate without a claim.
func IsInconclusive(err error) bool {
	var target *InconclusiveError
	return errors.As(err, &target)
}

// Snapshot is a verified topology identity observation.
type Snapshot struct {
	Binding observationv1alpha1.NetworkBinding
	Actors  []observationv1alpha1.ObservedActorIdentity
}

// Reader verifies topology status against live Kubernetes objects.
type Reader struct{ APIReader client.Reader }

// Observe reads and verifies one admitted inventory without using an informer cache.
func (r Reader) Observe(ctx context.Context, namespace, name, expectedDigest string) (Snapshot, error) {
	if r.APIReader == nil {
		return Snapshot{}, fmt.Errorf("topology reader requires an uncached API reader")
	}
	before, actors, err := r.readNetwork(ctx, namespace, name)
	if err != nil {
		return Snapshot{}, err
	}
	if expectedDigest != "" && before.InventoryDigest != expectedDigest {
		return Snapshot{}, &InconclusiveError{Reason: fmt.Sprintf("expected inventory %s, observed %s", expectedDigest, before.InventoryDigest)}
	}
	resources, err := r.readResources(ctx, namespace, before)
	if err != nil {
		return Snapshot{}, err
	}
	if resources.countsDifferFrom(len(actors)) {
		return Snapshot{}, &InconclusiveError{Reason: "live leaf, Service, StatefulSet, and Pod sets do not match the admitted inventory"}
	}
	verified := make([]observationv1alpha1.ObservedActorIdentity, 0, len(actors))
	for _, actor := range actors {
		if err := verifyActor(before, actor, resources); err != nil {
			return Snapshot{}, &InconclusiveError{Reason: err.Error()}
		}
		verified = append(verified, actor)
	}
	recomputedDigest, err := inventoryDigest(before.ObservedGeneration, verified)
	if err != nil {
		return Snapshot{}, &InconclusiveError{Reason: fmt.Sprintf("recompute admitted inventory digest: %v", err)}
	}
	if recomputedDigest != before.InventoryDigest {
		return Snapshot{}, &InconclusiveError{Reason: fmt.Sprintf("admitted inventory digest %s does not match verified live inventory %s", before.InventoryDigest, recomputedDigest)}
	}
	after, _, err := r.readNetwork(ctx, namespace, name)
	if err != nil {
		return Snapshot{}, &InconclusiveError{Reason: fmt.Sprintf("topology changed during observation: %v", err)}
	}
	if before != after {
		return Snapshot{}, &InconclusiveError{Reason: "topology UID, generation, or inventory digest changed during observation"}
	}
	return Snapshot{Binding: before, Actors: verified}, nil
}

type resourceView struct {
	leaves       map[string]*unstructured.Unstructured
	services     map[string]*corev1.Service
	statefulSets map[string]*appsv1.StatefulSet
	pods         map[string]*corev1.Pod
}

func (v resourceView) countsDifferFrom(expected int) bool {
	return len(v.leaves) != expected || len(v.services) != expected || len(v.statefulSets) != expected || len(v.pods) != expected
}

func (r Reader) readResources(ctx context.Context, namespace string, binding observationv1alpha1.NetworkBinding) (resourceView, error) {
	view := resourceView{leaves: map[string]*unstructured.Unstructured{}, services: map[string]*corev1.Service{}, statefulSets: map[string]*appsv1.StatefulSet{}, pods: map[string]*corev1.Pod{}}
	leafOwners := make(map[types.UID]string)
	for _, resource := range leafResources {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(resource.gvk)
		if err := r.APIReader.List(ctx, list, client.InNamespace(namespace)); err != nil {
			return resourceView{}, fmt.Errorf("list admitted %s resources: %w", resource.kind, err)
		}
		for index := range list.Items {
			leaf := &list.Items[index]
			if !controlledBy(leaf, "StacksNetwork", binding.UID) {
				continue
			}
			view.leaves[resource.kind+"\x00"+leaf.GetName()] = leaf
			leafOwners[leaf.GetUID()] = resource.kind
		}
	}
	serviceList := &corev1.ServiceList{}
	if err := r.APIReader.List(ctx, serviceList, client.InNamespace(namespace)); err != nil {
		return resourceView{}, fmt.Errorf("list admitted Services: %w", err)
	}
	for index := range serviceList.Items {
		service := &serviceList.Items[index]
		if owner := controllerOwner(service); owner != nil && leafOwners[owner.UID] == owner.Kind {
			view.services[service.Name] = service
		}
	}
	statefulSetList := &appsv1.StatefulSetList{}
	if err := r.APIReader.List(ctx, statefulSetList, client.InNamespace(namespace)); err != nil {
		return resourceView{}, fmt.Errorf("list admitted StatefulSets: %w", err)
	}
	statefulSetOwners := make(map[types.UID]struct{})
	for index := range statefulSetList.Items {
		statefulSet := &statefulSetList.Items[index]
		if owner := controllerOwner(statefulSet); owner != nil && leafOwners[owner.UID] == owner.Kind {
			view.statefulSets[statefulSet.Name] = statefulSet
			statefulSetOwners[statefulSet.UID] = struct{}{}
		}
	}
	podList := &corev1.PodList{}
	if err := r.APIReader.List(ctx, podList, client.InNamespace(namespace)); err != nil {
		return resourceView{}, fmt.Errorf("list admitted Pods: %w", err)
	}
	for index := range podList.Items {
		pod := &podList.Items[index]
		if owner := controllerOwner(pod); owner != nil && owner.Kind == "StatefulSet" {
			if _, included := statefulSetOwners[owner.UID]; included {
				view.pods[pod.Name] = pod
			}
		}
	}
	return view, nil
}

func (r Reader) readNetwork(ctx context.Context, namespace, name string) (observationv1alpha1.NetworkBinding, []observationv1alpha1.ObservedActorIdentity, error) {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(networkGVK)
	if err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, object); err != nil {
		if apierrors.IsNotFound(err) {
			return observationv1alpha1.NetworkBinding{}, nil, &NotReadyError{Reason: "referenced StacksNetwork does not exist"}
		}
		return observationv1alpha1.NetworkBinding{}, nil, err
	}
	phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
	ready, _, _ := unstructured.NestedBool(object.Object, "status", "inventoryReady")
	digest, _, _ := unstructured.NestedString(object.Object, "status", "inventoryDigest")
	observedGeneration, _, _ := unstructured.NestedInt64(object.Object, "status", "observedGeneration")
	if phase != "Ready" || !ready || digest == "" || observedGeneration != object.GetGeneration() {
		return observationv1alpha1.NetworkBinding{}, nil, &NotReadyError{Reason: "referenced StacksNetwork has no complete current admitted inventory"}
	}
	actorValues, found, err := unstructured.NestedSlice(object.Object, "status", "actors")
	if err != nil || !found {
		return observationv1alpha1.NetworkBinding{}, nil, &InconclusiveError{Reason: "admitted actor inventory is absent or malformed"}
	}
	if len(actorValues) > 232 {
		return observationv1alpha1.NetworkBinding{}, nil, &InconclusiveError{Reason: "admitted actor inventory exceeds the supported topology maximum"}
	}
	actors := make([]observationv1alpha1.ObservedActorIdentity, 0, len(actorValues))
	seen := make(map[string]struct{}, len(actorValues))
	for _, value := range actorValues {
		fields, ok := value.(map[string]any)
		if !ok {
			return observationv1alpha1.NetworkBinding{}, nil, &InconclusiveError{Reason: "admitted actor identity is malformed"}
		}
		actor, err := decodeActor(fields)
		if err != nil {
			return observationv1alpha1.NetworkBinding{}, nil, &InconclusiveError{Reason: err.Error()}
		}
		key := actor.Kind + "\x00" + actor.Name
		if _, duplicate := seen[key]; duplicate {
			return observationv1alpha1.NetworkBinding{}, nil, &InconclusiveError{Reason: fmt.Sprintf("duplicate admitted actor %s/%s", actor.Kind, actor.Name)}
		}
		seen[key] = struct{}{}
		actors = append(actors, actor)
	}
	sort.Slice(actors, func(i, j int) bool {
		if actors[i].Kind != actors[j].Kind {
			return actors[i].Kind < actors[j].Kind
		}
		return actors[i].Name < actors[j].Name
	})
	binding := observationv1alpha1.NetworkBinding{Name: name, UID: object.GetUID(), ObservedGeneration: observedGeneration, InventoryDigest: digest}
	return binding, actors, nil
}

func decodeActor(value map[string]any) (observationv1alpha1.ObservedActorIdentity, error) {
	required := func(name string) (string, error) {
		field, ok := value[name].(string)
		if !ok || field == "" {
			return "", fmt.Errorf("admitted actor identity field %q is absent or malformed", name)
		}
		return field, nil
	}
	fields := make([]string, 0, 14)
	for _, name := range []string{"kind", "name", "role", "resourceName", "serviceName", "statefulSetName", "statefulSetUID", "controllerRevision", "podName", "podUID", "requestedImage", "runtimeImageID", "configDigest", "specDigest"} {
		if name == "role" && value["kind"] == "BitcoinNode" {
			if _, present := value[name]; present {
				return observationv1alpha1.ObservedActorIdentity{}, fmt.Errorf("Bitcoin identity must not declare a role")
			}
			fields = append(fields, "")
			continue
		}
		field, err := required(name)
		if err != nil {
			return observationv1alpha1.ObservedActorIdentity{}, err
		}
		fields = append(fields, field)
	}
	return observationv1alpha1.ObservedActorIdentity{
		Kind: fields[0], Name: fields[1], Role: fields[2], ResourceName: fields[3], ServiceName: fields[4],
		StatefulSetName: fields[5], StatefulSetUID: types.UID(fields[6]), ControllerRevision: fields[7],
		PodName: fields[8], PodUID: types.UID(fields[9]), RequestedImage: fields[10], RuntimeImageID: fields[11],
		ConfigDigest: fields[12], SpecDigest: fields[13], EvidenceClass: "orchestrator-observed",
	}, nil
}

func verifyActor(binding observationv1alpha1.NetworkBinding, actor observationv1alpha1.ObservedActorIdentity, resources resourceView) error {
	leaf := resources.leaves[actor.Kind+"\x00"+actor.ResourceName]
	if leaf == nil {
		return fmt.Errorf("admitted leaf resource is absent for %s/%s", actor.Kind, actor.Name)
	}
	if !controlledBy(leaf, "StacksNetwork", binding.UID) || leaf.GetGeneration() == 0 {
		return fmt.Errorf("leaf ownership or generation diverged for %s/%s", actor.Kind, actor.Name)
	}
	if !labelsMatch(leaf, binding.Name, actor, false) {
		return fmt.Errorf("leaf labels diverged for %s/%s", actor.Kind, actor.Name)
	}
	networkRef, _, _ := unstructured.NestedString(leaf.Object, "spec", "networkRef", "name")
	actorName, _, _ := unstructured.NestedString(leaf.Object, "spec", "actorName")
	role := ""
	switch actor.Kind {
	case "StacksSigner":
		role = "signer"
	case "StacksNode":
		role, _, _ = unstructured.NestedString(leaf.Object, "spec", "role")
	}
	if networkRef != binding.Name || actorName != actor.Name || role != actor.Role || leaf.GetName() != actor.ResourceName {
		return fmt.Errorf("leaf declaration diverged for %s/%s", actor.Kind, actor.Name)
	}
	observedGeneration, _, _ := unstructured.NestedInt64(leaf.Object, "status", "observedGeneration")
	phase, _, _ := unstructured.NestedString(leaf.Object, "status", "phase")
	ready, _, _ := unstructured.NestedBool(leaf.Object, "status", "ready")
	statusIdentity, found, err := unstructured.NestedMap(leaf.Object, "status", "identity")
	if err != nil || !found || observedGeneration != leaf.GetGeneration() || phase != "Ready" || !ready {
		return fmt.Errorf("leaf status is not current and ready for %s/%s", actor.Kind, actor.Name)
	}
	leafIdentity, err := decodeActor(statusIdentity)
	if err != nil || !reflect.DeepEqual(leafIdentity, actor) {
		return fmt.Errorf("leaf status identity diverged for %s/%s", actor.Kind, actor.Name)
	}
	spec, found, err := unstructured.NestedMap(leaf.Object, "spec")
	if err != nil || !found {
		return fmt.Errorf("leaf specification is absent for %s/%s", actor.Kind, actor.Name)
	}
	specDigest, err := canonical.Digest(spec)
	if err != nil || specDigest != actor.SpecDigest {
		return fmt.Errorf("leaf specification digest diverged for %s/%s", actor.Kind, actor.Name)
	}
	service := resources.services[actor.ServiceName]
	if service == nil || !controlledBy(service, actor.Kind, leaf.GetUID()) || service.Name != actor.ResourceName {
		return fmt.Errorf("Service identity diverged for %s/%s", actor.Kind, actor.Name)
	}
	if !labelsMatch(service, binding.Name, actor, true) {
		return fmt.Errorf("Service labels diverged for %s/%s", actor.Kind, actor.Name)
	}
	if !serviceSpecMatches(service, binding.Name, actor, spec) {
		return fmt.Errorf("Service routing contract diverged for %s/%s", actor.Kind, actor.Name)
	}
	statefulSet := resources.statefulSets[actor.StatefulSetName]
	if statefulSet == nil {
		return fmt.Errorf("admitted StatefulSet is absent for %s/%s", actor.Kind, actor.Name)
	}
	if !controlledBy(statefulSet, actor.Kind, leaf.GetUID()) || statefulSet.UID != actor.StatefulSetUID || statefulSet.Status.ObservedGeneration != statefulSet.Generation || statefulSet.Status.ReadyReplicas != 1 || statefulSet.Status.CurrentRevision != actor.ControllerRevision || statefulSet.Status.UpdateRevision != actor.ControllerRevision {
		return fmt.Errorf("StatefulSet identity diverged for %s/%s", actor.Kind, actor.Name)
	}
	if !labelsMatch(statefulSet, binding.Name, actor, true) {
		return fmt.Errorf("StatefulSet labels diverged for %s/%s", actor.Kind, actor.Name)
	}
	if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 1 || statefulSet.Spec.ServiceName != actor.ServiceName || statefulSet.Spec.Template.Annotations[configDigestAnnotation] != actor.ConfigDigest || containerImage(statefulSet.Spec.Template.Spec.Containers, "actor") != actor.RequestedImage {
		return fmt.Errorf("StatefulSet requested image diverged for %s/%s", actor.Kind, actor.Name)
	}
	pod := resources.pods[actor.PodName]
	if pod == nil {
		return fmt.Errorf("admitted Pod is absent for %s/%s", actor.Kind, actor.Name)
	}
	if pod.UID != actor.PodUID || !controlledBy(pod, "StatefulSet", actor.StatefulSetUID) || !podReady(pod) {
		return fmt.Errorf("Pod identity or readiness diverged for %s/%s", actor.Kind, actor.Name)
	}
	if !labelsMatch(pod, binding.Name, actor, true) {
		return fmt.Errorf("Pod labels diverged for %s/%s", actor.Kind, actor.Name)
	}
	if pod.Labels[appsv1.StatefulSetRevisionLabel] != actor.ControllerRevision {
		return fmt.Errorf("Pod revision diverged for %s/%s", actor.Kind, actor.Name)
	}
	if containerImage(pod.Spec.Containers, "actor") != actor.RequestedImage {
		return fmt.Errorf("Pod requested image diverged for %s/%s", actor.Kind, actor.Name)
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "actor" {
			if immutableImageID(status.ImageID) != actor.RuntimeImageID {
				return fmt.Errorf("runtime image identity diverged for %s/%s", actor.Kind, actor.Name)
			}
			return nil
		}
	}
	return fmt.Errorf("actor container status absent for %s/%s", actor.Kind, actor.Name)
}

func serviceSpecMatches(service *corev1.Service, network string, actor observationv1alpha1.ObservedActorIdentity, spec map[string]any) bool {
	if service.Spec.Type != corev1.ServiceTypeClusterIP || service.Spec.ClusterIP != corev1.ClusterIPNone || !service.Spec.PublishNotReadyAddresses ||
		service.Spec.ExternalName != "" || len(service.Spec.ExternalIPs) != 0 || service.Spec.SessionAffinity != corev1.ServiceAffinityNone ||
		service.Spec.SessionAffinityConfig != nil || service.Spec.LoadBalancerIP != "" || len(service.Spec.LoadBalancerSourceRanges) != 0 ||
		service.Spec.LoadBalancerClass != nil || service.Spec.AllocateLoadBalancerNodePorts != nil || service.Spec.HealthCheckNodePort != 0 ||
		service.Spec.TrafficDistribution != nil || service.Spec.InternalTrafficPolicy == nil ||
		*service.Spec.InternalTrafficPolicy != corev1.ServiceInternalTrafficPolicyCluster {
		return false
	}
	expectedSelector := map[string]string{
		managedByLabel: "stacks-network-operator", networkLabel: network, actorLabel: actor.Name, actorResourceLabel: actor.ResourceName,
	}
	if !reflect.DeepEqual(service.Spec.Selector, expectedSelector) {
		return false
	}
	expectedPorts, ok := expectedActorPorts(actor.Kind, spec)
	if !ok {
		return false
	}
	if len(service.Spec.Ports) != len(expectedPorts) {
		return false
	}
	seen := make(map[string]struct{}, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		expected, exists := expectedPorts[port.Name]
		_, duplicate := seen[port.Name]
		if !exists || duplicate || port.Port != expected || port.Protocol != corev1.ProtocolTCP || port.TargetPort.StrVal != port.Name || port.NodePort != 0 || port.AppProtocol != nil {
			return false
		}
		seen[port.Name] = struct{}{}
	}
	return true
}

func expectedActorPorts(kind string, spec map[string]any) (map[string]int32, bool) {
	switch kind {
	case "BitcoinNode":
		rpc, p2p := int32(18443), int32(18444)
		if value, found, err := unstructured.NestedInt64(spec, "rpcPort"); err != nil {
			return nil, false
		} else if found {
			rpc = int32(value)
		}
		if value, found, err := unstructured.NestedInt64(spec, "p2pPort"); err != nil {
			return nil, false
		} else if found {
			p2p = int32(value)
		}
		return map[string]int32{"rpc": rpc, "p2p": p2p}, true
	case "StacksNode":
		return map[string]int32{"rpc": 20443, "p2p": 20444, "metrics": 20446}, true
	case "StacksSigner":
		return map[string]int32{"events": 30000, "metrics": 31000}, true
	default:
		return nil, false
	}
}

type digestActor struct {
	Kind               string    `json:"kind"`
	Name               string    `json:"name"`
	Role               string    `json:"role,omitempty"`
	ResourceName       string    `json:"resourceName"`
	ServiceName        string    `json:"serviceName"`
	StatefulSetName    string    `json:"statefulSetName"`
	StatefulSetUID     types.UID `json:"statefulSetUID"`
	ControllerRevision string    `json:"controllerRevision"`
	PodName            string    `json:"podName"`
	PodUID             types.UID `json:"podUID"`
	RequestedImage     string    `json:"requestedImage"`
	RuntimeImageID     string    `json:"runtimeImageID"`
	ConfigDigest       string    `json:"configDigest"`
	SpecDigest         string    `json:"specDigest"`
}

func inventoryDigest(generation int64, actors []observationv1alpha1.ObservedActorIdentity) (string, error) {
	digested := make([]digestActor, 0, len(actors))
	for _, actor := range actors {
		digested = append(digested, digestActor{
			Kind: actor.Kind, Name: actor.Name, Role: actor.Role, ResourceName: actor.ResourceName, ServiceName: actor.ServiceName,
			StatefulSetName: actor.StatefulSetName, StatefulSetUID: actor.StatefulSetUID, ControllerRevision: actor.ControllerRevision,
			PodName: actor.PodName, PodUID: actor.PodUID, RequestedImage: actor.RequestedImage, RuntimeImageID: actor.RuntimeImageID,
			ConfigDigest: actor.ConfigDigest, SpecDigest: actor.SpecDigest,
		})
	}
	payload := struct {
		SchemaVersion      string        `json:"schemaVersion"`
		ObservedGeneration int64         `json:"observedGeneration"`
		Actors             []digestActor `json:"actors"`
	}{SchemaVersion: "network.stacks.org/inventory/v1", ObservedGeneration: generation, Actors: digested}
	return canonical.Digest(payload)
}

func containerImage(containers []corev1.Container, name string) string {
	for _, container := range containers {
		if container.Name == name {
			return container.Image
		}
	}
	return ""
}

func controlledBy(object client.Object, kind string, uid types.UID) bool {
	owner := controllerOwner(object)
	return owner != nil && owner.Kind == kind && owner.UID == uid
}

func controllerOwner(object client.Object) *metav1.OwnerReference {
	return metav1.GetControllerOf(object)
}

func labelsMatch(object client.Object, network string, actor observationv1alpha1.ObservedActorIdentity, requireResource bool) bool {
	actorKind, ok := map[string]string{"BitcoinNode": "bitcoin", "StacksNode": "stacks-node", "StacksSigner": "stacks-signer"}[actor.Kind]
	if !ok {
		return false
	}
	labels := object.GetLabels()
	if labels[managedByLabel] != "stacks-network-operator" || labels[networkLabel] != network || labels[actorLabel] != actor.Name || labels[actorKindLabel] != actorKind {
		return false
	}
	return !requireResource || labels[actorResourceLabel] == actor.ResourceName
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func immutableImageID(value string) string {
	index := strings.LastIndex(value, "sha256:")
	if index < 0 || len(value[index:]) != 71 {
		return ""
	}
	digest := value[index:]
	for _, character := range strings.TrimPrefix(digest, "sha256:") {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return ""
		}
	}
	return digest
}
