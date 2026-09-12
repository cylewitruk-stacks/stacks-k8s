//go:build integration

package publicintegration

import (
	"context"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// verifyArtifactDeletionRetention exercises real finalized API deletion; envtest has no garbage collector.
func verifyArtifactDeletionRetention(t *testing.T, ctx context.Context, c client.Client, scheme *runtime.Scheme) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	ns := "shared-artifact-retention"
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}))
	policy := api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{}}
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: ns}, Spec: api.StacksNetworkSpec{Operation: "Running", Participants: []api.Participant{{Name: "btc", Kind: "BitcoinNode", Definition: api.Definition{Inline: &policy}}}}}
	must(c.Create(ctx, root))
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "btc-participant", Namespace: ns, Finalizers: []string{"test.example/consumer"}}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: root.UID, ParticipantName: "btc", Kind: "BitcoinNode", Configuration: policy}}
	must(controllerutil.SetControllerReference(root, p, scheme))
	must(c.Create(ctx, p))
	record := &bitcoin.BitcoinExecution{ObjectMeta: metav1.ObjectMeta{Name: "btc-execution", Namespace: ns, Finalizers: []string{foundation.ArtifactFinalizer, "test.example/evidence"}}, Spec: bitcoin.BitcoinExecutionSpec{NetworkUID: root.UID, Participant: common.Binding{Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID}}}
	must(controllerutil.SetControllerReference(root, record, scheme))
	must(c.Create(ctx, record))
	uid := record.UID
	must(c.Delete(ctx, record))
	must(c.Delete(ctx, p))
	r := foundation.ArtifactReconciler{Client: c, Reader: c, Object: &bitcoin.BitcoinExecution{}}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(record)}
	result, err := r.Reconcile(ctx, request)
	must(err)
	must(c.Get(ctx, request.NamespacedName, record))
	if result.RequeueAfter == 0 || record.UID != uid || record.DeletionTimestamp == nil || !controllerutil.ContainsFinalizer(record, foundation.ArtifactFinalizer) {
		t.Fatal("pending consumer did not retain exact deleting record")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
	p.Finalizers = nil
	must(c.Update(ctx, p))
	if err := c.Get(ctx, client.ObjectKeyFromObject(p), p); !apierrors.IsNotFound(err) {
		t.Fatalf("consumer still exists: %v", err)
	}
	_, err = r.Reconcile(ctx, request)
	must(err)
	must(c.Get(ctx, request.NamespacedName, record))
	if record.UID != uid || controllerutil.ContainsFinalizer(record, foundation.ArtifactFinalizer) || !controllerutil.ContainsFinalizer(record, "test.example/evidence") {
		t.Fatal("artifact identity or independent finalizer changed")
	}
	record.Finalizers = nil
	must(c.Update(ctx, record))
	if err := c.Get(ctx, request.NamespacedName, record); !apierrors.IsNotFound(err) {
		t.Fatalf("artifact deletion incomplete: %v", err)
	}
}
