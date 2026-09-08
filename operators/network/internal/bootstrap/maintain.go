package bootstrap

import (
	"context"
	"fmt"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/environment"
	"net/http"
	"time"
)

// Maintain extends the declared direct stackers' locks for a bounded interval.
// It exits on any unknown submission outcome and never reenrolls an expired lock.
func Maintain(ctx context.Context, options Options, duration time.Duration) error {
	setup, declared, err := Load(options.Manifest)
	if err != nil {
		return err
	}
	if declared.Spec.Operation != nil {
		return fmt.Errorf("managed networks are maintained by their capability controllers")
	}
	if duration < time.Second || duration > 24*time.Hour || options.StacksPort < 1 || options.StacksPort > 65535 {
		return fmt.Errorf("valid loopback port and duration 1s..24h are required")
	}
	s := &session{options: options, namespace: setup.Namespace, http: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if err = s.claimEvidence("renewal"); err != nil {
		return err
	}
	if err = environment.ValidateBootstrapKeys(setup, options.SDKDirectory); err != nil {
		return err
	}
	deadline := time.Now().Add(duration)
	type renewal struct {
		pending        *signedTransaction
		previousUnlock int64
		deadline       time.Time
	}
	renewals := make([]renewal, len(setup.Participants))
	for time.Now().Before(deadline) {
		var pox poxInfo
		if err = s.stacks(ctx, "/v2/pox", &pox); err != nil {
			return err
		}
		if (pox.Contract != pox4 && (pox.Contract != pox5 || setup.Bridge == nil)) || pox.CycleLength < 1 {
			return fmt.Errorf("maintenance requires the declared PoX-4/PoX-5 protocol")
		}
		for i, participant := range setup.Participants {
			state := &renewals[i]
			account, err := s.account(ctx, participant.Stacker.Address)
			if err != nil {
				return err
			}
			if account.Locked == 0 {
				return fmt.Errorf("signer lock expired; inspect bootstrap before reenrollment")
			}
			if state.pending != nil {
				receipt, readErr := s.execution(ctx, state.pending.TxID)
				if readErr == nil && receipt.Found && !receipt.Success {
					return fmt.Errorf("renewal executed unsuccessfully; inspect recorded TxID")
				}
				if readErr == nil && receipt.Found && receipt.Success && account.UnlockHeight > state.previousUnlock {
					if err = s.record("SignerLockExtended", map[string]any{"txid": state.pending.TxID, "previousUnlockHeight": state.previousUnlock, "observedUnlockHeight": account.UnlockHeight, "blockID": receipt.BlockID}); err != nil {
						return err
					}
					state.pending = nil
				} else if time.Now().After(state.deadline) {
					return fmt.Errorf("renewal did not confirm; inspect recorded TxID before further work")
				}
			}
			remaining := (account.UnlockHeight - pox.BurnHeight + pox.CycleLength - 1) / pox.CycleLength
			if state.pending == nil && remaining <= 6 && pox.NextCycle.BlocksUntilPrepare > 0 {
				tx, err := s.sign(ctx, participant, "extend", pox, account, 0, 6, pox.BurnHeight)
				if err != nil {
					return err
				}
				if err = s.record("SignerRenewalAuthorized", map[string]any{"txid": tx.TxID, "nonce": account.Nonce, "previousUnlockHeight": account.UnlockHeight}); err != nil {
					return err
				}
				if err = s.submit(ctx, tx); err != nil {
					return err
				}
				state.pending = &tx
				state.previousUnlock = account.UnlockHeight
				state.deadline = time.Now().Add(180 * time.Second)
				if err = s.record("SignerRenewalSubmitted", map[string]any{"txid": tx.TxID}); err != nil {
					return err
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	for _, state := range renewals {
		if state.pending != nil {
			return fmt.Errorf("maintenance ended with a pending renewal; inspect recorded TxID")
		}
	}
	return nil
}
