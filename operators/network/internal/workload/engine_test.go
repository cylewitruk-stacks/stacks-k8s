package workload

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/network/api/v1alpha1"
	operatorlabels "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/labels"
)

func TestObserveWithholdsIdentityUntilStatefulSetGenerationIsObserved(t *testing.T) {
	descriptor, desired, statefulSet, pod := observationFixture(t)
	statefulSet.Status.ObservedGeneration = statefulSet.Generation - 1
	status := observeFixture(t, descriptor, desired, statefulSet, pod)
	if status.Ready || status.Identity != nil || status.Phase != "Progressing" {
		t.Fatalf("stale StatefulSet status admitted identity: %#v", status)
	}
}

func TestApplyServiceReplacesOwnedNonHeadlessService(t *testing.T) {
	scheme, owner, desired, current := serviceReplacementFixture(t)
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
	err := (Engine{Client: kubeClient, Scheme: scheme}).applyService(context.Background(), owner, desired)
	if err == nil || IsPermanent(err) {
		t.Fatalf("replacement error = %v, permanent = %t", err, IsPermanent(err))
	}
	actual := &corev1.Service{}
	if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(current), actual); !apierrors.IsNotFound(err) {
		t.Fatalf("owned non-headless Service survived replacement: %v", err)
	}
}

func TestApplyServiceNeverDeletesUnownedNonHeadlessService(t *testing.T) {
	scheme, owner, desired, current := serviceReplacementFixture(t)
	current.OwnerReferences[0].UID = "foreign-owner-uid"
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
	err := (Engine{Client: kubeClient, Scheme: scheme}).applyService(context.Background(), owner, desired)
	if err == nil || !strings.Contains(err.Error(), "refuse to adopt") {
		t.Fatalf("foreign Service error = %v", err)
	}
	actual := &corev1.Service{}
	if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(current), actual); err != nil {
		t.Fatalf("foreign Service was deleted: %v", err)
	}
	if actual.UID != current.UID || actual.Spec.ClusterIP != "10.0.0.222" {
		t.Fatalf("foreign Service was mutated: %#v", actual)
	}
}

func serviceReplacementFixture(t *testing.T) (*runtime.Scheme, *networkv1alpha1.StacksNode, *corev1.Service, *corev1.Service) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := networkv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	controller := true
	owner := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "net-follower", Namespace: "test", UID: "actor-uid"}}
	desired := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: owner.Name, Namespace: owner.Namespace}, Spec: corev1.ServiceSpec{
		Type: corev1.ServiceTypeClusterIP, ClusterIP: corev1.ClusterIPNone, PublishNotReadyAddresses: true,
		SessionAffinity: corev1.ServiceAffinityNone, InternalTrafficPolicy: pointer(corev1.ServiceInternalTrafficPolicyCluster),
		Selector: map[string]string{"actor": "follower"}, Ports: []corev1.ServicePort{{Name: "rpc", Port: 20443}},
	}}
	current := desired.DeepCopy()
	current.UID = "service-uid"
	current.CreationTimestamp = metav1.Now()
	current.OwnerReferences = []metav1.OwnerReference{{APIVersion: networkv1alpha1.GroupVersion.String(), Kind: "StacksNode", Name: owner.Name, UID: owner.UID, Controller: &controller}}
	current.Spec.ClusterIP = "10.0.0.222"
	current.Spec.ClusterIPs = []string{"10.0.0.222"}
	return scheme, owner, desired, current
}

func TestObservePublishesOnlyObservedAndVerifiedIdentity(t *testing.T) {
	descriptor, desired, statefulSet, pod := observationFixture(t)
	status := observeFixture(t, descriptor, desired, statefulSet, pod)
	if !status.Ready || status.Identity == nil {
		t.Fatalf("status = %#v", status)
	}
	identity := status.Identity
	if identity.ServiceName != "observed-service" || identity.ConfigDigest != "observed-config-digest" || identity.RequestedImage != "stacks:v2" || identity.SpecDigest != descriptor.SpecDigest {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestObserveRejectsPodRevisionAndImageMismatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*corev1.Pod)
	}{
		{name: "revision", mutate: func(pod *corev1.Pod) { pod.Labels[appsv1.StatefulSetRevisionLabel] = "old-revision" }},
		{name: "image", mutate: func(pod *corev1.Pod) { pod.Spec.Containers[0].Image = "stacks:v1" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			descriptor, desired, statefulSet, pod := observationFixture(t)
			test.mutate(pod)
			status := observeFixture(t, descriptor, desired, statefulSet, pod)
			if status.Ready || status.Identity != nil {
				t.Fatalf("mismatch admitted identity: %#v", status)
			}
		})
	}
}

func TestObserveRejectsStatefulSetRequestedImageMismatch(t *testing.T) {
	descriptor, desired, statefulSet, pod := observationFixture(t)
	statefulSet.Spec.Template.Spec.Containers[0].Image = "stacks:v1"
	status := observeFixture(t, descriptor, desired, statefulSet, pod)
	if status.Ready || status.Identity != nil {
		t.Fatalf("StatefulSet image mismatch admitted identity: %#v", status)
	}
}

func TestImmutableStatefulSetDeclarationIsPermanent(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := networkv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	controller := true
	owner := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "net-follower", Namespace: "test", UID: "actor-uid"}}
	current := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
		Name: owner.Name, Namespace: owner.Namespace, UID: "statefulset-uid", CreationTimestamp: metav1.Now(),
		OwnerReferences: []metav1.OwnerReference{{APIVersion: networkv1alpha1.GroupVersion.String(), Kind: "StacksNode", Name: owner.Name, UID: owner.UID, Controller: &controller}},
	}, Spec: appsv1.StatefulSetSpec{ServiceName: "old-service"}}
	desired := current.DeepCopy()
	desired.Spec.ServiceName = "new-service"
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
	err := (Engine{Client: kubeClient, Scheme: scheme}).applyStatefulSet(context.Background(), owner, desired)
	if !IsPermanent(err) {
		t.Fatalf("immutable declaration error is not permanent: %v", err)
	}
}

func TestApplyServiceRestoresCompleteRoutingContract(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := networkv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	controller := true
	owner := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "net-follower", Namespace: "test", UID: "actor-uid"}}
	desired := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: owner.Name, Namespace: owner.Namespace}, Spec: corev1.ServiceSpec{
		Type: corev1.ServiceTypeClusterIP, ClusterIP: corev1.ClusterIPNone, PublishNotReadyAddresses: true,
		SessionAffinity: corev1.ServiceAffinityNone, InternalTrafficPolicy: pointer(corev1.ServiceInternalTrafficPolicyCluster), Selector: map[string]string{"actor": "follower"},
		Ports: []corev1.ServicePort{{Name: "rpc", Port: 20443}},
	}}
	current := desired.DeepCopy()
	current.UID = "service-uid"
	current.CreationTimestamp = metav1.Now()
	current.OwnerReferences = []metav1.OwnerReference{{APIVersion: networkv1alpha1.GroupVersion.String(), Kind: "StacksNode", Name: owner.Name, UID: owner.UID, Controller: &controller}}
	current.Spec.Type = corev1.ServiceTypeExternalName
	current.Spec.ExternalName = "redirect.example"
	current.Spec.ExternalIPs = []string{"203.0.113.1"}
	current.Spec.SessionAffinity = corev1.ServiceAffinityClientIP
	current.Spec.SessionAffinityConfig = &corev1.SessionAffinityConfig{}
	current.Spec.InternalTrafficPolicy = pointer(corev1.ServiceInternalTrafficPolicyLocal)
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
	engine := Engine{Client: kubeClient, Scheme: scheme}
	if err := engine.applyService(context.Background(), owner, desired); err != nil {
		t.Fatal(err)
	}
	actual := &corev1.Service{}
	if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(current), actual); err != nil {
		t.Fatal(err)
	}
	if actual.Spec.Type != corev1.ServiceTypeClusterIP || actual.Spec.ClusterIP != corev1.ClusterIPNone || actual.Spec.ExternalName != "" || len(actual.Spec.ExternalIPs) != 0 || actual.Spec.SessionAffinity != corev1.ServiceAffinityNone || actual.Spec.SessionAffinityConfig != nil || actual.Spec.InternalTrafficPolicy == nil || *actual.Spec.InternalTrafficPolicy != corev1.ServiceInternalTrafficPolicyCluster {
		t.Fatalf("routing drift survived reconciliation: %#v", actual.Spec)
	}
}

func observationFixture(t *testing.T) (Descriptor, *appsv1.StatefulSet, *appsv1.StatefulSet, *corev1.Pod) {
	t.Helper()
	owner := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "net-follower", Namespace: "test", UID: "actor-uid", Generation: 2}}
	descriptor := Descriptor{Owner: owner, Kind: "StacksNode", Network: "net", Actor: "follower", Role: "follower", Image: "stacks:v2", SpecDigest: "declared-spec-digest"}
	desired := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: owner.Name, Namespace: owner.Namespace}, Spec: appsv1.StatefulSetSpec{
		ServiceName: "desired-service", Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{configDigestAnnotation: "desired-config-digest"}}},
	}}
	statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: owner.Name, Namespace: owner.Namespace, UID: "statefulset-uid", Generation: 2}, Spec: appsv1.StatefulSetSpec{
		ServiceName: "observed-service", Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{configDigestAnnotation: "observed-config-digest"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "actor", Image: "stacks:v2"}}}},
	}, Status: appsv1.StatefulSetStatus{ObservedGeneration: 2, ReadyReplicas: 1, CurrentRevision: "revision-v2", UpdateRevision: "revision-v2"}}
	controlled := true
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: owner.Name + "-0", Namespace: owner.Namespace, UID: "pod-uid", Labels: map[string]string{
		operatorlabels.ManagedByKey: operatorlabels.ManagedByValue, operatorlabels.NetworkKey: descriptor.Network, operatorlabels.ActorKey: descriptor.Actor,
		operatorlabels.ActorResourceKey: owner.Name, appsv1.StatefulSetRevisionLabel: "revision-v2",
	}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "StatefulSet", Name: statefulSet.Name, UID: statefulSet.UID, Controller: &controlled}}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "actor", Image: "stacks:v2"}}}, Status: corev1.PodStatus{
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "actor", ImageID: "docker-pullable://stacks@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		}}
	return descriptor, desired, statefulSet, pod
}

func observeFixture(t *testing.T, descriptor Descriptor, desired, statefulSet *appsv1.StatefulSet, pod *corev1.Pod) networkv1alpha1.ActorStatus {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(statefulSet, pod).Build()
	status, err := (Engine{Reader: reader}).observe(context.Background(), descriptor, desired)
	if err != nil {
		t.Fatal(err)
	}
	return status
}
