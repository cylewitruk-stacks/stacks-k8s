package bitcoincontrol

import (
	"context"
	"fmt"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	"k8s.io/apimachinery/pkg/api/equality"
)

// observePaused publishes native reads while discarding every proposed wallet mutation.
func (w *Worker) observePaused(ctx context.Context, record *bitcoin.BitcoinExecution) error {
	admitted, err := w.authorizeObservation(ctx, record)
	if err != nil {
		return err
	}
	if admitted.root.Spec.Operation != "Paused" {
		return nil
	}
	observation, _, err := w.observe(ctx, admitted, record.DeepCopy())
	if err != nil {
		return err
	}
	current, err := w.authorizeObservation(ctx, record)
	if err != nil {
		return err
	}
	if current.root.Spec.Operation != "Paused" {
		return nil
	}
	if !equality.Semantic.DeepEqual(admitted.target, current.target) {
		return fmt.Errorf("actor identity changed during paused observation")
	}
	if equality.Semantic.DeepEqual(record.Status.Observation, observation) {
		return nil
	}
	record.Status.Observation = observation
	return w.Client.Status().Update(ctx, record)
}
