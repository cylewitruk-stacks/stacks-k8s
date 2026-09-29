//go:build integration

package foundationintegration

import (
	"context"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyBatchGenerationSchema admits the first-gate batching choice without pruning it.
func verifyBatchGenerationSchema(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "batch-generation"}}
	if err := c.Create(ctx, namespace); err != nil {
		t.Fatal(err)
	}
	for name, batch := range map[string]*bool{"omitted": nil, "disabled": ptr.To(false), "enabled": ptr.To(true)} {
		production := &bitcoin.BitcoinBlockProduction{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace.Name},
			Spec: bitcoin.BitcoinBlockProductionSpec{Initialization: &bitcoin.Initialization{
				TargetNodeRef:         common.NameRef{Name: "btc-01"},
				MinimumHeight:         103,
				MatureOutputsPerMiner: 2,
				BatchGeneration:       batch,
			}},
		}
		if err := c.Create(ctx, production, client.FieldValidation("Strict")); err != nil {
			t.Fatalf("batch generation %s rejected: %v", name, err)
		}
		stored := &bitcoin.BitcoinBlockProduction{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(production), stored); err != nil {
			t.Fatal(err)
		}
		if got := stored.Spec.Initialization.BatchGeneration; (got == nil) != (batch == nil) ||
			got != nil && *got != *batch {
			t.Fatalf("batch generation %s not preserved: %v", name, got)
		}
	}
}
