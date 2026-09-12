//go:build integration

package foundationintegration

import (
	"context"
	"reflect"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyLateSignerAttachments exercises permanent constraints after an actual genesis capture.
func verifyLateSignerAttachments(t *testing.T, ctx context.Context, c client.Client, r *foundation.Reconciler, request ctrl.Request, root *api.StacksNetwork) {
	t.Helper()
	original := *root.Spec.DeepCopy()
	admitted := participant(t, ctx, c, root, "signer-01")
	extra := func(name, node string) api.Participant {
		return api.Participant{Name: name, Kind: "StacksSigner", Definition: api.Definition{Ref: &common.NameRef{Name: "signer-01"}}, Overrides: &api.Configuration{StacksSigner: &stacks.StacksSignerSpec{NodeRef: &common.NameRef{Name: node}}}}
	}
	root.Spec.Participants = append(root.Spec.Participants,
		extra("late-z", "late-node"), extra("aaa-conflict", "signer-node-01"), extra("late-a", "late-node"),
		api.Participant{Name: "late-node", Kind: "StacksNode", Definition: api.Definition{Ref: &common.NameRef{Name: "signer-node-01"}}})
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	for _, name := range []string{"aaa-conflict", "late-z"} {
		p := participant(t, ctx, c, root, name)
		requireReason(t, p, "TopologyConflict")
		if p.Status.Admission != nil || !meta.IsStatusConditionFalse(p.Status.Conditions, "Resolved") {
			t.Fatalf("conflicting signer %s admitted", name)
		}
	}
	existing := participant(t, ctx, c, root, "signer-01")
	requireReason(t, existing, "Admitted")
	if existing.UID != admitted.UID || !reflect.DeepEqual(existing.Status.Admission, admitted.Status.Admission) {
		t.Fatal("new contender displaced existing signer")
	}
	winner := participant(t, ctx, c, root, "late-a")
	requireReason(t, winner, "Admitted")
	// Both legal signers intentionally share an account; this is not key exclusivity.
	if winner.Status.Admission.Configuration.StacksSigner.AccountRef.Name != admitted.Status.Admission.Configuration.StacksSigner.AccountRef.Name {
		t.Fatal("fixture lost shared account")
	}
	// A lexically earlier addition cannot displace the newly established admission either.
	root.Spec.Participants = append(root.Spec.Participants, extra("aaa-later", "late-node"))
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	requireReason(t, participant(t, ctx, c, root, "aaa-later"), "TopologyConflict")
	requireReason(t, participant(t, ctx, c, root, "late-a"), "Admitted")
	root.Spec = original
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	if root.Status.Phase != "Initializing" {
		t.Fatalf("removing conflicts did not restore root: %+v", root.Status)
	}
}

// missingAccountReader injects an authoritative read outage without changing cached resolution.
type missingAccountReader struct {
	client.Reader
	key client.ObjectKey
}

func (r *missingAccountReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*stacks.StacksAccount); ok && key == r.key {
		return apierrors.NewNotFound(schema.GroupResource{Group: "stacks.stacks.org", Resource: "stacksaccounts"}, key.Name)
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

// verifyRetainedDependencyFailure keeps the old policy as evidence while its own input is unavailable.
func verifyRetainedDependencyFailure(t *testing.T, ctx context.Context, c client.Client, r *foundation.Reconciler, request ctrl.Request, root *api.StacksNetwork) {
	t.Helper()
	old := participant(t, ctx, c, root, "traffic")
	var definition stacks.StacksTransactionProduction
	if err := c.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: "traffic"}, &definition); err != nil {
		t.Fatal(err)
	}
	spec := *definition.Spec.DeepCopy()
	definition.Spec.Interval = ptr.To(common.Duration("20s"))
	updateObject(t, ctx, c, &definition)
	reader := r.Reader
	r.Reader = &missingAccountReader{Reader: reader, key: client.ObjectKey{Namespace: root.Namespace, Name: spec.Recipient.AccountRef.Name}}
	defer func() { r.Reader = reader }()
	driveRoot(t, ctx, c, r, request, root)
	p := participant(t, ctx, c, root, "traffic")
	requireReason(t, p, "DependencyUnavailable")
	if !meta.IsStatusConditionFalse(p.Status.Conditions, "Resolved") || !reflect.DeepEqual(p.Status.Admission, old.Status.Admission) || root.Status.Phase == "Failed" {
		t.Fatal("unavailable retained admission erased or authorized, or root failed")
	}
	requireReason(t, participant(t, ctx, c, root, "signer-01"), "Admitted")
	r.Reader = reader
	driveRoot(t, ctx, c, r, request, root)
	p = participant(t, ctx, c, root, "traffic")
	requireReason(t, p, "Admitted")
	if *p.Status.Admission.Configuration.StacksTransactionProduction.Interval != "20s" {
		t.Fatal("recovery did not admit compatible cadence")
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(&definition), &definition); err != nil {
		t.Fatal(err)
	}
	definition.Spec = spec
	updateObject(t, ctx, c, &definition)
	driveRoot(t, ctx, c, r, request, root)
}

// verifyResolutionConditions observes intermediate status writes on the real API server.
func verifyResolutionConditions(t *testing.T, ctx context.Context, c client.Client, scheme *runtime.Scheme) {
	t.Helper()
	ns := "foundation-conditions"
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		t.Fatal(err)
	}
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: ns}, Spec: api.StacksNetworkSpec{Operation: "Paused", Participants: []api.Participant{{Name: "missing", Kind: "BitcoinNode", Definition: api.Definition{Ref: &common.NameRef{Name: "absent"}}}}}}
	if err := c.Create(ctx, root); err != nil {
		t.Fatal(err)
	}
	r := &foundation.Reconciler{Client: c, Reader: c, Scheme: scheme}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(root)}
	seen := false
	for i := 0; i < 8; i++ {
		if _, err := r.Reconcile(ctx, request); err != nil {
			t.Fatal(err)
		}
		if err := c.Get(ctx, request.NamespacedName, root); err != nil {
			t.Fatal(err)
		}
		condition := meta.FindStatusCondition(root.Status.Conditions, "Resolved")
		if condition != nil && condition.Reason == "Allocating" {
			seen = true
			if condition.Status != metav1.ConditionUnknown || condition.ObservedGeneration != root.Generation {
				t.Fatalf("premature resolution: %+v", condition)
			}
		}
	}
	if !seen || !meta.IsStatusConditionFalse(root.Status.Conditions, "Resolved") {
		t.Fatal("allocation/error condition sequence missing")
	}
	root.Spec.Participants = nil
	updateObject(t, ctx, c, root)
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, request.NamespacedName, root); err != nil {
		t.Fatal(err)
	}
	condition := meta.FindStatusCondition(root.Status.Conditions, "Resolved")
	if condition == nil || condition.Reason != "Withdrawing" || condition.Status != metav1.ConditionUnknown {
		t.Fatalf("withdrawal falsely resolved: %+v", condition)
	}
	root.Spec.Operation = "Stopped"
	root.Spec.Participants = []api.Participant{{Name: "never-resolved", Kind: "BitcoinNode", Definition: api.Definition{Inline: &api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{}}}}}
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	condition = meta.FindStatusCondition(root.Status.Conditions, "Resolved")
	if root.Status.Phase != "Stopped" || condition.Status != metav1.ConditionUnknown || condition.ObservedGeneration != root.Generation {
		t.Fatalf("stopped intent falsely resolved: %+v", root.Status)
	}
	// The paused, completely resolved cohort is asserted by the main freeze test.
}
