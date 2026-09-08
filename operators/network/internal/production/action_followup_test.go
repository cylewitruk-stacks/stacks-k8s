package production

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// deleteWithGenerationChange supplies the finalized-delete generation behavior missing from the fake client.
func deleteWithGenerationChange(t *testing.T, f *productionFixture, a client.Object) {
	t.Helper()
	if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(a), a); err != nil {
		t.Fatal(err)
	}
	a.SetGeneration(a.GetGeneration() + 1)
	if err := f.r.Update(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Delete(context.Background(), a); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationIdleCancellationAcknowledgesBeforeRelease(t *testing.T) {
	for _, progress := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-arm", true: "between-receipts"}[progress], func(t *testing.T) {
			f := fixture(t)
			a := actionFixture(t, f, "cancel-idle", 3)
			admitAction(t, f, a)
			if progress {
				f.reconcile(t, context.Background())
				actionStep(t, f, a)
				if a.Status.BlocksGenerated != 1 {
					t.Fatal("between-receipts cancellation requires one acknowledged block")
				}
			}
			sends := f.rpc.sends
			deleteWithGenerationChange(t, f, a)
			f.reconcile(t, context.Background())
			if r := f.ledger(t).Status.Action; r == nil || r.StopReason != "ActionCancelled" {
				t.Fatal("cancellation lost its stop record")
			}
			f.reconcile(t, context.Background())
			if f.ledger(t).Status.Action == nil {
				t.Fatal("released before terminal acknowledgement")
			}
			actionStep(t, f, a)
			condition := meta.FindStatusCondition(a.Status.Conditions, "Progressing")
			if a.Status.Phase != "Failed" || condition == nil || condition.Reason != "ActionCancelled" || a.Status.BlocksGenerated != int32(sends) {
				t.Fatalf("incorrect cancelled evidence: %#v", a.Status)
			}
			f.reconcile(t, context.Background())
			if f.ledger(t).Status.Action != nil || f.rpc.sends != sends {
				t.Fatal("acknowledged cancellation retained or dispatched")
			}
		})
	}
}

func TestReorganizationCancellationAcknowledgesCompensationBeforeRelease(t *testing.T) {
	for _, progress := range []string{"before-arm", "after-invalidation", "between-receipts"} {
		t.Run(progress, func(t *testing.T) {
			f, rpc, a := reorgFixture(t)
			admitReorg(t, f, a)
			if progress != "before-arm" {
				f.reconcile(t, context.Background())
				reorgStep(t, f, a)
				if !a.Status.InvalidationAcknowledged {
					t.Fatal("cancellation requires acknowledged invalidation")
				}
			}
			if progress == "between-receipts" {
				f.now = f.now.Add(time.Second)
				f.reconcile(t, context.Background())
				reorgStep(t, f, a)
				if a.Status.BlocksGenerated != 1 {
					t.Fatal("between-receipts cancellation requires one acknowledged replacement")
				}
			}
			sends := f.rpc.sends
			deleteWithGenerationChange(t, f, a)
			f.reconcile(t, context.Background())
			if r := f.ledger(t).Status.Reorganization; r == nil || r.StopReason != "ActionCancelled" {
				t.Fatal("cancellation lost its stop record")
			}
			// Let the executor finish compensation while deliberately withholding lifecycle acknowledgement.
			for i := 0; i < 3; i++ {
				f.reconcile(t, context.Background())
			}
			r := f.ledger(t).Status.Reorganization
			if r == nil || (progress != "before-arm" && !r.CleanupAcknowledged) {
				t.Fatal("cleanup or acknowledgement gate lost")
			}
			reorgStep(t, f, a)
			c := meta.FindStatusCondition(a.Status.Conditions, "CleanupComplete")
			reason := meta.FindStatusCondition(a.Status.Conditions, "Progressing")
			if a.Status.Phase != "Failed" || reason.Reason != "ActionCancelled" || c.Status != metav1.ConditionTrue || a.Status.BlocksGenerated != int32(sends) {
				t.Fatalf("incorrect cancellation evidence: %#v", a.Status)
			}
			f.reconcile(t, context.Background())
			if f.ledger(t).Status.Reorganization != nil || f.rpc.sends != sends || rpc.cleanups != rpc.invalidations {
				t.Fatal("cancelled action did not release with exactly one compensation")
			}
		})
	}
}

// advanceReorganization pauses after the selected acknowledged execution boundary.
func advanceReorganization(t *testing.T, f *productionFixture, a *actionv1.BitcoinReorganization, done func(*bitcoinv1.ReorganizationReservation) bool) {
	t.Helper()
	for i := 0; i < 20; i++ {
		f.reconcile(t, context.Background())
		reorgStep(t, f, a)
		if done(f.ledger(t).Status.Reorganization) {
			return
		}
		f.now = f.now.Add(time.Second)
	}
	t.Fatal("execution boundary not reached")
}

func TestReorganizationAcknowledgedWorkSurvivesDelayedVerification(t *testing.T) {
	for _, boundary := range []string{"replacement-receipts", "cleanup-receipt", "cleanup-after-horizon", "suspended-after-cleanup"} {
		t.Run(boundary, func(t *testing.T) {
			f, rpc, a := reorgFixture(t)
			admitReorg(t, f, a)
			advanceReorganization(t, f, a, func(r *bitcoinv1.ReorganizationReservation) bool {
				if boundary == "replacement-receipts" {
					return r.BlocksGenerated == r.Spec.Depth+1
				}
				return r.CleanupAcknowledged
			})
			f.now = a.CreationTimestamp.Add(a.Spec.Timeout.Duration + time.Second)
			if boundary == "cleanup-after-horizon" {
				f.now = f.now.Add(time.Minute)
			}
			if boundary == "suspended-after-cleanup" {
				if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(f.parent), f.parent); err != nil {
					t.Fatal(err)
				}
				f.parent.Spec.Suspended = true
				if err := f.r.Update(context.Background(), f.parent); err != nil {
					t.Fatal(err)
				}
			}
			finishReorg(t, f, a)
			if a.Status.Phase != "Completed" || !a.Status.CleanupAcknowledged || a.Status.FinalChain == nil || rpc.cleanups != 1 || f.rpc.sends != 3 {
				t.Fatalf("acknowledged work misclassified: %#v", a.Status)
			}
		})
	}
}

func TestReorganizationUnavailableIdentityAfterCleanupPreservesCleanupFact(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed-runtime", true: "unavailable-runtime"}[unavailable], func(t *testing.T) {
			f, rpc, a := reorgFixture(t)
			admitReorg(t, f, a)
			advanceReorganization(t, f, a, func(r *bitcoinv1.ReorganizationReservation) bool { return r.CleanupAcknowledged })
			if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(f.pod), f.pod); err != nil {
				t.Fatal(err)
			}
			if unavailable {
				f.pod.Status.Conditions[0].Status = corev1.ConditionFalse
			} else {
				f.pod.Status.ContainerStatuses[0].ContainerID = "containerd://new"
			}
			if err := f.r.Status().Update(context.Background(), f.pod); err != nil {
				t.Fatal(err)
			}
			if unavailable {
				f.now = a.CreationTimestamp.Add(a.Spec.Timeout.Duration + time.Minute)
			}
			f.reconcile(t, context.Background())
			r := f.ledger(t).Status.Reorganization
			if r == nil || r.CleanupUnsafe || !r.CleanupAcknowledged {
				t.Fatal("acknowledged cleanup became unsafe")
			}
			reorgStep(t, f, a)
			if a.Status.Phase != "Inconclusive" || meta.FindStatusCondition(a.Status.Conditions, "CleanupComplete").Status != metav1.ConditionTrue || meta.FindStatusCondition(a.Status.Conditions, "Progressing").Reason != "IdentityDiverged" {
				t.Fatalf("incorrect final verification evidence: %#v", a.Status)
			}
			f.reconcile(t, context.Background())
			if f.ledger(t).Status.Reorganization != nil || rpc.cleanups != 1 {
				t.Fatal("known cleanup retained or repeated")
			}
		})
	}
}

func TestFinitePreflightReadFailureWaitsWithinDeadline(t *testing.T) {
	for _, outcome := range []string{"recover", "expire", "invalid"} {
		t.Run(outcome, func(t *testing.T) {
			f := fixture(t)
			a := actionFixture(t, f, "preflight", 2)
			admitAction(t, f, a)
			f.rpc.check = func(context.Context) error {
				if outcome == "invalid" {
					return errInvalidPreflight
				}
				return context.DeadlineExceeded
			}
			f.reconcile(t, context.Background())
			actionStep(t, f, a)
			if outcome == "invalid" {
				if a.Status.Phase != "Failed" {
					t.Fatal("definite preflight rejection did not stop")
				}
				return
			}
			if a.Status.Phase != "Admitted" || f.rpc.sends != 0 || f.ledger(t).Status.DispatchState == "Armed" {
				t.Fatal("read failure authorized or terminated work")
			}
			if outcome == "expire" {
				f.now = f.now.Add(2 * time.Minute)
			} else {
				f.rpc.check = nil
			}
			f.reconcile(t, context.Background())
			actionStep(t, f, a)
			if outcome == "expire" {
				if a.Status.Phase != "Failed" || f.rpc.sends != 0 {
					t.Fatal("preflight wait escaped deadline")
				}
			} else if a.Status.BlocksGenerated != 1 {
				t.Fatal("recovered preflight did not resume")
			}
		})
	}
}

func TestPreflightClassifiesOnlyDefiniteNegativeResults(t *testing.T) {
	for _, mode := range []string{"wrong-chain", "invalid-address", "unavailable", "malformed", "missing-chain", "missing-validation"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct{ ID, Method string }
				_ = json.NewDecoder(r.Body).Decode(&req)
				if mode == "unavailable" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if mode == "malformed" {
					_, _ = w.Write([]byte("not-json"))
					return
				}
				result := any(map[string]string{"chain": "regtest"})
				if mode == "wrong-chain" {
					result = map[string]string{"chain": "main"}
				} else if req.Method == "validateaddress" {
					result = map[string]bool{"isvalid": false}
				}
				if mode == "missing-chain" || mode == "missing-validation" && req.Method == "validateaddress" {
					result = map[string]any{}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "result": result})
			}))
			defer server.Close()
			err := NewBitcoinRPC(Credentials{}).Check(context.Background(), server.URL, "destination")
			if err == nil || errors.Is(err, errInvalidPreflight) != (mode == "wrong-chain" || mode == "invalid-address") {
				t.Fatalf("incorrect preflight classification: %v", err)
			}
		})
	}
}

func TestWaitingActionStatusReflectsReservationAndNoEffect(t *testing.T) {
	f, _, a := reorgFixture(t)
	admitReorg(t, f, a)
	queued := actionFixture(t, f, "queued", 1)
	actionStep(t, f, queued)
	if meta.FindStatusCondition(queued.Status.Conditions, "Progressing").Reason != "TargetBusy" {
		t.Fatal("held reorganization not reported busy")
	}
	f.now = f.now.Add(2 * time.Minute)
	reorgStep(t, f, a)
	if a.Status.Phase != "Admitted" || meta.FindStatusCondition(a.Status.Conditions, "EffectObserved").Status != metav1.ConditionFalse {
		t.Fatal("never-armed request reported recovery")
	}
}

// unavailablePodReader injects a read failure without changing admitted Kubernetes identity.
type unavailablePodReader struct {
	client.Reader
	// unavailable controls whether Pod reads fail during final admission.
	unavailable bool
}

// Get preserves all reads except the selected transient Pod failure.
func (r *unavailablePodReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.Pod); ok && r.unavailable {
		return errors.New("temporary API read failure")
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func TestReorganizationPostCleanupAdmissionReadWaitIsBounded(t *testing.T) {
	for _, recoverRead := range []bool{true, false} {
		t.Run(map[bool]string{true: "recovers", false: "expires"}[recoverRead], func(t *testing.T) {
			f, rpc, a := reorgFixture(t)
			admitReorg(t, f, a)
			advanceReorganization(t, f, a, func(r *bitcoinv1.ReorganizationReservation) bool { return r.CleanupAcknowledged })
			reader := &unavailablePodReader{Reader: f.r.APIReader, unavailable: true}
			f.r.APIReader = reader
			f.now = a.CreationTimestamp.Add(a.Spec.Timeout.Duration + 29*time.Second)
			f.reconcile(t, context.Background())
			reorgStep(t, f, a)
			record := f.ledger(t).Status.Reorganization
			if record == nil || record.StopReason != "" || record.CleanupUnsafe || !record.CleanupAcknowledged || a.Status.FinishedAt != nil || f.ledger(t).Status.Phase != "Reserved" {
				t.Fatal("transient admission failure terminated acknowledged cleanup")
			}
			if meta.FindStatusCondition(a.Status.Conditions, "CleanupComplete").Status != metav1.ConditionTrue {
				t.Fatal("acknowledged cleanup fact lost during read wait")
			}
			f.now = f.now.Add(time.Second)
			reader.unavailable = !recoverRead
			if recoverRead {
				finishReorg(t, f, a)
				if a.Status.Phase != "Completed" || a.Status.FinalChain == nil {
					t.Fatal("recovered final admission did not complete")
				}
			} else {
				f.reconcile(t, context.Background())
				record = f.ledger(t).Status.Reorganization
				if record == nil || record.StopReason != "IdentityDiverged" || record.CleanupUnsafe {
					t.Fatal("read wait did not stop at its fixed horizon with known cleanup")
				}
				reorgStep(t, f, a)
				if a.Status.Phase != "Inconclusive" || meta.FindStatusCondition(a.Status.Conditions, "CleanupComplete").Status != metav1.ConditionTrue {
					t.Fatal("exhausted verification lost known cleanup")
				}
				f.reconcile(t, context.Background())
				if f.ledger(t).Status.Reorganization != nil {
					t.Fatal("terminal acknowledgement did not release known cleanup")
				}
			}
			if rpc.invalidations != 1 || rpc.cleanups != 1 || f.rpc.sends != 3 {
				t.Fatal("post-cleanup admission wait repeated a mutation")
			}
		})
	}
}

func TestPendingReorganizationReportsBusyAcrossActionKinds(t *testing.T) {
	for _, kind := range []string{"generation", "reorganization"} {
		t.Run(kind, func(t *testing.T) {
			f, _, a := reorgFixture(t)
			if kind == "generation" {
				// Keep the pending reorganization ineligible until the generation reservation is admitted.
				f.r.ReorganizationEnabled = false
				g := actionFixture(t, f, "held-generation", 2)
				admitAction(t, f, g)
				f.r.ReorganizationEnabled = true
			} else {
				admitReorg(t, f, a)
				a = a.DeepCopy()
				a.Name = "pending-reorganization"
				a.UID = "pending-reorganization"
				a.ResourceVersion = ""
				a.Finalizers = nil
				a.Status = actionv1.BitcoinReorganizationStatus{}
				if err := f.r.Create(context.Background(), a); err != nil {
					t.Fatal(err)
				}
			}
			reorgStep(t, f, a)
			c := meta.FindStatusCondition(a.Status.Conditions, "Progressing")
			if a.Status.Phase != "Pending" || c == nil || c.Reason != "TargetBusy" {
				t.Fatalf("held %s not reported busy: %#v", kind, a.Status)
			}
		})
	}
}

func TestPostCleanupReadWaitUsesOneDeadlineDecision(t *testing.T) {
	f, _, a := reorgFixture(t)
	admitReorg(t, f, a)
	advanceReorganization(t, f, a, func(r *bitcoinv1.ReorganizationReservation) bool { return r.CleanupAcknowledged })
	f.r.APIReader = &unavailablePodReader{Reader: f.r.APIReader, unavailable: true}
	instant := a.CreationTimestamp.Add(a.Spec.Timeout.Duration + 30*time.Second - time.Nanosecond)
	f.r.Now = func() time.Time { now := instant; instant = instant.Add(time.Nanosecond); return now }
	f.reconcile(t, context.Background())
	record := f.ledger(t).Status.Reorganization
	if record == nil || record.StopReason != "" || record.CleanupUnsafe || !record.CleanupAcknowledged {
		t.Fatal("crossing the horizon between clock reads misclassified acknowledged cleanup")
	}
}
