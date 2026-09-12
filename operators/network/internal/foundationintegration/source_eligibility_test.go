//go:build integration

package foundationintegration

import (
	"context"
	"reflect"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyRetainedSourceEligibility exercises independent candidate and retained-source status writes.
func verifyRetainedSourceEligibility(t *testing.T, ctx context.Context, c client.Client, r *foundation.Reconciler, request ctrl.Request, root *api.StacksNetwork) {
	t.Helper()
	var source stacks.StacksTransactionProduction
	key := client.ObjectKey{Namespace: root.Namespace, Name: "traffic"}
	if err := c.Get(ctx, key, &source); err != nil {
		t.Fatal(err)
	}
	original := source.Spec.DeepCopy()
	prior := participant(t, ctx, c, root, "traffic").Status.Admission.DeepCopy()
	if prior.Source.UID != source.UID || prior.Source.Generation != source.Generation {
		t.Fatalf("admission omitted source provenance: %+v", prior.Source)
	}
	// A validly stored but replacement-only candidate must not revoke the previous policy.
	source.Spec.Recipient = &stacks.Recipient{AccountRef: &common.NameRef{Name: "admin-01"}}
	updateObject(t, ctx, c, &source)
	for i := range root.Spec.Participants {
		if root.Spec.Participants[i].Name == "traffic" {
			root.Spec.Participants[i].Control = &api.Control{Paused: ptr.To(true)}
		}
	}
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	assert := func(ready metav1.ConditionStatus, reason string) {
		t.Helper()
		p := participant(t, ctx, c, root, "traffic")
		condition := meta.FindStatusCondition(p.Status.Conditions, "AdmissionReady")
		if !reflect.DeepEqual(prior, p.Status.Admission) || condition == nil || condition.Status != ready || condition.Reason != reason || condition.ObservedGeneration != p.Generation {
			t.Fatalf("retained policy/eligibility mismatch: %+v", p.Status)
		}
		if p.Spec.Control == nil || !ptr.Deref(p.Spec.Control.Paused, false) {
			t.Fatal("source validation blocked independent pause projection")
		}
	}
	requireReason(t, participant(t, ctx, c, root, "traffic"), "RequiresReplacement")
	assert(metav1.ConditionTrue, "RetainedPolicyEligible")
	// Both deletion-in-progress and actual disappearance withdraw eligibility without erasing admission.
	source.Finalizers = []string{"test.stacks.network/retain-source"}
	updateObject(t, ctx, c, &source)
	if err := c.Delete(ctx, &source); err != nil {
		t.Fatal(err)
	}
	driveRoot(t, ctx, c, r, request, root)
	assert(metav1.ConditionFalse, "DefinitionUnavailable")
	if err := c.Get(ctx, key, &source); err != nil {
		t.Fatal(err)
	}
	source.Finalizers = nil
	updateObject(t, ctx, c, &source)
	driveRoot(t, ctx, c, r, request, root)
	assert(metav1.ConditionFalse, "DefinitionUnavailable")
	// Restore the declaration for fresh-root tests; this root cannot inherit its UID.
	replacement := &stacks.StacksTransactionProduction{ObjectMeta: metav1.ObjectMeta{Namespace: root.Namespace, Name: key.Name}, Spec: *original}
	if err := c.Create(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	driveRoot(t, ctx, c, r, request, root)
	assert(metav1.ConditionFalse, "DefinitionUnavailable")
}
