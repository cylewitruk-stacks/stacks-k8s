package productionscheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// jitterPolicy applies the same parent and compiled policy with real generation changes.
func jitterPolicy(t *testing.T, f *schedulerFixture, interval, jitter int32, paused bool) {
	t.Helper()
	ctx := context.Background()
	f.p.Spec.Policy.IntervalSeconds, f.p.Spec.Policy.JitterSeconds, f.p.Spec.Policy.Paused = interval, jitter, paused
	f.p.Generation++
	f.n.Spec.BitcoinBlockProduction = f.p.Spec.Policy.DeepCopy()
	if err := f.r.Update(ctx, f.n); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Update(ctx, f.p); err != nil {
		t.Fatal(err)
	}
}

func TestJitterPersistsWithoutChangingWeightedSelection(t *testing.T) {
	f := scheduler(t)
	jitterPolicy(t, f, 2, 1, false)
	f.now = f.now.Add(123456789 * time.Nanosecond)
	f.step(t)
	seen := map[time.Duration]bool{}
	for i := 0; i < 100; i++ {
		due := f.p.Status.NextOpportunityAt.DeepCopy()
		delay := due.Sub(f.now)
		if delay < time.Second || delay >= 3*time.Second+time.Microsecond {
			t.Fatal(delay)
		}
		seen[delay.Truncate(time.Second)] = true
		f.now = due.Add(-time.Nanosecond)
		f.step(t)
		if f.p.Status.Opportunities != int64(i) || !f.p.Status.NextOpportunityAt.Equal(due) {
			t.Fatal("timer redrawn before due")
		}
		f.now = due.Time
		f.draw = i % 4
		f.step(t)
		selected := "b"
		if f.draw == 0 {
			selected = "a"
		}
		if !f.p.Ledger(selected).Opportunity.ExpiresAt.Equal(f.p.Status.NextOpportunityAt) {
			t.Fatal("offer expiry is not the sampled next deadline")
		}
	}
	if len(seen) != 3 || f.p.Ledger("a").Offered != 25 || f.p.Ledger("b").Offered != 75 {
		t.Fatal("jitter altered target draws or omitted interval endpoints")
	}
}

func TestJitterLostWriteAndRestartKeepCommittedDeadline(t *testing.T) {
	for _, initial := range []bool{false, true} {
		t.Run(fmt.Sprint(initial), func(t *testing.T) {
			f := scheduler(t)
			jitterPolicy(t, f, 5, 3, false)
			if !initial {
				f.step(t)
				f.now = f.p.Status.NextOpportunityAt.Time
			}
			var stored *metav1.MicroTime
			f.r.Client = interceptor.NewClient(f.r.Client.(client.WithWatch), interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, o client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				err := c.SubResource(sub).Patch(ctx, o, patch, opts...)
				if p, ok := o.(*bitcoinv1.BitcoinBlockProduction); ok && err == nil && stored == nil {
					stored = p.Status.NextOpportunityAt.DeepCopy()
					return fmt.Errorf("lost scheduler write acknowledgement")
				}
				return err
			}})
			if _, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.p)}); err == nil || stored == nil {
				t.Fatal("lost write not exercised")
			}
			old := f.r
			f.r = &Reconciler{Client: old.Client, APIReader: old.APIReader, Now: func() time.Time { return f.now }, Draw: func(int) int { t.Fatal("restart redrew committed selection"); return 0 }}
			f.step(t)
			expected := int64(1)
			if initial {
				expected = 0
			}
			if f.p.Status.Opportunities != expected || !f.p.Status.NextOpportunityAt.Equal(stored) {
				t.Fatal("restart redrew committed deadline")
			}
			f.r.Draw = func(int) int { return 3 }
			f.now = stored.Add(time.Hour)
			f.step(t)
			f.step(t)
			if f.p.Status.Opportunities != expected+1 || f.p.Status.NextOpportunityAt.Sub(f.now) < 2*time.Second {
				t.Fatal("restart caused catch-up or shortened cadence")
			}
		})
	}
}

func TestJitterPolicyUpdateAndResumeStartFreshInterval(t *testing.T) {
	f := scheduler(t)
	jitterPolicy(t, f, 2, 1, false)
	f.step(t)
	f.now = f.p.Status.NextOpportunityAt.Time
	f.step(t)
	jitterPolicy(t, f, 20, 5, false)
	f.step(t)
	if f.p.Status.NextOpportunityAt.Sub(f.now) < 15*time.Second || f.p.Status.Opportunities != 1 {
		t.Fatal("policy update retained obsolete due time")
	}
	jitterPolicy(t, f, 20, 5, true)
	f.step(t)
	if f.p.Status.NextOpportunityAt != nil {
		t.Fatal("pause retained pending timer")
	}
	f.now = f.now.Add(time.Hour)
	jitterPolicy(t, f, 20, 5, false)
	f.step(t)
	if f.p.Status.NextOpportunityAt.Sub(f.now) < 15*time.Second || f.p.Status.Opportunities != 1 {
		t.Fatal("resume replayed missed intervals")
	}
}

func TestSuspensionResumeReusesPendingSampleWithFreshAnchor(t *testing.T) {
	f := scheduler(t)
	ctx := context.Background()
	jitterPolicy(t, f, 5, 3, false)
	f.step(t)
	f.now = f.p.Status.NextOpportunityAt.Time
	f.step(t)
	delay := f.p.Status.NextOpportunityAt.Sub(f.now)
	generation, offered := f.p.Generation, f.p.Status.Opportunities
	f.n.Spec.Suspended = true
	if err := f.r.Update(ctx, f.n); err != nil {
		t.Fatal(err)
	}
	f.step(t)
	if f.p.Status.NextOpportunityAt != nil {
		t.Fatal("suspension retained a due time")
	}
	f.now = f.now.Add(time.Hour)
	f.n.Spec.Suspended = false
	if err := f.r.Update(ctx, f.n); err != nil {
		t.Fatal(err)
	}
	f.step(t)
	due := f.p.Status.NextOpportunityAt.DeepCopy()
	if f.p.Generation != generation || f.p.Status.Opportunities != offered || due.Sub(f.now) != delay {
		t.Fatal("resume changed the pending sample or issued catch-up work")
	}
	f.step(t)
	if !f.p.Status.NextOpportunityAt.Equal(due) {
		t.Fatal("resume retry moved its committed anchor")
	}
	f.now = due.Time
	f.step(t)
	if f.p.Status.Opportunities != offered+1 {
		t.Fatal("resumed opportunity did not advance")
	}
}
