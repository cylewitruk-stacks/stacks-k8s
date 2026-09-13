package networkruntime

import (
	"context"
	"fmt"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestStoppedUnrecordedWorkerRequiresFreshNoActivationProof(t *testing.T) {
	for _, mode := range []string{
		"unstarted",
		"failed-unstarted",
		"allocation-observed",
		"pod-present",
		"pod-read-error",
		"admitted",
		"candidate",
		"execution",
		"prior-pod",
		"prior-workload",
		"finalizer",
		"newer-current-history",
	} {
		t.Run(mode, func(t *testing.T) {
			root := rootFixture()
			root.Spec.Operation = "Stopped"
			p := participantFixture(root)
			p.Spec.Kind = "StacksFaucet"
			p.Spec.ParticipantName = "faucet"
			p.Name = foundation.ParticipantName(string(root.UID), p.Spec.ParticipantName)
			p.Status = api.ParticipantStatus{}
			root.Spec.Participants = []api.Participant{{Name: p.Spec.ParticipantName, Kind: p.Spec.Kind}}
			switch mode {
			case "failed-unstarted":
				root.Status.Phase = "Failed"
			case "allocation-observed":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{ObservedGeneration: p.Generation}
			case "admitted":
				p.Status.Admission = &api.Admission{}
			case "candidate":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{WorkerCandidate: &api.WorkerCandidate{}}
			case "execution":
				p.Status.Execution = &api.WorkerExecutionStatus{Phase: "Inactive"}
			case "prior-pod":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{
					PodRef: &common.Binding{Kind: "Pod", Name: "prior", UID: "prior"},
				}
			case "prior-workload":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{
					WorkloadRefs: []common.Binding{{Kind: "Pod", Name: "prior", UID: "prior"}},
				}
			case "finalizer":
				p.Finalizers = []string{stacksworker.Finalizer}
			}
			objects := []client.Object{p}
			if mode == "pod-present" {
				objects = append(
					objects,
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      stacksworker.Name(p),
							Namespace: p.Namespace,
							UID:       "unexpected",
						},
					},
				)
			}
			scheme := runtime.NewScheme()
			_ = api.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			r := Reconciler{Client: c, Reader: c, Scheme: scheme}
			if mode == "pod-read-error" {
				r.Reader = allocationReadFailure{Reader: c}
			}
			if mode == "newer-current-history" {
				current := p.DeepCopy()
				current.Status.Admission = &api.Admission{}
				r.Reader = fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
			}
			if _, err := r.Reconcile(t.Context(), root); err != nil {
				t.Fatal(err)
			}
			stopped := mode == "unstarted" || mode == "failed-unstarted" || mode == "allocation-observed"
			if (root.Status.Phase == "Stopped") != stopped || len(root.Status.Identities) != 0 {
				t.Fatalf("stop result with %s: %+v", mode, root.Status)
			}
			if mode == "failed-unstarted" && !meta.IsStatusConditionTrue(root.Status.Conditions, "Failed") {
				t.Fatal("failure latch lost during unstarted stop")
			}
			if stopped &&
				(!meta.IsStatusConditionFalse(
					root.Status.Conditions,
					"Running",
				) ||
					!meta.IsStatusConditionFalse(
						root.Status.Conditions,
						"Operational",
					)) {
				t.Fatal("stop claimed operation")
			}
			current := &api.StacksNetworkParticipant{}
			if err := c.Get(t.Context(), client.ObjectKeyFromObject(p), current); err != nil {
				t.Fatal(err)
			}
			if current.Status.Runtime != nil && current.Status.Runtime.Terminated {
				t.Fatal("no-activation proof was persisted as a reconstructed runtime")
			}
		})
	}
}

// allocationReadFailure prevents absence from being inferred from an API outage.
type allocationReadFailure struct{ client.Reader }

func (r allocationReadFailure) Get(
	ctx context.Context,
	key client.ObjectKey,
	object client.Object,
	opts ...client.GetOption,
) error {
	if _, ok := object.(*corev1.Pod); ok {
		return fmt.Errorf("API unavailable")
	}
	return r.Reader.Get(ctx, key, object, opts...)
}
