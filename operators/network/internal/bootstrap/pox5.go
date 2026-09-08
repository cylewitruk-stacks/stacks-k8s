package bootstrap

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/environment"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
)

const pox5 = "ST000000000000000000002AMW42H.pox-5"

// pauseBitcoin waits for controller acknowledgement and all previously dispatched production receipts.
func (s *session) pauseBitcoin(ctx context.Context, setup environment.Bootstrap) error {
	if err := s.patch(ctx, setup.NetworkName, map[string]any{"bitcoinBlockProduction": map[string]any{"paused": true}}); err != nil {
		return err
	}
	var target bitcoin.BitcoinProductionTarget
	err := wait(ctx, "paused Bitcoin production and completed receipt accounting", 180, func() bool {
		if s.get(ctx, "bitcoinproductiontarget", naming.Child(setup.NetworkName, "bitcoin"), &target) != nil {
			return false
		}
		return s.networkUID != "" && target.Spec.NetworkUID == s.networkUID && target.DeletionTimestamp.IsZero() && target.Spec.NetworkName == setup.NetworkName && target.Spec.Policy.Paused && (target.Status.DispatchState == "" || target.Status.DispatchState == "Idle") && target.Status.Action == nil && target.Status.Reorganization == nil
	})
	if err != nil {
		return err
	}
	var height int64
	if err = s.bitcoin(ctx, setup, "getblockcount", nil, "", &height); err != nil {
		return err
	}
	var info nodeInfo
	if err = wait(ctx, "Stacks view of paused Bitcoin tip", 180, func() bool { return s.stacks(ctx, "/v2/info", &info) == nil && info.BurnHeight == height }); err != nil {
		return err
	}
	return s.record("BitcoinProductionPaused", map[string]any{"burnHeight": height, "targetUID": string(target.UID), "blocksProduced": target.Status.BlocksProduced, "dispatchState": target.Status.DispatchState})

}

// initializeBridge deploys real contracts and explicitly initializes the test registry before Epoch 4.0.
func (s *session) initializeBridge(ctx context.Context, setup environment.Bootstrap, contracts []contractSource) error {
	bridge := setup.Bridge
	var info nodeInfo
	if err := s.stacks(ctx, "/v2/info", &info); err != nil {
		return err
	}
	if info.BurnHeight >= profiles.PoX5ActivationHeight {
		return fmt.Errorf("sBTC prerequisites must be confirmed before Epoch 4.0")
	}
	for _, contract := range contracts {
		if err := s.publish(ctx, bridge.Deployer, contract); err != nil {
			return err
		}
	}
	keys := []clarityArgument{}
	for _, signer := range bridge.Signers {
		keys = append(keys, cv("buffer", signer.PublicKey))
	}
	tx, err := s.contractCall(ctx, bridge.Deployer, bridge.Deployer.Address+".sbtc-bootstrap-signers", "rotate-keys-wrapper", []clarityArgument{{Type: "list", Items: keys}, cv("buffer", bridge.Aggregate.PublicKey), cv("uint", strconv.Itoa(bridge.Threshold))}, false)
	if err != nil {
		return err
	}
	if err = s.execute(ctx, tx, "initialize bridge registry"); err != nil {
		return err
	}
	return s.verifyBridge(ctx, setup)
}

// verifyBridge observes protocol prerequisites independently of the explicit initialization mechanism.
func (s *session) verifyBridge(ctx context.Context, setup environment.Bootstrap) error {
	bridge := setup.Bridge
	registry := bridge.Deployer.Address + ".sbtc-registry"
	buffer := func(key string) string { return "0200000021" + key }
	list := "0x0b" + fmt.Sprintf("%08x", len(bridge.Signers))
	for _, signer := range bridge.Signers {
		list += buffer(signer.PublicKey)
	}
	for _, expected := range []struct{ method, value string }{
		{"get-current-aggregate-pubkey", "0x" + buffer(bridge.Aggregate.PublicKey)},
		{"get-current-signer-set", list},
	} {
		observed, err := s.readOnly(ctx, bridge.Deployer.Address, registry, expected.method, nil)
		if err != nil {
			return err
		}
		if observed != expected.value {
			return fmt.Errorf("bridge prerequisite %s does not match initialization evidence", expected.method)
		}
	}
	var threshold struct {
		Data string `json:"data"`
	}
	if err := s.stacks(ctx, "/v2/data_var/"+bridge.Deployer.Address+"/sbtc-registry/current-signature-threshold?proof=0", &threshold); err != nil {
		return err
	}
	if threshold.Data != fmt.Sprintf("0x01%032x", bridge.Threshold) {
		return fmt.Errorf("bridge signature threshold does not match initialization")
	}
	return s.record("BridgePrerequisitesObserved", map[string]any{"registry": registry, "aggregatePublicKey": bridge.Aggregate.PublicKey, "signerCount": len(bridge.Signers), "threshold": bridge.Threshold, "mode": "explicit-test-initialization"})
}

// enterPoX5 advances only after prerequisites, then registers each manager and stakes while Bitcoin is paused.
func (s *session) enterPoX5(ctx context.Context, setup environment.Bootstrap) error {
	if err := s.patch(ctx, setup.NetworkName, map[string]any{"bitcoinBlockProduction": map[string]any{"paused": false}}); err != nil {
		return err
	}
	var pox poxInfo
	if err := wait(ctx, "PoX-5 activation", 180, func() bool { return s.stacks(ctx, "/v2/pox", &pox) == nil && pox.Contract == pox5 }); err != nil {
		return err
	}
	if err := s.pauseBitcoin(ctx, setup); err != nil {
		return err
	}
	if err := s.stacks(ctx, "/v2/pox", &pox); err != nil {
		return err
	}
	if pox.Contract != pox5 || pox.NextCycle.BlocksUntilPrepare <= 0 {
		return fmt.Errorf("PoX-5 enrollment requires a reward-phase activation window")
	}
	if err := s.record("PoX5Activated", map[string]any{"burnHeight": pox.BurnHeight, "rewardCycle": pox.RewardCycle}); err != nil {
		return err
	}
	for _, participant := range setup.Participants {
		if err := s.registerManager(ctx, participant); err != nil {
			return err
		}
		account, err := s.account(ctx, participant.Stacker.Address)
		if err != nil {
			return err
		}
		if account.Locked != 0 {
			return fmt.Errorf("PoX-4 lock has not unlocked at PoX-5 transition")
		}
		tx, err := s.signPoX5(ctx, participant, "enroll", pox, account, 100000000000, 12)
		if err != nil {
			return err
		}
		if err = s.execute(ctx, tx, "PoX-5 direct stake "+participant.Signer); err != nil {
			return err
		}
		account, err = s.account(ctx, participant.Stacker.Address)
		if err != nil {
			return err
		}
		if account.Locked != 100000000000 || account.UnlockHeight <= pox.BurnHeight {
			return fmt.Errorf("PoX-5 direct stake lock was not observed")
		}
		if err = s.record("PoX5StakeObserved", map[string]any{"signer": participant.Signer, "holder": participant.Stacker.Address, "manager": participant.Manager, "locked": account.Locked, "unlockHeight": account.UnlockHeight}); err != nil {
			return err
		}
	}
	if err := s.patch(ctx, setup.NetworkName, map[string]any{"bitcoinBlockProduction": map[string]any{"paused": false}}); err != nil {
		return err
	}
	firstCycle := pox.RewardCycle + 1
	return wait(ctx, "first PoX-5 reward cycle", 240, func() bool {
		var current poxInfo
		return s.stacks(ctx, "/v2/pox", &current) == nil && current.Contract == pox5 && current.RewardCycle >= firstCycle
	})
}

// registerManager administers one manager using its own sender nonce and the consensus key's authorization.
func (s *session) registerManager(ctx context.Context, participant environment.StackingParticipant) error {
	source, err := directManager(participant.Stacker.Address)
	if err != nil {
		return err
	}
	parts := strings.Split(participant.Manager, ".")
	if len(parts) != 2 || parts[0] != participant.Administrator.Address {
		return fmt.Errorf("manager administrator binding mismatch")
	}
	if err = s.publish(ctx, participant.Administrator, contractSource{Name: parts[1], Source: source, ClarityVersion: 6}); err != nil {
		return err
	}
	tx, err := s.contractCall(ctx, participant.Administrator, participant.Manager, "register-self", []clarityArgument{cv("principal", participant.Manager), cv("buffer", participant.Consensus.PublicKey), cv("uint", "1"), {Type: "signer-grant", Manager: participant.Manager, AuthID: "1", PrivateKey: participant.Consensus.PrivateKey}}, false)
	if err != nil {
		return err
	}
	if err = s.execute(ctx, tx, "register "+participant.Signer); err != nil {
		return err
	}
	return nil
}

// signPoX5 encodes direct staking or extension with no delegated pool administration.
func (s *session) signPoX5(ctx context.Context, p environment.StackingParticipant, operation string, pox poxInfo, account accountState, amount, cycles int64) (signedTransaction, error) {
	if pox.Contract != pox5 || pox.BurnHeight < 0 || pox.CycleLength < 1 || pox.NextCycle.BlocksUntilPrepare <= 0 || cycles < 1 || cycles > 96 {
		return signedTransaction{}, fmt.Errorf("invalid PoX-5 staking observation")
	}
	method := "stake"
	args := []clarityArgument{cv("principal", p.Manager), cv("uint", strconv.FormatInt(amount, 10)), cv("uint", strconv.FormatInt(cycles, 10)), cv("uint", strconv.FormatInt(pox.BurnHeight, 10)), cv("none", "")}
	if operation == "extend" {
		method = "stake-update"
		args = []clarityArgument{cv("principal", p.Manager), cv("principal", p.Manager), cv("uint", strconv.FormatInt(cycles, 10)), cv("uint", "0"), cv("none", "")}
	} else if operation != "enroll" || amount <= 0 {
		return signedTransaction{}, fmt.Errorf("invalid PoX-5 operation")
	}
	return s.offline(ctx, "contract-sign.mjs", map[string]any{"operation": "call", "account": p.Stacker, "fee": poxFee, "nonce": strconv.FormatInt(account.Nonce, 10), "contractAddress": "ST000000000000000000002AMW42H", "contractName": "pox-5", "functionName": method, "arguments": args, "allowSTXLock": true})
}
