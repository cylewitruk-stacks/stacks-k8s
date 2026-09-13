package bitcoincontrol

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// overrideFixture intercepts only status transport; API ownership is verified by envtest.
func overrideFixture(t *testing.T) (*testFixture, *Scheduler, *OverrideReconciler) {
	t.Helper()
	f := baselineFixture(t)
	f.c = interceptor.NewClient(
		f.c.(client.WithWatch),
		interceptor.Funcs{
			SubResourcePatch: func(
				ctx context.Context,
				c client.Client,
				sub string,
				obj client.Object,
				patch client.Patch,
				opts ...client.SubResourcePatchOption,
			) error {
				request, ok := obj.(*bitcoin.BitcoinBlockScheduleOverride)
				if !ok {
					return c.SubResource(sub).Patch(ctx, obj, patch, opts...)
				}
				current := &bitcoin.BitcoinBlockScheduleOverride{}
				if err := c.Get(ctx, client.ObjectKeyFromObject(request), current); err != nil {
					return err
				}
				current.Status = request.Status
				if err := c.Update(ctx, current); err != nil {
					return err
				}
				*request = *current
				return nil
			},
		},
	)
	f.worker.Client = f.c
	f.worker.Reader = f.c
	return f, f.schedulerFor(), &OverrideReconciler{
		Client: f.c,
		Reader: f.c,
		Now:    func() time.Time { return f.now },
	}
}

// newOverride fixes a public request to this test's current production incarnation.
func newOverride(f *testFixture, name, interval, duration string) *bitcoin.BitcoinBlockScheduleOverride {
	return &bitcoin.BitcoinBlockScheduleOverride{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         f.root.Namespace,
			CreationTimestamp: metav1.NewTime(f.now),
		},
		Spec: bitcoin.BitcoinBlockScheduleOverrideSpec{
			NetworkUID:    f.root.UID,
			ProductionRef: common.NameRef{Name: "production"},
			Schedule: &bitcoin.BitcoinBlockScheduleSpec{
				Cadence: bitcoin.Cadence{Mode: "Fixed", Interval: ptr.To(common.Duration(interval))},
			},
			Duration: common.Duration(duration),
		},
	}
}

func reconcileOverrideRequest(t *testing.T, r *OverrideReconciler, request *bitcoin.BitcoinBlockScheduleOverride) {
	t.Helper()
	if _, err := r.Reconcile(
		context.Background(),
		ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)},
	); err != nil {
		t.Fatal(err)
	}
}

func TestOverrideDeterministicContentionExpiresPendingWhileActive(t *testing.T) {
	f, s, r := overrideFixture(t)
	ctx := context.Background()
	later := newOverride(f, "later", "10s", "10m")
	older := newOverride(f, "older", "20s", "10m")
	older.CreationTimestamp = metav1.NewTime(f.now.Add(-time.Second))
	for _, request := range []*bitcoin.BitcoinBlockScheduleOverride{later, older} {
		if err := f.c.Create(ctx, request); err != nil {
			t.Fatal(err)
		}
		reconcileOverrideRequest(t, r, request)
	}
	f.reconcile(t, s)
	reconcileOverrideRequest(t, r, older)
	state := f.readInitial(t).Status.Override
	if state == nil || state.Override.UID != older.UID {
		t.Fatal("contention did not choose oldest eligible override", state)
	}
	f.now = f.now.Add(6 * time.Minute)
	reconcileOverrideRequest(t, r, later)
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(later), later); err != nil {
		t.Fatal(err)
	}
	if later.Status.Phase != "Expired" || f.readInitial(t).Status.Override.Override.UID != older.UID {
		t.Fatal("pending deadline waited for active override or preempted it")
	}
}

func TestOverrideLostActivationPublicationAndPauseResumeLatestBaseline(t *testing.T) {
	f, s, r := overrideFixture(t)
	ctx := context.Background()
	request := newOverride(f, "slow", "20s", "2m")
	if err := f.c.Create(ctx, request); err != nil {
		t.Fatal(err)
	}
	reconcileOverrideRequest(t, r, request)
	lost := false
	s.Client = interceptor.NewClient(
		f.c.(client.WithWatch),
		interceptor.Funcs{
			SubResourceUpdate: func(
				ctx context.Context,
				c client.Client,
				sub string,
				obj client.Object,
				opts ...client.SubResourceUpdateOption,
			) error {
				err := c.SubResource(sub).Update(ctx, obj, opts...)
				if initial, ok := obj.(*bitcoin.BitcoinInitialization); ok && !lost && err == nil &&
					initial.Status.Override != nil {
					lost = true
					return errors.New("activation response lost")
				}
				return err
			},
		},
	)
	if _, err := s.Reconcile(
		ctx,
		ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.initial)},
	); err == nil ||
		!lost {
		t.Fatal("lost activation boundary not exercised")
	}
	original := f.readInitial(t).Status.Override.DeepCopy()
	reconcileOverrideRequest(t, r, request)
	for range 2 {
		f.reconcile(t, s)
	}
	if got := f.readInitial(
		t,
	).Status; got.NextOpportunityAt == nil || !got.NextOpportunityAt.Time.Equal(f.now.Add(20*time.Second)) ||
		!got.Override.StartedAt.Equal(&original.StartedAt) {
		t.Fatal("lost activation restarted duration or ignored cadence", got)
	}
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.production), f.production); err != nil {
		t.Fatal(err)
	}
	f.production.Status.Admission.Configuration.BitcoinBlockProduction.Schedule.Cadence.Interval = ptr.To(
		common.Duration("3s"),
	)
	f.production.Status.Admission.PolicyDigest = foundation.Digest(f.production.Status.Admission.Configuration)
	if err := f.c.Status().Update(ctx, f.production); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.root), f.root); err != nil {
		t.Fatal(err)
	}
	f.root.Spec.Operation = "Paused"
	if err := f.c.Update(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, s)
	f.now = f.now.Add(121 * time.Second)
	reconcileOverrideRequest(t, r, request)
	f.reconcile(t, s)
	reconcileOverrideRequest(t, r, request)
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(request), request); err != nil {
		t.Fatal(err)
	}
	if request.Status.Phase != "Completed" || f.readInitial(t).Status.Override != nil ||
		!request.Status.Admission.StartedAt.Equal(&original.StartedAt) {
		t.Fatal("pause extended active wall-clock expiry", request.Status)
	}
	f.root.Spec.Operation = "Running"
	if err := f.c.Update(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		f.reconcile(t, s)
	}
	if got := f.readInitial(
		t,
	).Status; got.NextOpportunityAt == nil || !got.NextOpportunityAt.Time.Equal(f.now.Add(3*time.Second)) ||
		got.Baseline.Scheduling.Override != nil {
		t.Fatal("override restored stale baseline or caught up", got)
	}
}

func TestOverridePendingWithoutNetworkExpiresAndDeletionWithdrawsTiming(t *testing.T) {
	f, s, r := overrideFixture(t)
	ctx := context.Background()
	wrong := newOverride(f, "wrong-network", "1s", "1m")
	wrong.Spec.NetworkUID = "unavailable-network"
	wrong.CreationTimestamp = metav1.NewTime(f.now.Add(-6 * time.Minute))
	if err := f.c.Create(ctx, wrong); err != nil {
		t.Fatal(err)
	}
	reconcileOverrideRequest(t, r, wrong)
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(wrong), wrong); err != nil {
		t.Fatal(err)
	}
	if wrong.Status.Phase != "Expired" {
		t.Fatal("unavailable network prevented pending expiry")
	}
	request := newOverride(f, "cancel", "1s", "1m")
	if err := f.c.Create(ctx, request); err != nil {
		t.Fatal(err)
	}
	reconcileOverrideRequest(t, r, request)
	f.reconcile(t, s)
	reconcileOverrideRequest(t, r, request)
	if err := f.c.Delete(ctx, request); err != nil {
		t.Fatal(err)
	}
	reconcileOverrideRequest(t, r, request)
	f.reconcile(t, s)
	if f.readInitial(t).Status.Override != nil || f.readInitial(t).Status.Offer != nil {
		t.Fatal("deletion retained temporary send authority")
	}
}

func TestOverrideExpiryOutcomeSurvivesSchedulerFirstOrdering(t *testing.T) {
	f, s, r := overrideFixture(t)
	ctx := context.Background()
	request := newOverride(f, "expiry-order", "10s", "1m")
	if err := f.c.Create(ctx, request); err != nil {
		t.Fatal(err)
	}
	reconcileOverrideRequest(t, r, request)
	f.reconcile(t, s)
	reconcileOverrideRequest(t, r, request)
	active := f.readInitial(t).Status.Override.DeepCopy()
	if active == nil {
		t.Fatal("override not activated")
	}
	f.now = active.ExpiresAt.Add(time.Second)
	f.reconcile(t, s)
	if got := f.readInitial(t).Status.Override; got == nil || !reflect.DeepEqual(got, active) {
		t.Fatal("expiry evidence withdrawn before terminal publication")
	}
	initial := f.readInitial(t)
	if err := f.worker.authorizeTimingOverride(
		ctx,
		admitted{root: f.root, initialization: initial},
		&bitcoin.BitcoinBlockOffer{Production: active.Production, Override: &active.Override},
	); err == nil {
		t.Fatal("retained expiry evidence authorized dispatch")
	}
	reconcileOverrideRequest(t, r, request)
	f.reconcile(t, s)
	reconcileOverrideRequest(t, r, request)
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(request), request); err != nil {
		t.Fatal(err)
	}
	if request.Status.Phase != "Completed" || request.Status.Reason != "DurationElapsed" ||
		request.Status.Admission == nil ||
		!request.Status.Admission.StartedAt.Equal(&active.StartedAt) ||
		f.readInitial(t).Status.Override != nil {
		t.Fatalf("expiry lost or reclassified: %+v", request.Status)
	}
}
