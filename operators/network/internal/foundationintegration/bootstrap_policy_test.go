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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyBootstrapGatePolicyRelease checks compatible rolls and the last affected gate on a real API server.
func verifyBootstrapGatePolicyRelease(t *testing.T, ctx context.Context, c client.Client, r *foundation.Reconciler, request ctrl.Request, root *api.StacksNetwork, genesis *api.StacksGenesis, cfg *rest.Config, scheme *runtime.Scheme) {
	t.Helper()
	var node stacks.StacksNode
	if err := c.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: "miner-01"}, &node); err != nil {
		t.Fatal(err)
	}
	oldNode := participant(t, ctx, c, root, "miner-01")
	originalNode := node.Spec.DeepCopy()
	node.Spec.Image = ptr.To("example.invalid/stacks:compatible-roll")
	node.Spec.Resources = &corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")}}
	updateObject(t, ctx, c, &node)
	driveRoot(t, ctx, c, r, request, root)
	rolled := participant(t, ctx, c, root, "miner-01")
	requireReason(t, rolled, "Admitted")
	if rolled.UID != oldNode.UID || rolled.Status.Admission.PolicyDigest == oldNode.Status.Admission.PolicyDigest || *rolled.Status.Admission.Configuration.StacksNode.Image != *node.Spec.Image || meta.IsStatusConditionTrue(rolled.Status.Conditions, "PolicyDeferred") {
		t.Fatal("compatible actor roll was deferred or replaced its instance")
	}
	node.Spec = *originalNode
	updateObject(t, ctx, c, &node)
	driveRoot(t, ctx, c, r, request, root)

	var stacker stacks.StacksStacker
	if err := c.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: "stacker-01"}, &stacker); err != nil {
		t.Fatal(err)
	}
	original := stacker.Spec.DeepCopy()
	prior := participant(t, ctx, c, root, "stacker-01").Status.Admission.DeepCopy()
	stacker.Spec.AmountMicroSTX = ptr.To(common.Amount("999999999999999"))
	updateObject(t, ctx, c, &stacker)
	driveRoot(t, ctx, c, r, request, root)
	deferred := participant(t, ctx, c, root, "stacker-01")
	requireReason(t, deferred, "BootstrapPending")
	if !reflect.DeepEqual(deferred.Status.Admission, prior) || !meta.IsStatusConditionTrue(deferred.Status.Conditions, "PolicyDeferred") {
		t.Fatal("conflicting amount did not retain the whole captured policy")
	}
	// Complete every earlier gate; the prepared waterfall set still depends on original enrollment.
	state := &api.InitializationStatus{GenesisUID: genesis.UID, GenesisDigest: root.Status.GenesisDigest, GateIndex: int32(len(genesis.Spec.Bootstrap.Gates) - 1), AuthorizedCeiling: genesis.Spec.Bootstrap.Gates[len(genesis.Spec.Bootstrap.Gates)-1].BitcoinCeiling}
	now := metav1.Now()
	for i, gate := range genesis.Spec.Bootstrap.Gates {
		observation := api.GateObservation{Name: gate.Name}
		if i < int(state.GateIndex) {
			observation.CompletedAt = &now
		}
		state.Gates = append(state.Gates, observation)
	}
	root.Status.Initialization = state
	if err := c.Status().Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	driveRoot(t, ctx, c, r, request, root)
	requireReason(t, participant(t, ctx, c, root, "stacker-01"), "BootstrapPending")
	// Installed manager routing must release the latest edit from a gate-status-only notification.
	verifyInstalledPolicyRelease(t, ctx, c, cfg, scheme, root, &stacker)
	admitted := participant(t, ctx, c, root, "stacker-01")
	requireReason(t, admitted, "Admitted")
	if *admitted.Status.Admission.Configuration.StacksStacker.AmountMicroSTX != *stacker.Spec.AmountMicroSTX || meta.IsStatusConditionTrue(admitted.Status.Conditions, "PolicyDeferred") || root.Status.GenesisRef.UID != genesis.UID || !root.Status.Initialization.Completed {
		t.Fatal("latest policy was not admitted after the last affected gate")
	}
	stacker.Spec = *original
	updateObject(t, ctx, c, &stacker)
	driveRoot(t, ctx, c, r, request, root)
	if !root.Status.Initialization.Completed || participant(t, ctx, c, root, "stacker-01").UID != admitted.UID {
		t.Fatal("post-initialization edit replayed bootstrap or replaced the instance")
	}
}

// verifyTrafficRecipientReplacement keeps both recipient forms protected while controls remain independent.
func verifyTrafficRecipientReplacement(t *testing.T, ctx context.Context, c client.Client, r *foundation.Reconciler, request ctrl.Request, root *api.StacksNetwork) {
	t.Helper()
	var traffic stacks.StacksTransactionProduction
	if err := c.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: "traffic"}, &traffic); err != nil {
		t.Fatal(err)
	}
	original := traffic.Spec.DeepCopy()
	prior := participant(t, ctx, c, root, "traffic").Status.Admission.DeepCopy()
	for _, recipient := range []*stacks.Recipient{{AccountRef: &common.NameRef{Name: "admin-01"}}, {Address: ptr.To("ST000000000000000000002AMW42H")}} {
		traffic.Spec.Recipient = recipient
		updateObject(t, ctx, c, &traffic)
		for i := range root.Spec.Participants {
			if root.Spec.Participants[i].Name == "traffic" {
				root.Spec.Participants[i].Control = &api.Control{Paused: ptr.To(recipient.AccountRef != nil)}
			}
		}
		updateObject(t, ctx, c, root)
		driveRoot(t, ctx, c, r, request, root)
		got := participant(t, ctx, c, root, "traffic")
		requireReason(t, got, "RequiresReplacement")
		if !reflect.DeepEqual(got.Status.Admission, prior) || got.Spec.Control == nil || ptr.Deref(got.Spec.Control.Paused, false) != (recipient.AccountRef != nil) {
			t.Fatal("recipient rejection changed admission or blocked controls")
		}
	}
	traffic.Spec = *original
	updateObject(t, ctx, c, &traffic)
	driveRoot(t, ctx, c, r, request, root)
}
