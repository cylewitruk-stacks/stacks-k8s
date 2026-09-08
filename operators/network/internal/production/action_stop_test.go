package production

import (
	"context"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/action/controllers/generation"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// finiteArmWriteClient delays authorization before its API-server commit.
type finiteArmWriteClient struct {
	client.Client
	// entered signals that the authorization is pending.
	entered chan struct{}
	// release permits the delayed compare-and-swap.
	release chan struct{}
}

// Status wraps only status writes for this race fixture.
func (c *finiteArmWriteClient) Status() client.SubResourceWriter {
	return &finiteArmWriteStatus{SubResourceWriter: c.Client.Status(), owner: c}
}

// finiteArmWriteStatus preserves ordinary API behavior after the injected delay.
type finiteArmWriteStatus struct {
	client.SubResourceWriter
	// owner supplies the test synchronization boundary.
	owner *finiteArmWriteClient
}

// Patch delays an Armed write without changing its original resource version.
func (w *finiteArmWriteStatus) Patch(ctx context.Context, o client.Object, p client.Patch, options ...client.SubResourcePatchOption) error {
	if ledger, ok := o.(*bitcoinv1.BitcoinProductionTarget); ok && ledger.Status.DispatchState == "Armed" {
		close(w.owner.entered)
		<-w.owner.release
	}
	return w.SubResourceWriter.Patch(ctx, o, p, options...)
}

func TestDeadlineWithdrawalConflictsWithDelayedArmAcrossBothKinds(t *testing.T) {
	for _, kind := range []string{"generation", "reorganization"} {
		t.Run(kind, func(t *testing.T) {
			f, rpc, reorg := reorgFixture(t)
			var generationAction *actionv1.BitcoinBlockGeneration
			if kind == "generation" {
				if err := f.r.Delete(context.Background(), reorg); err != nil {
					t.Fatal(err)
				}
				generationAction = actionFixture(t, f, "generation", 2)
				admitAction(t, f, generationAction)
			} else {
				admitReorg(t, f, reorg)
			}
			original := f.r.Client
			blocked := &finiteArmWriteClient{Client: original, entered: make(chan struct{}), release: make(chan struct{})}
			f.r.Client = blocked
			finished := make(chan error, 1)
			go func() {
				_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
				finished <- err
			}()
			<-blocked.entered
			f.now = f.now.Add(2 * time.Minute)
			if kind == "generation" {
				actionStep(t, f, generationAction)
				if generation.Terminal(generationAction.Status.Phase) {
					t.Fatal("deadline claimed terminal before executor withdrawal")
				}
			} else {
				reorgStep(t, f, reorg)
				if generation.Terminal(reorg.Status.Phase) {
					t.Fatal("deadline claimed cleanup before executor withdrawal")
				}
			}
			withdrawing := *f.r
			withdrawing.Client = original
			if _, err := withdrawing.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)}); err != nil {
				t.Fatal(err)
			}
			close(blocked.release)
			if err := <-finished; !apierrors.IsConflict(err) {
				t.Fatalf("delayed authorization escaped stop CAS: %v", err)
			}
			f.r.Client = original
			if kind == "generation" {
				actionStep(t, f, generationAction)
				if generationAction.Status.Phase != "Failed" {
					t.Fatal("withdrawn generation did not fail cleanly")
				}
			} else {
				reorgStep(t, f, reorg)
				if reorg.Status.Phase != "Failed" {
					t.Fatal("withdrawn reorganization did not fail cleanly")
				}
			}
			if rpc.invalidations != 0 || f.rpc.sends != 0 || f.ledger(t).Status.DispatchState == "Armed" {
				t.Fatal("delayed authorization sent after safe stop")
			}
		})
	}
}
