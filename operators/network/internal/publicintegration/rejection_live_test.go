//go:build live

package publicintegration

import (
	"context"
	"fmt"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// qualifyTrafficRejection uses a real native fee refusal, then restores the declared fee in the same process.
func (h *harness) qualifyTrafficRejection(ctx context.Context, before snapshot) (snapshot, error) {
	var definition stacks.StacksTransactionProduction
	key := client.ObjectKey{Namespace: h.config.namespace, Name: "traffic"}
	if err := h.c.Get(ctx, key, &definition); err != nil {
		return before, err
	}
	original := definition.DeepCopy()
	low := common.Amount("1")
	definition.Spec.FeeMicroSTX = &low
	if err := h.c.Patch(
		ctx,
		&definition,
		client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}),
	); err != nil {
		return before, err
	}
	var refused *api.WorkerExecutionStatus
	refusedSnapshot, err := h.wait(
		ctx,
		"native-fee-rejected",
		h.config.progressTimeout,
		true,
		func(s snapshot) (bool, error) {
			for _, p := range s.Participants {
				e := p.Status.Execution
				if p.Kind == "StacksTransactionProduction" && e != nil && e.Reason == "RejectedFeeTooLow" &&
					e.Transactions != nil &&
					e.Transactions.Rejected > 0 &&
					e.Pending == 0 &&
					len(e.Transactions.LastTxID) == 64 {
					refused = e.DeepCopy()
					return true, nil
				}
			}
			return false, nil
		},
	)
	if err != nil {
		return before, err
	}
	if err := h.setOperation(ctx, "Paused"); err != nil {
		return refusedSnapshot, err
	}
	paused, err := h.wait(
		ctx,
		"rejected-stream-paused",
		h.config.progressTimeout,
		true,
		func(s snapshot) (bool, error) {
			for _, p := range s.Participants {
				e := p.Status.Execution
				if p.Kind == "StacksTransactionProduction" && e != nil && e.PodUID == refused.PodUID &&
					e.ProcessNonce == refused.ProcessNonce &&
					e.Phase == "Paused" &&
					e.Pending == 0 {
					return condition(s, "Running", metav1.ConditionFalse), nil
				}
			}
			return false, nil
		},
	)
	if err != nil {
		return refusedSnapshot, err
	}
	if err := h.c.Get(ctx, key, &definition); err != nil {
		return paused, err
	}
	if definition.UID != original.UID {
		return paused, fmt.Errorf("traffic definition replaced")
	}
	base := definition.DeepCopy()
	definition.Spec.FeeMicroSTX = original.Spec.FeeMicroSTX
	if err := h.c.Patch(
		ctx,
		&definition,
		client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}),
	); err != nil {
		return paused, err
	}
	if err := h.setOperation(ctx, "Running"); err != nil {
		return paused, err
	}
	return h.wait(ctx, "rejected-stream-recovered", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		for _, p := range s.Participants {
			e := p.Status.Execution
			if p.Kind != "StacksTransactionProduction" || e == nil {
				continue
			}
			if e.PodUID != refused.PodUID || e.ProcessNonce != refused.ProcessNonce {
				return false, fmt.Errorf("rejection recovered by replacing its process")
			}
			return e.Transactions != nil && e.Transactions.Rejected >= refused.Transactions.Rejected &&
				e.Transactions.Included > refused.Transactions.Included &&
				condition(s, "Operational", metav1.ConditionTrue), nil
		}
		return false, nil
	})
}
