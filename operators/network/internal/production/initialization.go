package production

import (
	"context"
	"fmt"
	"reflect"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// initialize observes wallet prerequisites and enables bounded initial advancement on this same executor.
func (r *Reconciler) initialize(ctx context.Context, target *bitcoin.BitcoinProductionTarget, parent *network.StacksNetwork) (bool, error) {
	policy := parent.Spec.BitcoinBlockProduction
	if policy == nil || (policy.Initialization == nil && parent.Spec.Operation == nil) {
		return false, nil
	}
	rpc, ok := r.RPC.(InitializationRPC)
	if !ok {
		return false, fmt.Errorf("producer does not support declared initialization")
	}
	admitted, err := r.admit(ctx, target, parent)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.RPCTimeout)
	defer cancel()
	height, err := rpc.Height(ctx, admitted.endpoint)
	if err != nil {
		return false, err
	}
	base := target.DeepCopy()
	target.Status.ObservedHeight = height
	initializing := false
	if init := policy.Initialization; init != nil && init.Target == target.Spec.Policy.Target {
		if err := rpc.PrepareWallet(ctx, admitted.endpoint, init.Wallet, target.Spec.Policy.Address); err != nil {
			return false, err
		}
		target.Status.WalletReady = true
		initializing = height < init.InitialHeight
	}
	if !reflect.DeepEqual(base.Status, target.Status) {
		if err := r.Status().Patch(ctx, target, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return false, err
		}
	}
	if err := r.protocolGate(ctx, target, parent, height); err != nil {
		return false, err
	}
	return initializing, nil
}

// protocolGate holds baseline advancement for declared protocol prerequisites without modifying user intent.
func (r *Reconciler) protocolGate(ctx context.Context, target *bitcoin.BitcoinProductionTarget, parent *network.StacksNetwork, height int64) error {
	operation := parent.Spec.Operation
	if operation == nil {
		return nil
	}
	genesis, err := profiles.ResolveGenesis(parent.Spec.Genesis)
	if err != nil {
		return err
	}
	epochs := map[string]int64{}
	for _, epoch := range genesis.Epochs {
		epochs[epoch.Name] = epoch.StartHeight
	}
	// Initial PoX-4 enrollment needs Bitcoin-triggered legacy blocks to execute.
	// Wait for durable authorization, then permit confirmation blocks within the reward phase.
	// Core selects PoX-4 strictly after its configured activation height.
	if stageRank(target.Status.ProtocolStage) < 1 && height > epochs["2.5"] && height < epochs["3.0"] {
		allReady := true
		for _, policy := range operation.Participants {
			participant, err := r.participant(ctx, parent, policy)
			if err != nil {
				return err
			}
			if !operation.Paused && !policy.Paused && participant.Status.Phase == "Ready" && participant.Status.Protocol == "ST000000000000000000002AMW42H.pox-4" && participant.Status.UnlockHeight > height {
				continue
			}
			account := &stacks.StacksAccount{}
			if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: naming.Child(parent.Name, "account-"+policy.HolderAccount)}, account); err != nil {
				return fmt.Errorf("initial stacking account cannot be observed")
			}
			cycle := int64(genesis.PoX.RewardCycleLength)
			prepare := ((epochs["2.5"]/cycle)+1)*cycle - int64(genesis.PoX.PrepareLength)
			tx := account.Status.Transaction
			if !operation.Paused && !policy.Paused && r.pinnedCapability(parent, account, "StacksAccount") && tx != nil && tx.ConsumerUID == string(participant.UID) && tx.Receipt == nil && tx.RejectionReason == "" && height < prepare-1 {
				allReady = false
				continue
			}
			return fmt.Errorf("Bitcoin advancement is waiting for initial PoX-4 enrollment")
		}
		if allReady {
			if err := r.advanceProtocolStage(ctx, target, "PoX4"); err != nil {
				return err
			}
		}
	}
	// Nakamoto can execute prerequisite transactions while Bitcoin is held here.
	contractHold := min(epochs["3.0"]+7, epochs["4.0"]-1)
	if stageRank(target.Status.ProtocolStage) < 2 && height >= contractHold {
		for _, policy := range operation.ContractSets {
			set := &stacks.StacksContractSet{}
			if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: naming.Child(parent.Name, "contracts-"+policy.Name)}, set); err != nil {
				return fmt.Errorf("contract prerequisites cannot be observed")
			}
			if operation.Paused || policy.Paused || !r.pinnedCapability(parent, set, "StacksContractSet") || set.Spec.NetworkUID != string(parent.UID) || !reflect.DeepEqual(set.Spec.Policy, policy) || set.Status.ObservedGeneration != set.Generation || set.Status.Phase != "Ready" {
				return fmt.Errorf("Bitcoin advancement is waiting for declared contract prerequisites")
			}
		}
		if err := r.advanceProtocolStage(ctx, target, "Contracts"); err != nil {
			return err
		}
	}
	if stageRank(target.Status.ProtocolStage) < 3 && height >= epochs["4.0"]+2 {
		for _, policy := range operation.Participants {
			participant, err := r.participant(ctx, parent, policy)
			if err != nil {
				return err
			}
			if operation.Paused || policy.Paused || participant.Status.Phase != "Ready" || participant.Status.Protocol != "ST000000000000000000002AMW42H.pox-5" || participant.Status.UnlockHeight <= height {
				return fmt.Errorf("Bitcoin advancement is waiting for maintained PoX-5 participation")
			}
		}
		if err := r.advanceProtocolStage(ctx, target, "PoX5"); err != nil {
			return err
		}
	}
	return nil
}

// participant checks only the declared prerequisite's current owned incarnation.
func (r *Reconciler) participant(ctx context.Context, parent *network.StacksNetwork, policy stacks.StackingPolicy) (*stacks.StacksStackingParticipant, error) {
	p := &stacks.StacksStackingParticipant{}
	if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: naming.Child(parent.Name, "stacking-"+policy.Name)}, p); err != nil {
		return nil, fmt.Errorf("stacking prerequisite cannot be observed")
	}
	if !r.pinnedCapability(parent, p, "StacksStackingParticipant") || p.Spec.NetworkUID != string(parent.UID) || !reflect.DeepEqual(p.Spec.Policy, policy) || p.Status.ObservedGeneration != p.Generation {
		return nil, fmt.Errorf("stacking prerequisite is not current")
	}
	return p, nil
}

// pinnedCapability prevents a deleted/replaced prerequisite from reopening production.
func (r *Reconciler) pinnedCapability(parent *network.StacksNetwork, object client.Object, kind string) bool {
	if !object.GetDeletionTimestamp().IsZero() || !metav1.IsControlledBy(object, parent) {
		return false
	}
	for _, identity := range parent.Status.Capabilities {
		if identity.Kind == kind && identity.Name == object.GetName() && identity.UID == string(object.GetUID()) && identity.UID != "" {
			return true
		}
	}
	return false
}

// stageRank orders retained startup acknowledgements without treating runtime health as a production gate.
func stageRank(stage string) int {
	switch stage {
	case "PoX4":
		return 1
	case "Contracts":
		return 2
	case "PoX5":
		return 3
	}
	return 0
}

// advanceProtocolStage records a completed initialization dependency without changing desired policy.
func (r *Reconciler) advanceProtocolStage(ctx context.Context, target *bitcoin.BitcoinProductionTarget, stage string) error {
	if stageRank(target.Status.ProtocolStage) >= stageRank(stage) {
		return nil
	}
	base := target.DeepCopy()
	target.Status.ProtocolStage = stage
	return r.Status().Patch(ctx, target, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}
