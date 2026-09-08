package managedoperation

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/accountledger"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksrpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssdk"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackstx"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// bootAddress is the testnet boot-code principal.
	bootAddress = "ST000000000000000000002AMW42H"
	// pox4 is the supported pre-Epoch-4.0 direct stacking contract.
	pox4 = bootAddress + ".pox-4"
	// pox5 is the supported manager-authorized direct staking contract.
	pox5 = bootAddress + ".pox-5"
	// prepareMargin reduces boundary races; delayed execution may still cross the boundary.
	prepareMargin = 2
)

// StackingReconciler maintains direct holders and their separate manager administrators.
type StackingReconciler struct{ Runtime *Runtime }

// SetupWithManager installs the direct stacking capability controller.
func (r *StackingReconciler) SetupWithManager(m ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(m).For(&stacks.StacksStackingParticipant{}).Complete(r.Runtime.Binding.Wrap(r.Runtime.Reader, &stacks.StacksStackingParticipant{}, r))
}

// Reconcile re-observes the active protocol and lock instead of replaying a bootstrap sequence.
func (r *StackingReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	participant := &stacks.StacksStackingParticipant{}
	if err := r.Runtime.Reader.Get(ctx, request.NamespacedName, participant); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !participant.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	err := r.reconcile(ctx, participant)
	return r.Runtime.result(ctx, participant, err)
}

// reconcile treats holder funding, administrator spending and consensus signing as distinct roles.
func (r *StackingReconciler) reconcile(ctx context.Context, participant *stacks.StacksStackingParticipant) error {
	rt, policy := r.Runtime, participant.Spec.Policy
	parent, err := rt.parent(ctx, participant, participant.Spec.NetworkName, participant.Spec.NetworkUID, "StacksStackingParticipant")
	if err != nil {
		return err
	}
	holder, err := rt.account(ctx, parent, policy.HolderAccount)
	if err != nil {
		return err
	}
	admin, err := rt.account(ctx, parent, policy.AdministratorAccount)
	if err != nil {
		return err
	}
	if holder.Spec.Policy.Address == admin.Spec.Policy.Address {
		return fmt.Errorf("holder and administrator require separate accounts")
	}
	for _, account := range []*stacks.StacksAccount{holder, admin} {
		if err := rt.drain(ctx, participant, account); err != nil {
			return err
		}
	}
	if parent.Spec.Suspended || parent.Spec.Operation == nil || parent.Spec.Operation.Paused || policy.Paused {
		return errPaused
	}
	target, err := rt.Ledger.Admission.Admit(ctx, holder, string(participant.UID), false)
	if err != nil {
		return err
	}
	pox, err := rt.RPC.PoX(ctx, target.Endpoint)
	if err != nil {
		return err
	}
	if pox.Contract != pox4 && pox.Contract != pox5 {
		return fmt.Errorf("waiting for a supported direct stacking protocol")
	}
	genesis, err := profiles.ResolveGenesis(parent.Spec.Genesis)
	if err != nil {
		return err
	}
	if legacyTransitionPending(pox, genesis.Epochs[len(genesis.Epochs)-1].StartHeight) {
		return fmt.Errorf("waiting for PoX-5 activation before another holder operation")
	}
	account, err := rt.RPC.Account(ctx, target.Endpoint, holder.Spec.Policy.Address)
	if err != nil {
		return err
	}
	status := participant.Status.DeepCopy()
	status.Protocol, status.BurnHeight, status.UnlockHeight = pox.Contract, pox.BurnHeight, account.UnlockHeight
	if err := rt.status(ctx, participant, *status); err != nil {
		return err
	}
	if holder.Status.NextNonce != nil && *holder.Status.NextNonce != account.Nonce {
		return fmt.Errorf("holder nonce does not match recorded canonical state")
	}
	if holder.Status.Transaction == nil && account.Locked > 0 {
		return fmt.Errorf("holder has an existing lock without managed enrollment evidence")
	}
	consensus, err := rt.key(ctx, participant.Namespace, policy.ConsensusKeySecretRef, "")
	if err != nil {
		return err
	}
	matched := false
	for _, signer := range parent.Spec.Signers {
		if signer.Name == policy.Signer && signer.PublicKey == consensus.PublicKey && !signer.Suspended {
			matched = true
		}
	}
	if !matched || consensus.Address == holder.Spec.Policy.Address || consensus.Address == admin.Spec.Policy.Address {
		return fmt.Errorf("consensus authorization does not match the declared signer identity")
	}
	manager := admin.Spec.Policy.Address + ".direct-signer"
	if pox.Contract == pox5 {
		if err := r.manager(ctx, participant, admin, holder.Spec.Policy.Address, manager, consensus, target.Endpoint); err != nil {
			return err
		}
	}
	operation, cycles, ordinal, err := stackingOperation(pox, account, policy.LockCycles)
	if err != nil {
		return err
	}
	if operation == "" {
		return nil
	}
	key, err := rt.key(ctx, participant.Namespace, holder.Spec.Policy.KeySecretRef, holder.Spec.Policy.Address)
	if err != nil {
		return err
	}
	amount := policy.AmountMicroSTX
	if pox.Contract == pox4 && amount < pox.MinThreshold+pox.MinThreshold/2 {
		amount = pox.MinThreshold + pox.MinThreshold/2
	}
	if operation == "enroll" && (amount <= 0 || account.Balance < amount) {
		return fmt.Errorf("holder balance is insufficient for its direct stake")
	}
	return r.stake(ctx, participant, holder, key, consensus, manager, pox, operation, amount, cycles, ordinal)
}

// stackingOperation selects a bounded extension or enrollment from current protocol observations.
func stackingOperation(pox stacksrpc.PoX, account stacksrpc.Account, horizon int32) (string, int64, int64, error) {
	if horizon < 2 || horizon > 12 || pox.CycleLength < 1 || pox.RewardCycle < 0 || pox.RewardCycle > 9999999999 {
		return "", 0, 0, fmt.Errorf("unsupported stacking policy or timing observation")
	}
	version := int64(4)
	if pox.Contract == pox5 {
		version = 5
	} else if pox.Contract != pox4 {
		return "", 0, 0, fmt.Errorf("unsupported stacking protocol")
	}
	ordinal := version*1000000000000 + pox.RewardCycle*100 + 1
	operation, cycles := "enroll", int64(horizon)
	if account.Locked > 0 {
		remaining := (account.UnlockHeight - pox.BurnHeight + pox.CycleLength - 1) / pox.CycleLength
		if remaining > int64(horizon)/2 {
			return "", 0, 0, nil
		}
		operation, cycles, ordinal = "extend", int64(horizon)/2, ordinal+1
	}
	if pox.BlocksUntilPrepare < prepareMargin {
		if account.Locked > 0 {
			return "", 0, 0, nil
		}
		return "", 0, 0, fmt.Errorf("enrollment is waiting for the next safe reward-phase window")
	}
	return operation, cycles, ordinal, nil
}

// manager establishes this holder's non-custodial manager and observes its registered signer key.
func (r *StackingReconciler) manager(ctx context.Context, participant *stacks.StacksStackingParticipant, admin *stacks.StacksAccount, holder, manager string, consensus stackssdk.Key, endpoint string) error {
	rt := r.Runtime
	source, err := protocolcontracts.DirectManager(holder)
	if err != nil {
		return err
	}
	observed, found, err := rt.RPC.Source(ctx, endpoint, admin.Spec.Policy.Address, "direct-signer")
	if err != nil {
		return err
	}
	if found && observed != source {
		return fmt.Errorf("direct signer manager source differs from the declared holder profile")
	}
	if !found {
		key, err := rt.key(ctx, participant.Namespace, admin.Spec.Policy.KeySecretRef, admin.Spec.Policy.Address)
		if err != nil {
			return err
		}
		input := map[string]any{"operation": "publish", "contractName": "direct-signer", "clarityVersion": 6, "source": source, "fee": strconv.Itoa(len(source)*10 + 3000)}
		if err := rt.signed(ctx, participant, admin, 1, input, key); err != nil {
			return err
		}
		return accountledger.ErrWaiting
	}
	encoded, err := rt.SDK.EncodeArguments(ctx, []stackssdk.Argument{{Type: "principal", Value: manager}})
	if err != nil {
		return err
	}
	registered, err := rt.RPC.ReadOnly(ctx, endpoint, holder, bootAddress, "pox-5", "get-signer-info", encoded)
	if err != nil {
		return err
	}
	if registered == "0x0a0200000021"+consensus.PublicKey {
		return nil
	}
	if registered != "0x09" {
		return fmt.Errorf("manager is registered with a different signer key")
	}
	key, err := rt.key(ctx, participant.Namespace, admin.Spec.Policy.KeySecretRef, admin.Spec.Policy.Address)
	if err != nil {
		return err
	}
	arguments := []stackssdk.Argument{{Type: "principal", Value: manager}, {Type: "buffer", Value: consensus.PublicKey}, {Type: "uint", Value: "1"}, {Type: "signer-grant", Manager: manager, AuthID: "1"}}
	public := map[string]any{"operation": "call", "contractAddress": key.Address, "contractName": "direct-signer", "functionName": "register-self", "fee": "3000", "arguments": arguments}
	data, err := json.Marshal(public)
	if err != nil {
		return err
	}
	err = rt.execute(ctx, participant, admin, accountledger.Request{Ordinal: 2, Digest: digest(data), Sign: func(ctx context.Context, nonce int64) (stackstx.Transaction, error) {
		// The private authorization key never enters the public intent digest or Kubernetes status.
		arguments[3].PrivateKey = consensus.PrivateKey
		public["arguments"], public["account"], public["nonce"] = arguments, key, strconv.FormatInt(nonce, 10)
		return rt.SDK.Sign(ctx, "contract-sign.mjs", public)
	}})
	if err != nil {
		return err
	}
	return accountledger.ErrWaiting
}

// stake signs explicit direct protocol inputs while the account ledger owns nonce and submission.
func (r *StackingReconciler) stake(ctx context.Context, participant *stacks.StacksStackingParticipant, holder *stacks.StacksAccount, key, consensus stackssdk.Key, manager string, pox stacksrpc.PoX, operation string, amount, cycles, ordinal int64) error {
	rt := r.Runtime
	public := map[string]any{"operation": operation, "protocol": pox.Contract, "manager": manager, "signer": consensus.PublicKey,
		"amount": amount, "cycles": cycles, "rewardCycle": pox.RewardCycle}
	data, err := json.Marshal(public)
	if err != nil {
		return err
	}
	err = rt.execute(ctx, participant, holder, accountledger.Request{Ordinal: ordinal, Digest: digest(data), Sign: func(ctx context.Context, nonce int64) (stackstx.Transaction, error) {
		if pox.Contract == pox4 {
			input := map[string]any{"fee": "3000", "account": key, "consensus": consensus, "operation": operation, "contract": pox.Contract,
				"nonce": strconv.FormatInt(nonce, 10), "burnHeight": strconv.FormatInt(pox.BurnHeight, 10), "rewardCycle": strconv.FormatInt(pox.RewardCycle, 10),
				"amount": strconv.FormatInt(amount, 10), "cycles": strconv.FormatInt(cycles, 10), "authID": strconv.FormatInt(ordinal, 10)}
			return rt.SDK.Sign(ctx, "pox-sign.mjs", input)
		}
		method := "stake"
		args := []stackssdk.Argument{{Type: "principal", Value: manager}, {Type: "uint", Value: strconv.FormatInt(amount, 10)}, {Type: "uint", Value: strconv.FormatInt(cycles, 10)}, {Type: "uint", Value: strconv.FormatInt(pox.BurnHeight, 10)}, {Type: "none"}}
		if operation == "extend" {
			method = "stake-update"
			args = []stackssdk.Argument{{Type: "principal", Value: manager}, {Type: "principal", Value: manager}, {Type: "uint", Value: strconv.FormatInt(cycles, 10)}, {Type: "uint", Value: "0"}, {Type: "none"}}
		}
		return rt.SDK.Sign(ctx, "contract-sign.mjs", map[string]any{"operation": "call", "account": key, "fee": "3000", "nonce": strconv.FormatInt(nonce, 10),
			"contractAddress": bootAddress, "contractName": "pox-5", "functionName": method, "arguments": args, "allowSTXLock": true})
	}})
	if err != nil {
		return err
	}
	return accountledger.ErrWaiting
}

// legacyTransitionPending avoids reenrolling a forcibly unlocked PoX-4 holder at the Epoch-4 boundary.
// Core's active-contract response switches strictly after the activation height.
func legacyTransitionPending(pox stacksrpc.PoX, activation int64) bool {
	return pox.Contract == pox4 && pox.BurnHeight >= activation-prepareMargin
}
