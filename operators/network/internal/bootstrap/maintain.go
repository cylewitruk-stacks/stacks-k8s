package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Maintain extends the declared PoX-4 stacker's lock for a bounded interval.
// It exits on any unknown submission outcome and never reenrolls an expired lock.
func Maintain(ctx context.Context, options Options, duration time.Duration) error {
	setup, _, err := Load(options.Manifest)
	if err != nil {
		return err
	}
	if duration < time.Second || duration > 24*time.Hour || options.StacksPort < 1 || options.StacksPort > 65535 {
		return fmt.Errorf("valid loopback port and duration 1s..24h are required")
	}
	s := &session{options: options, namespace: setup.Namespace, http: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if err = s.claimEvidence("renewal"); err != nil {
		return err
	}
	deadline := time.Now().Add(duration)
	var pending *signedTransaction
	var previousUnlock int64
	var pendingDeadline time.Time
	for time.Now().Before(deadline) {
		var pox poxInfo
		if err = s.stacks(ctx, "/v2/pox", &pox); err != nil {
			return err
		}
		if pox.Contract != pox4 || pox.CycleLength < 1 {
			return fmt.Errorf("maintenance requires PoX-4")
		}
		account, err := s.account(ctx, setup.Signer.Address)
		if err != nil {
			return err
		}
		if account.Locked == 0 {
			return fmt.Errorf("signer lock expired; inspect bootstrap before reenrollment")
		}
		if pending != nil {
			if account.UnlockHeight > previousUnlock {
				if err = s.record("SignerLockExtended", map[string]any{"txid": pending.TxID, "previousUnlockHeight": previousUnlock, "observedUnlockHeight": account.UnlockHeight}); err != nil {
					return err
				}
				pending = nil
			} else if time.Now().After(pendingDeadline) {
				return fmt.Errorf("renewal did not confirm; inspect recorded TxID before further work")
			}
		}
		remaining := (account.UnlockHeight - pox.BurnHeight + pox.CycleLength - 1) / pox.CycleLength
		if pending == nil && remaining <= 6 && pox.NextCycle.BlocksUntilPrepare > 0 {
			tx, err := s.sign(ctx, setup, "extend", pox, account, 0, 6, pox.BurnHeight)
			if err != nil {
				return err
			}
			if err = s.record("SignerRenewalAuthorized", map[string]any{"txid": tx.TxID, "nonce": account.Nonce, "previousUnlockHeight": account.UnlockHeight}); err != nil {
				return err
			}
			if err = s.submit(ctx, tx); err != nil {
				return err
			}
			pending = &tx
			previousUnlock = account.UnlockHeight
			pendingDeadline = time.Now().Add(180 * time.Second)
			if err = s.record("SignerRenewalSubmitted", map[string]any{"txid": tx.TxID}); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if pending != nil {
		return fmt.Errorf("maintenance ended with a pending renewal; inspect recorded TxID")
	}
	return nil
}
