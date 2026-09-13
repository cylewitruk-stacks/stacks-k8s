package participantworkload

import (
	"context"
	"encoding/json"
	"testing"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestStacksResolverWaitsForCompleteBitcoinPublication(t *testing.T) {
	for _, candidate := range []bool{false, true} {
		t.Run(map[bool]string{false: "published-genesis", true: "pre-freeze-candidate"}[candidate], func(t *testing.T) {
			base, root, p, btc := stacksInputFixture(t)
			_ = batchv1.AddToScheme(base.Scheme())
			_ = rbacv1.AddToScheme(base.Scheme())
			c := interceptor.NewClient(
				base.(client.WithWatch),
				interceptor.Funcs{
					Create: func(
						ctx context.Context,
						c client.WithWatch,
						obj client.Object,
						opts ...client.CreateOption,
					) error {
						if obj.GetUID() == "" {
							obj.SetUID(types.UID(obj.GetName() + "-uid"))
						}
						return c.Create(ctx, obj, opts...)
					},
				},
			)
			r := &Reconciler{Client: c, Reader: metadataOnlyReader{c}, ResolverImage: "resolver:test"}
			btcState := api.ParticipantRuntimeStatus{ActorRPCSecretRef: btc.Status.Runtime.ActorRPCSecretRef.DeepCopy()}
			btc.Status.Runtime = &btcState
			if candidate {
				root.Status.GenesisRef = nil
				r.candidateConfiguration = &foundation.CandidateConfiguration{
					Root: root,
					Participants: map[string]*api.StacksNetworkParticipant{
						p.Spec.ParticipantName:   p,
						btc.Spec.ParticipantName: btc,
					},
				}
			}
			publish := func() {
				t.Helper()
				btc.Status.Runtime = btcState.DeepCopy()
				actual := btc.DeepCopy()
				if candidate {
					actual.Status.Runtime = &api.ParticipantRuntimeStatus{
						ActorRPCSecretRef: btcState.ActorRPCSecretRef.DeepCopy(),
					}
				}
				if err := c.Status().Update(t.Context(), actual); err != nil {
					t.Fatal(err)
				}
				btc.ResourceVersion = actual.ResourceVersion
			}
			countJobs := func() int {
				t.Helper()
				var jobs batchv1.JobList
				if err := c.List(
					t.Context(),
					&jobs,
					client.MatchingLabels{"network.stacks.org/participant-uid": string(p.UID)},
				); err != nil {
					t.Fatal(err)
				}
				return len(jobs.Items)
			}
			publish()
			state := api.ParticipantRuntimeStatus{}
			if _, _, err := r.stacksConfiguration(t.Context(), root, p, &state); err == nil || countJobs() != 0 {
				t.Fatal("allocated empty Bitcoin credentials launched a dependent resolver")
			}
			ready, err := r.configuration(t.Context(), root, btc, &btcState)
			if err != nil || ready {
				t.Fatalf("Bitcoin pending: ready=%v err=%v", ready, err)
			}
			publish()
			if _, _, err := r.stacksConfiguration(t.Context(), root, p, &state); err == nil || countJobs() != 0 {
				t.Fatal("unfinished Bitcoin configuration launched a dependent resolver")
			}
			var reports corev1.ConfigMapList
			if err := c.List(
				t.Context(),
				&reports,
				client.MatchingLabels{"network.stacks.org/participant-uid": string(btc.UID)},
			); err != nil ||
				len(reports.Items) != 1 {
				t.Fatalf("Bitcoin report: %v count=%d", err, len(reports.Items))
			}
			var input BitcoinConfigInput
			if err := json.Unmarshal([]byte(reports.Items[0].Data["input.json"]), &input); err != nil {
				t.Fatal(err)
			}
			if err := RunBitcoinConfigResolver(t.Context(), c, input); err != nil {
				t.Fatal(err)
			}
			ready, err = r.configuration(t.Context(), root, btc, &btcState)
			if err != nil || !ready {
				t.Fatalf("Bitcoin resolved: ready=%v err=%v", ready, err)
			}
			publish()
			complete := *btcState.DeepCopy()
			for _, missing := range []string{"fingerprint", "input-digest", "policy"} {
				btcState = *complete.DeepCopy()
				switch missing {
				case "fingerprint":
					btcState.ConfigRef.Fingerprint = ""
				case "input-digest":
					btcState.ConfigurationDigest = ""
				case "policy":
					btcState.PolicyDigest = "stale"
				}
				publish()
				if _, _, err := r.stacksConfiguration(t.Context(), root, p, &state); err == nil || countJobs() != 0 {
					t.Fatalf("incomplete %s publication launched dependent resolver", missing)
				}
			}
			btcState = complete
			publish()
			if ready, _, err := r.stacksConfiguration(
				t.Context(),
				root,
				p,
				&state,
			); err != nil || ready ||
				countJobs() != 1 {
				t.Fatalf(
					"completed Bitcoin publication did not launch resolver: ready=%v err=%v jobs=%d",
					ready,
					err,
					countJobs(),
				)
			}
			var workloads appsv1.StatefulSetList
			if err := c.List(t.Context(), &workloads); err != nil || len(workloads.Items) != 0 {
				t.Fatal("support preparation activated actor")
			}
			if candidate && root.Status.GenesisRef != nil {
				t.Fatal("candidate rendering required a genesis publication")
			}
		})
	}
}
