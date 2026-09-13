package networkruntime

import (
	"context"
	"fmt"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// projectionReads counts uncached lifecycle checks independently of cached observations.
type projectionReads struct {
	client.Reader
	gets int
}

func (r *projectionReads) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	r.gets++
	return r.Reader.Get(ctx, key, obj, opts...)
}

func TestWorkerProjectionUsesCacheUntilLifecycleEvidenceChanges(t *testing.T) {
	for _, mode := range []string{"healthy", "cache-missing", "cache-failed", "actually-missing"} {
		t.Run(mode, func(t *testing.T) {
			root := rootFixture()
			root.Spec.Operation = "Running"
			root.Status.Identities = nil
			root.Spec.Participants = nil
			scheme := runtime.NewScheme()
			_ = api.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			var cached, live []client.Object
			var participants []api.StacksNetworkParticipant
			for i := 0; i < 40; i++ {
				p := participantFixture(root)
				p.Name = fmt.Sprintf("p-%02d", i)
				p.UID = types.UID(p.Name)
				p.Spec.ParticipantName = p.Name
				p.Spec.Kind = "StacksStacker"
				profile := stacksworker.Profile{
					Image:         "worker:test",
					Configuration: common.Binding{Kind: "ConfigMap", Name: "bootstrap", UID: "config"},
					Keys: []stacksworker.KeyMount{
						{
							Role:   "sender",
							Secret: common.Binding{Kind: "Secret", Name: "key", UID: "key"},
							Key:    "privateKey",
						},
					},
				}
				pod, err := stacksworker.Pod(p, profile)
				if err != nil {
					t.Fatal(err)
				}
				pod.UID = types.UID("pod-" + p.Name)
				root.Spec.Participants = append(
					root.Spec.Participants,
					api.Participant{Name: p.Name, Kind: p.Spec.Kind},
				)
				root.Status.Identities = append(
					root.Status.Identities,
					api.InstanceIdentity{
						Name: p.Name,
						UID:  p.UID,
						Worker: &api.WorkerSession{
							Pod:           api.WorkerPodBinding{Kind: "Pod", Name: pod.Name, UID: pod.UID},
							ProfileDigest: pod.Annotations["network.stacks.org/worker-profile"],
						},
					},
				)
				participants = append(participants, *p)
				cached = append(cached, p.DeepCopy())
				live = append(live, p.DeepCopy())
				if mode != "actually-missing" {
					live = append(live, pod.DeepCopy())
				}
				if mode != "cache-missing" && mode != "actually-missing" {
					if mode == "cache-failed" {
						pod.Status.Phase = corev1.PodFailed
					}
					cached = append(cached, pod)
				}
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cached...).Build()
			direct := &projectionReads{Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(live...).Build()}
			r := &Reconciler{Client: c, Reader: direct}
			for range 3 {
				if _, _, err := r.projectWorkers(context.Background(), root, participants); err != nil {
					t.Fatal(err)
				}
			}
			if got := meta.IsStatusConditionTrue(
				root.Status.Conditions,
				"Failed",
			); got != (mode == "actually-missing") {
				t.Fatalf("failure=%v mode=%s", got, mode)
			}
			want := 0
			if mode != "healthy" {
				want = 40 * 2 * 3
			}
			if direct.gets != want {
				t.Fatalf("fresh GETs=%d want=%d", direct.gets, want)
			}
			for _, id := range root.Status.Identities {
				if id.Worker == nil || id.Worker.Disposal != nil || id.Worker.Shutdown != nil {
					t.Fatal("observation mutated session identity")
				}
			}
		})
	}
}
