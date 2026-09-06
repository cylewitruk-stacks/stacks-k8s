//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyGenerationAdmission exercises generated structural and transition validation.
func verifyGenerationAdmission(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	a := &actionv1.BitcoinBlockGeneration{ObjectMeta: metav1.ObjectMeta{Name: "bounded-generation", Namespace: testNamespace}, Spec: actionv1.BitcoinBlockGenerationSpec{NetworkRef: actionv1.LocalReference{Name: "network"}, BitcoinNodeRef: actionv1.LocalReference{Name: "network-bitcoin"}, Count: 2, IntervalSeconds: 1, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn", Timeout: metav1.Duration{Duration: time.Minute}}}
	must(t, c.Create(ctx, a))
	for name, change := range map[string]func(*actionv1.BitcoinBlockGeneration){
		"count":    func(a *actionv1.BitcoinBlockGeneration) { a.Spec.Count = 3 },
		"target":   func(a *actionv1.BitcoinBlockGeneration) { a.Spec.BitcoinNodeRef.Name = "other" },
		"deadline": func(a *actionv1.BitcoinBlockGeneration) { a.Spec.Timeout.Duration = time.Second },
	} {
		t.Run("immutable action "+name, func(t *testing.T) {
			changed := a.DeepCopy()
			change(changed)
			if err := c.Update(ctx, changed); !apierrors.IsInvalid(err) {
				t.Fatalf("immutable spec accepted: %v", err)
			}
		})
	}
	for name, change := range map[string]func(*actionv1.BitcoinBlockGeneration){
		"zero-count":    func(a *actionv1.BitcoinBlockGeneration) { a.Spec.Count = 0 },
		"large-count":   func(a *actionv1.BitcoinBlockGeneration) { a.Spec.Count = 101 },
		"zero-interval": func(a *actionv1.BitcoinBlockGeneration) { a.Spec.IntervalSeconds = 0 },
		"long-interval": func(a *actionv1.BitcoinBlockGeneration) { a.Spec.IntervalSeconds = 61 },
		"zero-timeout":  func(a *actionv1.BitcoinBlockGeneration) { a.Spec.Timeout.Duration = 0 },
		"long-timeout":  func(a *actionv1.BitcoinBlockGeneration) { a.Spec.Timeout.Duration = 11 * time.Minute },
	} {
		t.Run("bounded action "+name, func(t *testing.T) {
			changed := a.DeepCopy()
			changed.Name = name
			changed.ResourceVersion = ""
			changed.UID = ""
			change(changed)
			if err := c.Create(ctx, changed); !apierrors.IsInvalid(err) {
				t.Fatalf("unbounded request accepted: %v", err)
			}
		})
	}
	a.Status.Phase = "Active"
	a.Status.BlocksGenerated = 1
	must(t, c.Status().Update(ctx, a))
	must(t, c.Get(ctx, client.ObjectKeyFromObject(a), a))
	if a.Status.BlocksGenerated != 1 {
		t.Fatal("receipt status pruned")
	}
	a.Status.Conditions = []metav1.Condition{{Type: "Invented", Status: metav1.ConditionTrue, Reason: "Test", Message: "invalid", LastTransitionTime: metav1.Now()}}
	if err := c.Status().Update(ctx, a); !apierrors.IsInvalid(err) {
		t.Fatalf("unknown condition accepted: %v", err)
	}
	a.Status.Conditions = nil
	a.Status.Phase = "Invented"
	if err := c.Status().Update(ctx, a); !apierrors.IsInvalid(err) {
		t.Fatalf("unknown phase accepted: %v", err)
	}
}
