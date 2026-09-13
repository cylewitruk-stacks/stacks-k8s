//go:build live

package integration

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// producerPod resolves the one production worker in the administrator-only fixture.
func (f *faultFixture) producerPod() *corev1.Pod {
	f.t.Helper()
	pods := &corev1.PodList{}
	if err := f.admin.List(
		f.ctx,
		pods,
		client.InNamespace(f.namespace),
		client.MatchingLabels{"app.kubernetes.io/name": "bitcoin-production"},
	); err != nil {
		f.t.Fatal(err)
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning {
			return pod.DeepCopy()
		}
	}
	return nil
}

// controlFault deliberately requires administrator authority outside the public actor-only profile.
func (f *faultFixture) controlFault(name, duration string) *unstructured.Unstructured {
	fault := f.partition(name, "unused", "bitcoin", duration)
	_ = unstructured.SetNestedStringMap(
		fault.Object,
		map[string]string{"app.kubernetes.io/name": "bitcoin-production"},
		"spec",
		"selector",
		"labelSelectors",
	)
	return fault
}

// TestLiveProducerControlLoss distinguishes preflight failure from a lost in-flight receipt.
// The receipt-delay image is a test-only proxy around real Core, not a runtime adapter.
func TestLiveProducerControlLoss(t *testing.T) {
	namespace := os.Getenv("STACKS_CHAOS_CONTROL_NAMESPACE")
	if namespace == "" {
		namespace = "partition-control"
	}
	f := newFaultFixture(t, namespace, "control", false)
	f.pod("bitcoin")
	ledger := func() *unstructured.Unstructured {
		return f.object("bitcoin.stacks.org", "BitcoinProductionTarget", "control-bitcoin")
	}
	state := func(o *unstructured.Unstructured, key string) string {
		s, _, _ := unstructured.NestedString(o.Object, "status", key)
		return s
	}
	if height, _ := f.chain(
		"bitcoin",
	); height != 0 || f.receipts("bitcoin") != 0 ||
		state(ledger(), "dispatchID") != "" {
		t.Fatal("control test requires a fresh unused receipt-delay fixture")
	}
	f.wait("ready producer", 30*time.Second, func() bool { return f.producerPod() != nil })
	preflight := f.controlFault("control-before-dispatch", "60s")
	f.inject(preflight)
	f.pause(false)
	// Unavailable combines target admission and RPC preflight failure. This proves
	// refusal before dispatch, not which check failed; the status message cannot distinguish them.
	f.wait("unavailable target opportunity skipped", 25*time.Second, func() bool {
		current := ledger()
		if state(current, "dispatchID") != "" || f.receipts("bitcoin") != 0 {
			t.Fatal("unavailable target authorized a mutation")
		}
		return state(current, "lastSkipReason") == "Unavailable"
	})
	if height, _ := f.chain("bitcoin"); height != 0 {
		t.Fatal("Core mutated while preflight was unavailable")
	}
	t.Log("control loss before dispatch: no authorization, receipt, or Core block")
	f.recover(preflight, false)
	// The proxy logs only after real Core returned its first successful generation receipt.
	f.wait("real receipt held by test proxy", 25*time.Second, func() bool {
		ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
		defer cancel()
		output, err := f.command(ctx, "logs", "control-bitcoin-0", "--tail=100").Output()
		if err != nil {
			t.Fatal("fixture log unavailable")
		}
		return strings.Contains(string(output), "Bitcoin returned generation receipt; delaying delivery for 15 seconds")
	})
	old := f.producerPod()
	if old == nil {
		t.Fatal("producer absent before receipt loss")
	}
	armed := ledger()
	dispatch := state(armed, "dispatchID")
	if dispatch == "" || state(armed, "dispatchState") != "Armed" {
		t.Fatal("receipt not tied to an Armed dispatch")
	}
	fault := f.controlFault("control-inflight-receipt", "120s")
	f.inject(fault)
	if state(ledger(), "dispatchState") != "Armed" || f.receipts("bitcoin") != 0 {
		t.Fatal("receipt arrived before control loss")
	}
	// Capture process-local drain evidence before the old Pod object disappears.
	logctx, stopLogs := context.WithTimeout(f.ctx, 70*time.Second)
	defer stopLogs()
	logCommand := f.command(logctx, "logs", "--follow", old.Name, "--tail=100")
	var drainLog bytes.Buffer
	logCommand.Stdout = &drainLog
	if err := logCommand.Start(); err != nil {
		t.Fatal(err)
	}
	// A normal Pod deletion allows the existing bounded drain to run; no forced kill or status edit.
	if err := f.admin.Delete(f.ctx, old); err != nil {
		t.Fatal(err)
	}
	f.wait("old producer termination and replacement", 65*time.Second, func() bool {
		if err := f.admin.Get(f.ctx, client.ObjectKeyFromObject(old), &corev1.Pod{}); !apierrors.IsNotFound(err) {
			return false
		}
		replacement := f.producerPod()
		return replacement != nil && replacement.UID != old.UID
	})
	replacement := f.producerPod()
	if err := logCommand.Wait(); err != nil {
		t.Fatalf("drain log stream: %v", err)
	}
	if !strings.Contains(drainLog.String(), "Bitcoin receipt drain exhausted; unresolved ledgers remain closed") ||
		!strings.Contains(drainLog.String(), `"pending":1`) {
		t.Fatal("old process did not report bounded drain exhaustion with an outstanding collector")
	}
	// A Running replacement may still be waiting for the prior leader lease to expire.
	f.wait(
		"unresolved replacement stays closed",
		60*time.Second,
		func() bool { return state(ledger(), "phase") == "Blocked" },
	)
	if !f.condition(fault.GetName(), "AllInjected") || f.condition(fault.GetName(), "AllRecovered") {
		t.Fatal("control loss expired before restart evidence")
	}
	f.recover(fault, false)
	// Restoration permits observation, but does not turn missing receipt evidence into dispatch authority.
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		current := ledger()
		if state(current, "dispatchID") != dispatch || state(current, "dispatchState") != "Armed" ||
			state(current, "phase") != "Blocked" ||
			f.receipts("bitcoin") != 0 {
			t.Fatal("ambiguous dispatch was reopened or incorrectly accounted")
		}
		if height, _ := f.chain("bitcoin"); height != 1 {
			t.Fatal("expected exactly one real Core mutation and no retry")
		}
		time.Sleep(time.Second)
	}
	f.pod("bitcoin")
	t.Logf(
		"producer UID=%s -> %s; dispatchID=%s remains Armed/Blocked; Core blocks=1, receipts=0 after restoration",
		old.UID,
		replacement.UID,
		dispatch,
	)
}
