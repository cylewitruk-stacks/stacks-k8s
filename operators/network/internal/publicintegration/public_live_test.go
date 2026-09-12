//go:build live

// Package publicintegration qualifies the replacement API through public declarations and observations.
package publicintegration

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestPublicBaselineLifecycle is opt-in and never adopts an existing namespace or environment.
func TestPublicBaselineLifecycle(t *testing.T) {
	if os.Getenv("STACKS_PUBLIC_WORKER_EVICTION") == "1" {
		t.Skip("worker eviction qualifies a terminal Failed fixture separately")
	}
	if os.Getenv("STACKS_PUBLIC_LIVE") != "1" {
		t.Skip("set STACKS_PUBLIC_LIVE=1 with explicit cluster, fresh namespace and image inputs")
	}
	config, err := readConfig()
	if err != nil {
		t.Fatal(err)
	}
	declared, err := loadFixture(config.fixtureOptions)
	if err != nil {
		t.Fatal(err)
	}
	h, err := newHarness(t, config, declared)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("public qualification context=%s namespace=%s variant=%s cadence=%s evidence=%s", config.kubecontext, config.namespace, config.variant, config.cadence, h.evidence)
	t.Cleanup(func() {
		if err := h.cleanup(); err != nil {
			t.Errorf("bounded cleanup incomplete for namespace %s UID=%s: %v; public evidence=%s", config.namespace, h.namespaceUID, err, h.evidence)
		}
	})
	signals, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithTimeout(signals, config.timeout)
	defer cancel()
	if err := h.create(ctx); err != nil {
		t.Fatal(err)
	}
	initialized, err := h.wait(ctx, "initialized", config.timeout, true, func(s snapshot) (bool, error) {
		_, ready := progress(s)
		return condition(s, "Initialized", metav1.ConditionTrue) && condition(s, "Operational", metav1.ConditionTrue) && ready, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	progressing, err := h.awaitProgress(ctx, "native-progress", initialized)
	if err != nil {
		t.Fatal(err)
	}
	if config.operatorName != "" {
		progressing, err = h.restartOperator(ctx, progressing)
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("STACKS_PUBLIC_ACTORS") == "1" && os.Getenv("STACKS_PUBLIC_ACTORS_FIRST") == "1" {
		progressing, err = h.qualifyActors(ctx, progressing)
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("STACKS_PUBLIC_RENEWAL") == "1" {
		progressing, err = h.qualifyRenewal(ctx, progressing)
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("STACKS_PUBLIC_REJECTIONS") == "1" {
		progressing, err = h.qualifyTrafficRejection(ctx, progressing)
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("STACKS_PUBLIC_FAUCET") == "1" {
		progressing, err = h.qualifyFaucet(ctx, progressing)
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("STACKS_PUBLIC_SCHEDULES") == "1" {
		progressing, err = h.qualifySchedules(ctx, progressing)
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("STACKS_PUBLIC_ACTIONS") == "1" {
		progressing, err = h.qualifyActions(ctx, progressing)
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("STACKS_PUBLIC_ACTORS") == "1" && os.Getenv("STACKS_PUBLIC_ACTORS_FIRST") != "1" {
		progressing, err = h.qualifyActors(ctx, progressing)
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("STACKS_PUBLIC_SHARED_DEFINITIONS") == "1" {
		progressing, err = h.qualifySharedDefinitions(ctx, progressing)
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("STACKS_PUBLIC_CHAOS") == "1" {
		progressing, err = h.qualifyChaos(ctx, progressing)
		if err != nil {
			t.Fatal(err)
		}
	}
	if progressing, err = h.pauseResume(ctx, progressing); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("STACKS_PUBLIC_REPEAT") == "1" {
		if err := h.qualifyRepeat(ctx, progressing); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("STACKS_PUBLIC_FINAL_EVICTION") == "1" {
		if err := h.evictBoundWorker(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.cleanup(); err != nil {
		t.Fatal(err)
	}
	t.Logf("declaration→Initialized+Operational→native progress→optional operator rollout→optional faucet/actions/actors→pause/resume→Stopped→root deletion→reusable declarations retained→namespace deleted; evidence=%s", h.evidence)
}
