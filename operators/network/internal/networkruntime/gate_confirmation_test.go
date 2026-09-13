package networkruntime

import (
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGateTransitionConfirmsCurrentProcessDespiteHealthyCache(t *testing.T) {
	for _, mode := range []string{"valid", "replaced", "terminated"} {
		t.Run(mode, func(t *testing.T) {
			root, g, participants := cohortFixture(time.Now())
			p := participantFixture(root)
			p.UID = "bitcoin-participant"
			root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: p.Spec.ParticipantName, Kind: p.Spec.Kind})
			root.Status.Identities = append(root.Status.Identities, api.InstanceIdentity{Name: p.Spec.ParticipantName, UID: p.UID})
			p.Status.Admission = &api.Admission{PolicyDigest: "policy"}
			p.Status.Conditions = []metav1.Condition{{Type: "ConfigVerified", Status: metav1.ConditionTrue, ObservedGeneration: p.Generation}, {Type: "WorkloadReady", Status: metav1.ConditionTrue, ObservedGeneration: p.Generation}}
			owner := []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID, Controller: ptr.To(true)}}
			config := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: root.Namespace, UID: "config", OwnerReferences: owner}, Data: map[string][]byte{"value": []byte("private")}}
			credential := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "credential", Namespace: root.Namespace, UID: "credential", OwnerReferences: owner}, Data: map[string][]byte{"value": []byte("private")}}
			workload := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: root.Namespace, UID: "workload", OwnerReferences: owner}, Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))}}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: root.Namespace, UID: "pod", Labels: map[string]string{"network.stacks.org/network-uid": string(root.UID), "network.stacks.org/participant-uid": string(p.UID)}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "StatefulSet", Name: workload.Name, UID: workload.UID, Controller: ptr.To(true)}}}, Status: corev1.PodStatus{PodIP: "10.0.0.1", ContainerStatuses: []corev1.ContainerStatus{{Name: "bitcoin", ContainerID: "container", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
			p.Status.Runtime = &api.ParticipantRuntimeStatus{ObservedGeneration: p.Generation, PolicyDigest: "policy", PodRef: &common.Binding{Kind: "Pod", Name: pod.Name, UID: pod.UID}, ContainerID: "container", ConfigRef: &common.Binding{Kind: "Secret", Name: config.Name, UID: config.UID}, RPCSecretRef: &common.Binding{Kind: "Secret", Name: credential.Name, UID: credential.UID}, WorkloadRefs: []common.Binding{{Kind: "StatefulSet", Name: workload.Name, UID: workload.UID}}, Endpoints: []api.RuntimeEndpoint{{Name: "rpc", Port: 18443}}}
			record := &bitcoin.BitcoinExecution{ObjectMeta: metav1.ObjectMeta{Name: "execution", Namespace: root.Namespace, UID: "execution", OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetwork", Name: root.Name, UID: root.UID, Controller: ptr.To(true)}}}, Spec: bitcoin.BitcoinExecutionSpec{NetworkUID: root.UID, Participant: common.Binding{Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID}}, Status: bitcoin.BitcoinExecutionStatus{Observation: &bitcoin.BitcoinObservation{Height: 234, ObservedAt: metav1.Now(), Target: bitcoin.BitcoinTargetIdentity{Participant: common.Binding{Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID}, Pod: *p.Status.Runtime.PodRef, ContainerID: "container", Endpoint: "http://10.0.0.1:18443", Configuration: *p.Status.Runtime.ConfigRef, Credentials: *p.Status.Runtime.RPCSecretRef, PolicyDigest: "policy"}}}}
			root.Status.Bitcoin = &api.BitcoinRuntimeStatus{ExecutionRefs: []common.Binding{{Name: record.Name, UID: record.UID}}}
			initial := &bitcoin.BitcoinInitialization{ObjectMeta: metav1.ObjectMeta{Name: "initial", Namespace: root.Namespace, UID: "initial", OwnerReferences: record.OwnerReferences}, Spec: bitcoin.BitcoinInitializationSpec{NetworkUID: root.UID, Target: record.Spec.Participant}}

			root.Status.GenesisRef.Fingerprint = foundation.Digest(g.Spec)
			if err := initializeGates(root, g); err != nil {
				t.Fatal(err)
			}
			root.Status.Initialization.GateIndex = 1
			root.Status.Initialization.AuthorizedCeiling = 234
			at := metav1.Now()
			root.Status.Initialization.Gates[0].CompletedAt = &at
			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, bitcoin.AddToScheme, corev1.AddToScheme, appsv1.AddToScheme} {
				if err := add(scheme); err != nil {
					t.Fatal(err)
				}
			}
			objects := []client.Object{root, g, p, config, credential, workload, pod, record, initial}
			for i := range participants {
				objects = append(objects, &participants[i])
			}
			cache := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			switch mode {
			case "replaced":
				pod.UID = "new-pod"
			case "terminated":
				pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}
			}
			direct := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			r := &Reconciler{Client: cache, Reader: direct, Scheme: scheme}
			failed, err := r.reconcileGates(t.Context(), root, initial, participants)
			if err != nil || failed {
				t.Fatalf("failed=%v error=%v", failed, err)
			}
			want := int32(1)
			if mode == "valid" {
				want = 2
			}
			if root.Status.Initialization.GateIndex != want || (mode != "valid" && root.Status.Initialization.AuthorizedCeiling != 234) {
				t.Fatalf("stale cache authorized gate: %+v", root.Status.Initialization)
			}
		})
	}
}

func TestGateDeadlineReadsPreparationAcknowledgementHiddenByCache(t *testing.T) {
	root, g := gateFixture()
	first := metav1.NewTime(time.Now().Add(-3 * time.Minute).Truncate(time.Second))
	prepared := metav1.NewTime(first.Add(time.Minute))
	record := &bitcoin.BitcoinInitialization{ObjectMeta: metav1.ObjectMeta{Name: "initial", Namespace: root.Namespace, UID: "initial", OwnerReferences: g.OwnerReferences}, Spec: bitcoin.BitcoinInitializationSpec{NetworkUID: root.UID}, Status: bitcoin.BitcoinInitializationStatus{FirstCeilingObservedAt: &first}}
	r := fixtureReconciler(t, g, record)
	current := record.DeepCopy()
	current.Status.PreparedAt = &prepared
	r.Reader = fake.NewClientBuilder().WithScheme(r.Scheme).WithObjects(g, current).Build()
	failed, err := r.reconcileGates(t.Context(), root, record, nil)
	if err != nil || failed || meta.IsStatusConditionTrue(root.Status.Conditions, "Failed") || root.Status.Initialization.GateIndex != 1 || !root.Status.Initialization.Gates[0].CompletedAt.Equal(&prepared) {
		t.Fatalf("stale preparation latched failure: failed=%v error=%v status=%+v", failed, err, root.Status)
	}
}
