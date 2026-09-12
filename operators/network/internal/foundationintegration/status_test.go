//go:build integration

package foundationintegration

import (
	"context"
	"reflect"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	ctrl "sigs.k8s.io/controller-runtime"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifySharedParticipantStatus exercises ownership migration and field deletion at the API server.
func verifySharedParticipantStatus(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	const ns = "foundation-shared-status"
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		t.Fatal(err)
	}
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "participant", Namespace: ns}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: "test-network", ParticipantName: "btc", Kind: "BitcoinNode", Source: api.Source{Generation: 1, Digest: "test"}, Configuration: api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{}}}}
	if err := c.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	mk := func(typ, reason string) metav1.Condition {
		return metav1.Condition{Type: typ, Status: metav1.ConditionTrue, Reason: reason, Message: reason, ObservedGeneration: p.Generation, LastTransitionTime: metav1.Now()}
	}
	// Seed the actual pre-migration representation, not SSA-created test state.
	before := p.DeepCopy()
	p.Status.Admission = &api.Admission{PolicyDigest: "old-policy", Configuration: p.Spec.Configuration}
	p.Status.Conditions = []metav1.Condition{mk("Resolved", "Admitted"), mk("WorkloadReady", "RuntimeNotImplemented")}
	if err := c.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}), client.FieldOwner("foundation")); err != nil {
		t.Fatal(err)
	}
	// Match the real aggregate call: candidate admission aliases the fetched participant.
	p.Status.Admission.PolicyDigest = "policy"
	aggregate := api.ParticipantStatus{Admission: p.Status.Admission, Conditions: append([]metav1.Condition(nil), p.Status.Conditions...)}
	if err := participantstatus.Apply(ctx, c, p, aggregate, participantstatus.AggregateManager); err != nil {
		t.Fatal(err)
	}
	if p.Status.Admission.PolicyDigest != "policy" {
		t.Fatal("migration overwrote aliased candidate admission")
	}
	domain := api.ParticipantStatus{Runtime: &api.ParticipantRuntimeStatus{ObservedGeneration: p.Generation, PolicyDigest: "policy"}, Conditions: []metav1.Condition{mk("WorkloadReady", "ActorReady")}}
	const domainManager = "stacks-network-domain-bitcoinnode"
	if err := participantstatus.Apply(ctx, c, p, domain, domainManager); err != nil {
		t.Fatal(err)
	}
	stale := p.DeepCopy()
	// Aggregate relinquishes its fallback after the domain takes responsibility.
	aggregate.Conditions = aggregate.Conditions[:1]
	aggregate.Conditions[0].Reason = "PolicyRetained"
	if err := participantstatus.Apply(ctx, c, p, aggregate, participantstatus.AggregateManager); err != nil {
		t.Fatal(err)
	}
	if p.Status.Runtime == nil || p.Status.Runtime.PolicyDigest != "policy" || meta.FindStatusCondition(p.Status.Conditions, "WorkloadReady").Reason != "ActorReady" {
		t.Fatal("aggregate erased domain state during handoff")
	}
	if err := participantstatus.Apply(ctx, c, stale, domain, domainManager); !apierrors.IsConflict(err) {
		t.Fatalf("stale status write must conflict, got %v", err)
	}
	// Independent deletion of a condition cannot erase the other manager's condition or admission.
	domain.Conditions = nil
	if err := participantstatus.Apply(ctx, c, p, domain, domainManager); err != nil {
		t.Fatal(err)
	}
	if meta.FindStatusCondition(p.Status.Conditions, "WorkloadReady") != nil || meta.FindStatusCondition(p.Status.Conditions, "Resolved") == nil || p.Status.Admission == nil {
		t.Fatalf("condition deletion crossed ownership: %+v", p.Status)
	}
	domain.Runtime = nil
	if err := participantstatus.Apply(ctx, c, p, domain, domainManager); err != nil {
		t.Fatal(err)
	}
	if p.Status.Runtime != nil || p.Status.Admission.PolicyDigest != "policy" {
		t.Fatal("subtree deletion crossed ownership")
	}
}

// verifyRuntimeConditionHandoff tests the aggregate's unchanged-policy fast path during rollout.
func verifyRuntimeConditionHandoff(t *testing.T, ctx context.Context, c client.Client, r *foundation.Reconciler, request ctrl.Request, root *api.StacksNetwork) {
	t.Helper()
	var name string
	for _, entry := range root.Spec.Participants {
		if entry.Kind == "BitcoinNode" {
			name = entry.Name
			break
		}
	}
	if name == "" {
		t.Fatal("fixture has no Bitcoin participant")
	}
	p := participant(t, ctx, c, root, name)
	priorResolved := *meta.FindStatusCondition(p.Status.Conditions, "Resolved")
	domain := api.ParticipantStatus{Conditions: []metav1.Condition{{Type: "WorkloadReady", Status: metav1.ConditionTrue, Reason: "ActorReady", Message: "ActorReady", ObservedGeneration: p.Generation, LastTransitionTime: metav1.Now()}}}
	const owner = "stacks-network-domain-bitcoinnode"
	if err := participantstatus.Apply(ctx, c, p, domain, owner); err != nil {
		t.Fatal(err)
	}
	saved := r.RuntimeKinds
	r.RuntimeKinds = map[api.ParticipantKind]bool{"BitcoinNode": true}
	defer func() { r.RuntimeKinds = saved }()
	driveRoot(t, ctx, c, r, request, root)
	p = participant(t, ctx, c, root, name)
	if participantstatus.OwnsCondition(p, participantstatus.AggregateManager, "WorkloadReady") {
		t.Fatal("unchanged admission retained fallback field ownership")
	}
	if got := meta.FindStatusCondition(p.Status.Conditions, "Resolved"); got == nil || !reflect.DeepEqual(*got, priorResolved) {
		t.Fatal("handoff required an unrelated condition change")
	}
	if got := meta.FindStatusCondition(p.Status.Conditions, "WorkloadReady"); got == nil || got.Reason != "ActorReady" {
		t.Fatal("handoff erased domain condition")
	}
	if err := participantstatus.Apply(ctx, c, p, api.ParticipantStatus{}, owner); err != nil {
		t.Fatal(err)
	}
	if meta.FindStatusCondition(p.Status.Conditions, "WorkloadReady") != nil {
		t.Fatal("retired fallback prevented domain removal")
	}
}
