package topology

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	observationv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/canonical"
)

const (
	testNamespace = "observation-test"
	testNetwork   = "network"
	testConfig    = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testImageID   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

var testDigest = mustTestInventoryDigest()

func TestObserveVerifiesAdmittedIdentity(t *testing.T) {
	reader := testReader(t)
	snapshot, err := reader.Observe(context.Background(), testNamespace, testNetwork, testDigest)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Binding.InventoryDigest != testDigest || len(snapshot.Actors) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Actors[0].EvidenceClass != "orchestrator-observed" {
		t.Fatalf("evidence class = %q", snapshot.Actors[0].EvidenceClass)
	}
}

func TestObserveFailsClosedOnPodReplacement(t *testing.T) {
	objects := testObjects()
	objects[2].(*corev1.Pod).UID = "replacement-pod"
	reader := testReaderWith(t, objects...)
	_, err := reader.Observe(context.Background(), testNamespace, testNetwork, "")
	if !IsInconclusive(err) {
		t.Fatalf("error = %v", err)
	}
}

func TestObserveFailsClosedOnRequestedImageDrift(t *testing.T) {
	objects := testObjects()
	objects[2].(*corev1.Pod).Spec.Containers[0].Image = "stacks:changed"
	reader := testReaderWith(t, objects...)
	_, err := reader.Observe(context.Background(), testNamespace, testNetwork, "")
	if !IsInconclusive(err) {
		t.Fatalf("error = %v", err)
	}
}

func TestObserveFailsClosedOnUnverifiedTopologyFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]client.Object) []client.Object
	}{
		{name: "StatefulSet generation", mutate: func(objects []client.Object) []client.Object {
			objects[1].(*appsv1.StatefulSet).Status.ObservedGeneration = 1
			return objects
		}},
		{name: "configuration digest", mutate: func(objects []client.Object) []client.Object {
			objects[1].(*appsv1.StatefulSet).Spec.Template.Annotations[configDigestAnnotation] = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
			return objects
		}},
		{name: "Service missing", mutate: func(objects []client.Object) []client.Object {
			return append(objects[:3], objects[4:]...)
		}},
		{name: "Service traffic redirection", mutate: func(objects []client.Object) []client.Object {
			service := objects[3].(*corev1.Service)
			service.Spec.Type = corev1.ServiceTypeExternalName
			service.Spec.ExternalName = "redirect.example"
			return objects
		}},
		{name: "Service node-local routing", mutate: func(objects []client.Object) []client.Object {
			objects[3].(*corev1.Service).Spec.InternalTrafficPolicy = pointer(corev1.ServiceInternalTrafficPolicyLocal)
			return objects
		}},
		{name: "Service external IP", mutate: func(objects []client.Object) []client.Object {
			objects[3].(*corev1.Service).Spec.ExternalIPs = []string{"192.0.2.10"}
			return objects
		}},
		{name: "Service session affinity", mutate: func(objects []client.Object) []client.Object {
			objects[3].(*corev1.Service).Spec.SessionAffinity = corev1.ServiceAffinityClientIP
			return objects
		}},
		{name: "Service port", mutate: func(objects []client.Object) []client.Object {
			objects[3].(*corev1.Service).Spec.Ports[0].Port++
			return objects
		}},
		{name: "Service selector", mutate: func(objects []client.Object) []client.Object {
			objects[3].(*corev1.Service).Spec.Selector[actorLabel] = "other"
			return objects
		}},
		{name: "leaf specification", mutate: func(objects []client.Object) []client.Object {
			leaf := objects[4].(*unstructured.Unstructured)
			_ = unstructured.SetNestedField(leaf.Object, "stacks:changed", "spec", "image")
			return objects
		}},
		{name: "leaf labels", mutate: func(objects []client.Object) []client.Object {
			objects[4].(*unstructured.Unstructured).SetLabels(map[string]string{managedByLabel: "stacks-network-operator", networkLabel: "other"})
			return objects
		}},
		{name: "workload labels", mutate: func(objects []client.Object) []client.Object {
			objects[1].(*appsv1.StatefulSet).Labels[actorKindLabel] = "bitcoin"
			return objects
		}},
		{name: "aggregate digest", mutate: func(objects []client.Object) []client.Object {
			network := objects[0].(*unstructured.Unstructured)
			_ = unstructured.SetNestedField(network.Object, "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "status", "inventoryDigest")
			return objects
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := testReaderWith(t, test.mutate(testObjects())...)
			_, err := reader.Observe(context.Background(), testNamespace, testNetwork, "")
			if !IsInconclusive(err) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestObserveDetectsRetiredOwnedLeafDespiteLabelDrift(t *testing.T) {
	objects := testObjects()
	controller := true
	retired := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "network.stacks.org/v1alpha1", "kind": "StacksNode",
		"metadata": map[string]any{
			"name": "retired", "namespace": testNamespace, "uid": "retired-uid", "labels": map[string]any{networkLabel: "wrong"},
			"ownerReferences": []any{map[string]any{"apiVersion": "network.stacks.org/v1alpha1", "kind": "StacksNetwork", "name": testNetwork, "uid": "network-uid", "controller": controller}},
		},
	}}
	retired.SetGroupVersionKind(schema.GroupVersionKind{Group: "network.stacks.org", Version: "v1alpha1", Kind: "StacksNode"})
	objects = append(objects, retired)
	_, err := testReaderWith(t, objects...).Observe(context.Background(), testNamespace, testNetwork, "")
	if !IsInconclusive(err) {
		t.Fatalf("error = %v", err)
	}
}

func TestObserveFailsClosedWhenTopologyChangesMidRead(t *testing.T) {
	base := testReader(t).APIReader
	reader := Reader{APIReader: &changingNetworkReader{Reader: base}}
	_, err := reader.Observe(context.Background(), testNamespace, testNetwork, "")
	if !IsInconclusive(err) {
		t.Fatalf("error = %v", err)
	}
}

func TestObserveWaitsForCurrentCompleteInventory(t *testing.T) {
	objects := testObjects()
	network := objects[0].(*unstructured.Unstructured)
	_ = unstructured.SetNestedField(network.Object, false, "status", "inventoryReady")
	reader := testReaderWith(t, objects...)
	_, err := reader.Observe(context.Background(), testNamespace, testNetwork, "")
	if !IsNotReady(err) {
		t.Fatalf("error = %v", err)
	}
}

func TestObserveRejectsUnexpectedInventory(t *testing.T) {
	reader := testReader(t)
	_, err := reader.Observe(context.Background(), testNamespace, testNetwork, "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	if !IsInconclusive(err) {
		t.Fatalf("error = %v", err)
	}
}

type changingNetworkReader struct {
	client.Reader
	reads int
}

func (r *changingNetworkReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	if err := r.Reader.Get(ctx, key, object, options...); err != nil {
		return err
	}
	if value, ok := object.(*unstructured.Unstructured); ok && value.GroupVersionKind() == networkGVK {
		r.reads++
		if r.reads > 1 {
			_ = unstructured.SetNestedField(value.Object, "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "status", "inventoryDigest")
		}
	}
	return nil
}

func testReader(t *testing.T) Reader {
	t.Helper()
	return testReaderWith(t, testObjects()...)
}

func testReaderWith(t *testing.T, objects ...client.Object) Reader {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	scheme.AddKnownTypeWithName(networkGVK, &unstructured.Unstructured{})
	for _, resource := range leafResources {
		scheme.AddKnownTypeWithName(resource.gvk, &unstructured.UnstructuredList{})
		itemGVK := resource.gvk
		itemGVK.Kind = resource.kind
		scheme.AddKnownTypeWithName(itemGVK, &unstructured.Unstructured{})
	}
	return Reader{APIReader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
}

func testObjects() []client.Object {
	controller := true
	spec := map[string]any{
		"networkRef": map[string]any{"name": testNetwork}, "actorName": "follower", "role": "follower", "image": "stacks:test",
		"config": map[string]any{"generated": map[string]any{"profile": "nakamoto-regtest-node/v1"}},
	}
	specDigest, err := canonical.Digest(spec)
	if err != nil {
		panic(err)
	}
	actor := testActor(specDigest)
	network := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "network.stacks.org/v1alpha1",
		"kind":       "StacksNetwork",
		"metadata": map[string]any{
			"name": testNetwork, "namespace": testNamespace, "uid": "network-uid", "generation": int64(2),
		},
		"status": map[string]any{
			"phase": "Ready", "inventoryReady": true, "inventoryDigest": testDigest, "observedGeneration": int64(2),
			"actors": []any{actorMap(actor)},
		},
	}}
	network.SetGroupVersionKind(networkGVK)
	leaf := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "network.stacks.org/v1alpha1", "kind": "StacksNode",
		"metadata": map[string]any{"name": "network-follower", "namespace": testNamespace, "uid": "leaf-uid", "generation": int64(2),
			"labels": map[string]any{
				managedByLabel: "stacks-network-operator", networkLabel: testNetwork,
				actorLabel: "follower", actorKindLabel: "stacks-node",
			},
			"ownerReferences": []any{map[string]any{"apiVersion": "network.stacks.org/v1alpha1", "kind": "StacksNetwork", "name": testNetwork, "uid": "network-uid", "controller": true}},
		},
		"spec":   spec,
		"status": map[string]any{"observedGeneration": int64(2), "phase": "Ready", "ready": true, "identity": actorMap(actor)},
	}}
	leaf.SetGroupVersionKind(schema.GroupVersionKind{Group: "network.stacks.org", Version: "v1alpha1", Kind: "StacksNode"})
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "network-follower", Namespace: testNamespace, UID: "statefulset-uid", Generation: 2, Labels: testActorLabels(), OwnerReferences: []metav1.OwnerReference{{APIVersion: "network.stacks.org/v1alpha1", Kind: "StacksNode", Name: "network-follower", UID: "leaf-uid", Controller: &controller}}},
		Spec: appsv1.StatefulSetSpec{Replicas: pointer(int32(1)), ServiceName: "network-follower", Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{configDigestAnnotation: testConfig}},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "actor", Image: "stacks:test"}}},
		}},
		Status: appsv1.StatefulSetStatus{ObservedGeneration: 2, ReadyReplicas: 1, CurrentRevision: "network-follower-abc", UpdateRevision: "network-follower-abc"},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "network-follower-0", Namespace: testNamespace, UID: "pod-uid", Labels: testActorLabels(), OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", UID: types.UID("statefulset-uid"), Controller: &controller}}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "actor", Image: "stacks:test"}}},
		Status: corev1.PodStatus{
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "actor", ImageID: "docker.io/example@" + testImageID}},
		},
	}
	pod.Labels[appsv1.StatefulSetRevisionLabel] = "network-follower-abc"
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "network-follower", Namespace: testNamespace, UID: "service-uid", Labels: testActorLabels(), OwnerReferences: []metav1.OwnerReference{{APIVersion: "network.stacks.org/v1alpha1", Kind: "StacksNode", Name: "network-follower", UID: "leaf-uid", Controller: &controller}}}, Spec: corev1.ServiceSpec{
		Type: corev1.ServiceTypeClusterIP, ClusterIP: corev1.ClusterIPNone, PublishNotReadyAddresses: true, SessionAffinity: corev1.ServiceAffinityNone,
		InternalTrafficPolicy: pointer(corev1.ServiceInternalTrafficPolicyCluster),
		Selector:              map[string]string{managedByLabel: "stacks-network-operator", networkLabel: testNetwork, actorLabel: "follower", actorResourceLabel: "network-follower"},
		Ports:                 []corev1.ServicePort{{Name: "rpc", Port: 20443, TargetPort: intstr.FromString("rpc"), Protocol: corev1.ProtocolTCP}, {Name: "p2p", Port: 20444, TargetPort: intstr.FromString("p2p"), Protocol: corev1.ProtocolTCP}, {Name: "metrics", Port: 20446, TargetPort: intstr.FromString("metrics"), Protocol: corev1.ProtocolTCP}},
	}}
	return []client.Object{network, statefulSet, pod, service, leaf}
}

func testActorLabels() map[string]string {
	return map[string]string{
		managedByLabel: "stacks-network-operator", networkLabel: testNetwork, actorLabel: "follower",
		actorKindLabel: "stacks-node", actorResourceLabel: "network-follower",
	}
}

func testActor(specDigest string) observationv1alpha1.ObservedActorIdentity {
	return observationv1alpha1.ObservedActorIdentity{
		Kind: "StacksNode", Name: "follower", Role: "follower", ResourceName: "network-follower", ServiceName: "network-follower",
		StatefulSetName: "network-follower", StatefulSetUID: "statefulset-uid", ControllerRevision: "network-follower-abc",
		PodName: "network-follower-0", PodUID: "pod-uid", RequestedImage: "stacks:test", RuntimeImageID: testImageID,
		ConfigDigest: testConfig, SpecDigest: specDigest, EvidenceClass: "orchestrator-observed",
	}
}

func actorMap(actor observationv1alpha1.ObservedActorIdentity) map[string]any {
	return map[string]any{
		"kind": actor.Kind, "name": actor.Name, "role": actor.Role, "resourceName": actor.ResourceName, "serviceName": actor.ServiceName,
		"statefulSetName": actor.StatefulSetName, "statefulSetUID": string(actor.StatefulSetUID), "controllerRevision": actor.ControllerRevision,
		"podName": actor.PodName, "podUID": string(actor.PodUID), "requestedImage": actor.RequestedImage, "runtimeImageID": actor.RuntimeImageID,
		"configDigest": actor.ConfigDigest, "specDigest": actor.SpecDigest,
	}
}

func mustTestInventoryDigest() string {
	spec := map[string]any{
		"networkRef": map[string]any{"name": testNetwork}, "actorName": "follower", "role": "follower", "image": "stacks:test",
		"config": map[string]any{"generated": map[string]any{"profile": "nakamoto-regtest-node/v1"}},
	}
	specDigest, err := canonical.Digest(spec)
	if err != nil {
		panic(err)
	}
	digest, err := inventoryDigest(2, []observationv1alpha1.ObservedActorIdentity{testActor(specDigest)})
	if err != nil {
		panic(err)
	}
	return digest
}

func pointer[T any](value T) *T { return &value }

func TestObserveBitcoinIdentityWithoutRole(t *testing.T) {
	objects := testObjects()
	network := objects[0].(*unstructured.Unstructured)
	statefulSet := objects[1].(*appsv1.StatefulSet)
	pod := objects[2].(*corev1.Pod)
	service := objects[3].(*corev1.Service)
	leaf := objects[4].(*unstructured.Unstructured)
	spec, _, err := unstructured.NestedMap(leaf.Object, "spec")
	if err != nil {
		t.Fatal(err)
	}
	delete(spec, "role")
	spec["image"] = "bitcoin:test"
	spec["config"] = map[string]any{"generated": map[string]any{"profile": "bitcoin-regtest/v1"}}
	specDigest, err := canonical.Digest(spec)
	if err != nil {
		t.Fatal(err)
	}
	actor := testActor(specDigest)
	actor.Kind = "BitcoinNode"
	actor.Role = ""
	actor.RequestedImage = "bitcoin:test"
	identity := actorMap(actor)
	delete(identity, "role")
	digest, err := inventoryDigest(2, []observationv1alpha1.ObservedActorIdentity{actor})
	if err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedSlice(network.Object, []any{identity}, "status", "actors"); err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(network.Object, digest, "status", "inventoryDigest"); err != nil {
		t.Fatal(err)
	}
	leaf.SetKind("BitcoinNode")
	leaf.Object["spec"] = spec
	if err := unstructured.SetNestedMap(leaf.Object, identity, "status", "identity"); err != nil {
		t.Fatal(err)
	}
	for _, o := range objects[1:] {
		labels := o.GetLabels()
		labels[actorKindLabel] = "bitcoin"
		o.SetLabels(labels)
	}
	statefulSet.OwnerReferences[0].Kind = "BitcoinNode"
	service.OwnerReferences[0].Kind = "BitcoinNode"
	statefulSet.Spec.Template.Spec.Containers[0].Image = "bitcoin:test"
	pod.Spec.Containers[0].Image = "bitcoin:test"
	service.Spec.Ports = service.Spec.Ports[:2]
	service.Spec.Ports[0].Port = 18443
	service.Spec.Ports[1].Port = 18444
	snapshot, err := testReaderWith(t, objects...).Observe(context.Background(), testNamespace, testNetwork, digest)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Actors) != 1 || snapshot.Actors[0].Kind != "BitcoinNode" || snapshot.Actors[0].Role != "" {
		t.Fatalf("incorrect Bitcoin identity: %#v", snapshot.Actors)
	}
	if _, err := decodeActor(identity); err != nil {
		t.Fatal(err)
	}
	identity["role"] = "unexpected"
	if _, err := decodeActor(identity); err == nil {
		t.Fatal("Bitcoin identity accepted a role")
	}
	stacks := actorMap(testActor(specDigest))
	delete(stacks, "role")
	if _, err := decodeActor(stacks); err == nil {
		t.Fatal("Stacks identity accepted a missing role")
	}
}
