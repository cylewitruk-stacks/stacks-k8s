//go:build live

package publicintegration

import (
	"context"
	"fmt"
	"reflect"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// qualifySharedDefinitions runs independent Bitcoin actors from one definition and derived wallet/account.
func (h *harness) qualifySharedDefinitions(ctx context.Context, before snapshot) (snapshot, error) {
	names := []string{"shared-bitcoin-a", "shared-bitcoin-b"}
	definition := &bitcoin.BitcoinNode{}
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: "btc-07"}, definition); err != nil {
		return before, err
	}
	wallet := &bitcoin.BitcoinWallet{}
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: "miner-wallet-01"}, wallet); err != nil {
		return before, err
	}
	if wallet.Spec.KeySource == nil || wallet.Spec.KeySource.StacksMinerAccountRef == nil {
		return before, fmt.Errorf("shared qualification requires the fixture's derived miner wallet")
	}
	account := &stacks.StacksAccount{}
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: wallet.Spec.KeySource.StacksMinerAccountRef.Name}, account); err != nil {
		return before, err
	}
	if wallet.UID == "" || account.UID == "" || wallet.Status.BitcoinAddress == "" {
		return before, fmt.Errorf("shared public wallet/account identity unavailable")
	}
	guard, err := h.captureActorGuard(ctx, before)
	if err != nil {
		return before, err
	}
	var initial participantEvidence
	var height int64
	for _, p := range before.Participants {
		if p.Name == "btc-07" {
			initial = p
		}
	}
	for _, r := range before.Executions {
		if r.Status.Observation != nil && r.Status.Observation.Height > height {
			height = r.Status.Observation.Height
		}
	}
	if initial.Status.Runtime == nil {
		return before, fmt.Errorf("original shared-definition actor unavailable")
	}
	originalStorage, err := h.actorClaim(ctx, initial.Status.Runtime)
	if err != nil {
		return before, err
	}
	epochs := map[string]actorEpoch{"btc-07": {Participant: initial.Identity, Runtime: *initial.Status.Runtime.DeepCopy(), Storage: originalStorage}}
	err = h.changeActors(ctx, func(root *api.StacksNetwork) error {
		for _, name := range names {
			for _, id := range root.Status.Identities {
				if id.Name == name {
					return fmt.Errorf("shared name already allocated")
				}
			}
			for _, entry := range root.Spec.Participants {
				if entry.Name == name {
					return fmt.Errorf("shared name already declared")
				}
			}
			root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: name, Kind: "BitcoinNode", Definition: api.Definition{Ref: &common.NameRef{Name: definition.Name}}, Overrides: &api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{ActorFields: common.ActorFields{Storage: &common.Storage{Size: ptr.To("1Gi"), RetainOnDelete: ptr.To(false)}}}}})
		}
		return nil
	})
	if err != nil {
		return before, err
	}
	ready, err := h.wait(ctx, "shared-definitions-ready", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		for _, name := range names {
			found := false
			for _, p := range s.Participants {
				if p.Name != name || p.Kind != "BitcoinNode" {
					continue
				}
				if p.Status.Admission == nil || p.Status.Admission.Source.UID != definition.UID || p.Status.Admission.Source.Name != definition.Name || p.Status.Runtime == nil {
					return false, nil
				}
				for _, r := range s.Executions {
					if r.ParticipantUID != p.Identity.UID {
						continue
					}
					view, e := (actionSelection{Participant: p, Execution: r.Identity}).observe(s)
					if e != nil || view.Height <= height {
						continue
					}
					for _, w := range view.Wallets {
						if w.Wallet.UID == wallet.UID && w.Wallet.Name == wallet.Name && w.Address == wallet.Status.BitcoinAddress && w.Ready {
							storage, e := h.actorClaim(ctx, p.Status.Runtime)
							if e != nil {
								return false, e
							}
							current := actorEpoch{Participant: p.Identity, Runtime: *p.Status.Runtime.DeepCopy(), Storage: storage, Height: uint64(view.Height)}
							for other, epoch := range epochs {
								if other != name {
									if e := newStorageEpoch(epoch, current); e != nil {
										return false, e
									}
								}
							}
							epochs[name] = current
							found = true
						}
					}
				}
			}
			if !found {
				return false, nil
			}
		}
		return true, h.checkActorGuard(ctx, guard, s)
	})
	if err != nil {
		return ready, err
	}
	ready, err = h.awaitProgress(ctx, "shared-definitions-progress", ready)
	if err != nil {
		return ready, err
	}
	if err = h.event("shared-definition-runtime", map[string]any{"definition": objectIdentity(definition), "wallet": objectIdentity(wallet), "account": objectIdentity(account), "epochs": epochs}); err != nil {
		return ready, err
	}
	err = h.changeActors(ctx, func(root *api.StacksNetwork) error {
		for _, name := range names {
			expected := epochs[name].Participant.UID
			matched := false
			for _, id := range root.Status.Identities {
				matched = matched || id.Name == name && id.UID == expected && !id.Removing
			}
			if !matched {
				return fmt.Errorf("shared participant identity changed")
			}
		}
		kept := root.Spec.Participants[:0]
		for _, entry := range root.Spec.Participants {
			if entry.Name != names[0] && entry.Name != names[1] {
				kept = append(kept, entry)
			}
		}
		root.Spec.Participants = kept
		return nil
	})
	if err != nil {
		return ready, err
	}
	for _, name := range names {
		ready, err = h.awaitActorRemoved(ctx, name, epochs[name], guard, false)
		if err != nil {
			return ready, err
		}
	}
	for _, original := range []client.Object{definition, wallet, account} {
		current := original.DeepCopyObject().(client.Object)
		if err = h.c.Get(ctx, client.ObjectKeyFromObject(original), current); err != nil {
			return ready, err
		}
		if current.GetUID() != original.GetUID() || current.GetGeneration() != original.GetGeneration() || current.GetDeletionTimestamp() != nil {
			return ready, fmt.Errorf("reusable definition/key changed during shared runtime disposal")
		}
	}
	// Definition/controller status may gain fresh observations; immutable key identity must not change.
	currentWallet := &bitcoin.BitcoinWallet{}
	if err = h.c.Get(ctx, client.ObjectKeyFromObject(wallet), currentWallet); err != nil {
		return ready, err
	}
	if currentWallet.Status.BitcoinAddress != wallet.Status.BitcoinAddress || !reflect.DeepEqual(currentWallet.Spec, wallet.Spec) {
		return ready, fmt.Errorf("shared wallet identity changed")
	}
	if err = h.event("shared-definitions-retained", map[string]any{"definitionUID": definition.UID, "walletUID": wallet.UID, "accountUID": account.UID}); err != nil {
		return ready, err
	}
	ready, err = h.wait(ctx, "shared-disposal-ready", 5*time.Minute, true, func(s snapshot) (bool, error) { _, ok := progress(s); return ok, nil })
	if err != nil {
		return ready, err
	}
	return h.awaitProgress(ctx, "shared-disposal-progress", ready)
}
