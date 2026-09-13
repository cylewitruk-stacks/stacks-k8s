package bitcoincontrol

import (
	"fmt"
	"time"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/cadence"
)

// validateActionCadence checks every decoded mode before reserving finite work.
func validateActionCadence(c action.GenerationCadence, count int32) error {
	if count < 1 || count > 100 || c.Mode != action.CadenceFixed && c.IntervalSeconds != 0 || c.Mode != action.CadenceUniform && (c.MinSeconds != 0 || c.MaxSeconds != 0) || c.Mode != action.CadenceExplicit && len(c.DelaysSeconds) != 0 {
		return fmt.Errorf("invalid finite cadence")
	}
	switch c.Mode {
	case action.CadenceImmediate:
		return nil
	case action.CadenceFixed:
		if c.IntervalSeconds >= 1 && c.IntervalSeconds <= 60 {
			return nil
		}
	case action.CadenceUniform:
		if c.MinSeconds >= 1 && c.MaxSeconds >= c.MinSeconds && c.MaxSeconds <= 60 {
			return nil
		}
	case action.CadenceExplicit:
		if len(c.DelaysSeconds) == int(count-1) {
			for _, delay := range c.DelaysSeconds {
				if delay < 0 || delay > 60 {
					return fmt.Errorf("invalid explicit delay")
				}
			}
			return nil
		}
	}
	return fmt.Errorf("unsupported finite cadence")
}

// actionDelay reuses the stable pure sampler with the original receipt identity as decision key.
func actionDelay(c action.GenerationCadence, count, completed int32, key string) (time.Duration, error) {
	if err := validateActionCadence(c, count); err != nil {
		return 0, err
	}
	if completed < 1 || completed >= count {
		return 0, fmt.Errorf("delay requires a non-final receipt")
	}
	switch c.Mode {
	case action.CadenceImmediate:
		return 0, nil
	case action.CadenceFixed:
		return cadence.Seconds(c.IntervalSeconds, c.IntervalSeconds, key)
	case action.CadenceUniform:
		return cadence.Seconds(c.MinSeconds, c.MaxSeconds, key)
	case action.CadenceExplicit:
		d := c.DelaysSeconds[completed-1]
		return cadence.Seconds(d, d, key)
	}
	return 0, fmt.Errorf("unsupported finite cadence")
}
