//go:build live && bitcoinproduction

package integration

import (
	"context"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestLiveIndependentActionOperator verifies receipt retention across a lifecycle-only outage.
func TestLiveIndependentActionOperator(t *testing.T) {
	c, key := bitcoinLiveClient(t)
	ctx := context.Background()
	updateNetwork(t, ctx, c, key, func(n *networkv1.StacksNetwork) { n.Spec.BitcoinBlockProduction.Paused = true })
	waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool {
		return p.Status.Phase == "Paused" && p.Status.Action == nil && p.Status.Reorganization == nil
	})
	deployments := &appsv1.DeploymentList{}
	liveMust(t, c.List(ctx, deployments, client.InNamespace(key.Namespace), client.MatchingLabels{"app.kubernetes.io/name": "stacks-action-operator"}))
	if len(deployments.Items) != 1 {
		t.Fatal("expected exactly one action lifecycle Deployment")
	}
	deployment := deployments.Items[0].DeepCopy()
	scale := func(replicas int32) {
		eventuallyLive(t, "scale lifecycle deployment", func() bool {
			if c.Get(ctx, client.ObjectKeyFromObject(deployment), deployment) != nil {
				return false
			}
			deployment.Spec.Replicas = &replicas
			return c.Update(ctx, deployment) == nil
		})
	}
	a := createGeneration(t, c, key, "lifecycle-restart", 2, 20, 2*time.Minute)
	waitGeneration(t, c, a, func(a *actionv1.BitcoinBlockGeneration) bool { return a.Status.BlocksGenerated == 1 })
	scale(0)
	t.Cleanup(func() { scale(1) })
	eventuallyLive(t, "lifecycle replicas stopped", func() bool {
		return c.Get(ctx, client.ObjectKeyFromObject(deployment), deployment) == nil && deployment.Status.ObservedGeneration == deployment.Generation && deployment.Status.Replicas == 0
	})
	p := waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool {
		return p.Status.Action != nil && p.Status.Action.BlocksGenerated == 2 && p.Status.DispatchState == "Idle"
	})
	liveMust(t, c.Get(ctx, client.ObjectKeyFromObject(a), a))
	if a.Status.Phase == "Completed" || len(a.Finalizers) == 0 || p.Status.Action.UID != string(a.UID) {
		t.Fatal("executor released without terminal lifecycle acknowledgement")
	}
	scale(1)
	waitGeneration(t, c, a, func(a *actionv1.BitcoinBlockGeneration) bool {
		return a.Status.Phase == "Completed" && a.Status.BlocksGenerated == 2 && len(a.Finalizers) == 0
	})
	waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool {
		return p.Status.Action == nil && p.Status.Phase == "Paused"
	})
	updateNetwork(t, ctx, c, key, func(n *networkv1.StacksNetwork) { n.Spec.BitcoinBlockProduction.Paused = false })
	t.Logf("independent lifecycle restart retained and acknowledged two receipts: action=%s dispatch=%s", a.UID, a.Status.LastDispatchID)
}
