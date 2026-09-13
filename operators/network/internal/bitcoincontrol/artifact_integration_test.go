//go:build integration

package bitcoincontrol

import (
	"context"
	"strings"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// TestSharedArtifactEarlyDeletionRemainsWritable models GC deletion requests; envtest has no garbage collector.
func TestSharedArtifactEarlyDeletionRemainsWritable(t *testing.T) {
	c := controlIntegrationClient(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, propagation := range []metav1.DeletionPropagation{
		metav1.DeletePropagationBackground,
		metav1.DeletePropagationForeground,
	} {
		t.Run(string(propagation), func(t *testing.T) {
			namespace := "artifact-" + strings.ToLower(string(propagation))
			must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
			root := &api.StacksNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "network",
					Namespace:  namespace,
					Finalizers: []string{"network.stacks.org/foundation"},
				},
				Spec: api.StacksNetworkSpec{Operation: "Stopped"},
			}
			must(c.Create(ctx, root))
			p := &api.StacksNetworkParticipant{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "core",
					Namespace:  namespace,
					Finalizers: []string{"test.example/dispose"},
				},
				Spec: api.StacksNetworkParticipantSpec{
					NetworkUID:      root.UID,
					ParticipantName: "core",
					Kind:            "BitcoinNode",
					Configuration:   api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{}},
				},
			}
			must(controllerutil.SetControllerReference(root, p, c.Scheme()))
			must(c.Create(ctx, p))
			//nolint:contextcheck // Fixture reads use testing.T.Context, independent of the exercised operation deadline.
			f := baselineFixture(t)
			execution := &bitcoin.BitcoinExecution{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "execution",
					Namespace:  namespace,
					Finalizers: []string{foundation.ArtifactFinalizer},
				},
				Spec: bitcoin.BitcoinExecutionSpec{
					NetworkUID:  root.UID,
					Participant: binding("StacksNetworkParticipant", p),
				},
			}
			initial := &bitcoin.BitcoinInitialization{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "initialization",
					Namespace:  namespace,
					Finalizers: []string{foundation.ArtifactFinalizer},
				},
				Spec: *f.initial.Spec.DeepCopy(),
			}
			initial.Spec.NetworkUID = root.UID
			objects := []client.Object{execution, initial}
			for _, o := range objects {
				must(controllerutil.SetControllerReference(root, o, c.Scheme()))
				must(c.Create(ctx, o))
			}
			must(c.Delete(ctx, root, client.PropagationPolicy(propagation)))
			// Explicit API Delete represents the early descendant deletion that real foreground GC may request.
			for _, o := range objects {
				must(c.Delete(ctx, o))
				must(c.Get(ctx, client.ObjectKeyFromObject(o), o))
				if o.GetDeletionTimestamp() == nil {
					t.Fatal("missing deletion timestamp")
				}
			}
			execution.Status.Phase = "Abandoned"
			must(c.Status().Update(ctx, execution))
			initial.Status.Reason = "CleanupObserved"
			must(c.Status().Update(ctx, initial))
			for _, o := range objects {
				reconciler := foundation.ArtifactReconciler{Client: c, Reader: c, Object: o}
				result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(o)})
				must(err)
				if result.RequeueAfter == 0 {
					t.Fatal("artifact released before consumer disposal")
				}
			}
			must(c.Delete(ctx, p))
			must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
			p.Finalizers = nil
			must(c.Update(ctx, p))
			for _, o := range objects {
				reconciler := foundation.ArtifactReconciler{Client: c, Reader: c, Object: o}
				_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(o)})
				must(err)
				if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); !apierrors.IsNotFound(err) {
					t.Fatalf("artifact retained after consumer disappeared: %v", err)
				}
			}
		})
	}
}
