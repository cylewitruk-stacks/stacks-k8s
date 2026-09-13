package networkruntime

import (
	"context"
	"fmt"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// publicObservationReader rejects Secret value reads by the aggregate gate projector.
type publicObservationReader struct {
	client.Reader
	metadataReads int
}

func (r *publicObservationReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		return fmt.Errorf("operator read Secret values")
	}
	if _, ok := obj.(*metav1.PartialObjectMetadata); ok {
		r.metadataReads++
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func TestInitializationHeightRequiresCurrentActorTransportAndMetadata(t *testing.T) {
	for _, mode := range []string{"valid", "participant", "pod", "process", "endpoint", "config", "credential", "workload", "policy", "metadata", "stale"} {
		t.Run(mode, func(t *testing.T) {
			root := rootFixture()
			p := participantFixture(root)
			root.Spec.Participants = []api.Participant{{Name: p.Spec.ParticipantName, Kind: p.Spec.Kind}}
			root.Status.Identities = []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: p.UID}}
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
			initial := &bitcoin.BitcoinInitialization{Spec: bitcoin.BitcoinInitializationSpec{Target: record.Spec.Participant}}
			switch mode {
			case "participant":
				p.UID = "replacement"
			case "pod":
				pod.UID = "replacement"
			case "process":
				pod.Status.ContainerStatuses[0].ContainerID = "replacement"
			case "endpoint":
				pod.Status.PodIP = "10.0.0.2"
			case "config":
				p.Status.Runtime.ConfigRef.UID = "replacement"
			case "credential":
				p.Status.Runtime.RPCSecretRef.UID = "replacement"
			case "workload":
				workload.UID = "replacement"
			case "policy":
				p.Status.Admission.PolicyDigest = "replacement"
			case "metadata":
				config.UID = "replacement"
			case "stale":
				record.Status.Observation.ObservedAt = metav1.NewTime(time.Now().Add(-17 * time.Second))
			}
			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, bitcoin.AddToScheme, corev1.AddToScheme, appsv1.AddToScheme} {
				if err := add(scheme); err != nil {
					t.Fatal(err)
				}
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(root, p, config, credential, workload, pod, record).Build()
			reader := &publicObservationReader{Reader: c}
			r := &Reconciler{Client: c, Reader: reader, Scheme: scheme}
			height, known, err := r.initializationHeight(context.Background(), root, initial, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if known != (mode == "valid") || known && (height != 234 || reader.metadataReads != 2) {
				t.Fatalf("height=%d known=%v metadata=%d", height, known, reader.metadataReads)
			}
			if mode == "stale" {
				before := record.DeepCopy()
				record.Status.Observation.ObservedAt = metav1.Now()
				if foundation.BitcoinExecutionChanged(before, record) {
					t.Fatal("timestamp-only recovery should not enqueue")
				}
				if err := c.Update(t.Context(), record); err != nil {
					t.Fatal(err)
				}
				// A timer-driven read observes cache recovery without a notification.
				height, known, err = r.initializationHeight(t.Context(), root, initial, time.Now())
				if err != nil || !known || height != 234 {
					t.Fatalf("timer recovery: %d %v %v", height, known, err)
				}
			}
		})
	}
}
