package managedoperation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/accountledger"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssdk"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackstx"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ContractReconciler maintains one set of declared contract artifacts and bridge prerequisites.
type ContractReconciler struct{ Runtime *Runtime }

// SetupWithManager installs the resource-focused contract controller.
func (r *ContractReconciler) SetupWithManager(m ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(m).For(&stacks.StacksContractSet{}).Complete(r.Runtime.Binding.Wrap(r.Runtime.Reader, &stacks.StacksContractSet{}, r))
}

// Reconcile observes durable account evidence before pursuing missing contract state.
func (r *ContractReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	set := &stacks.StacksContractSet{}
	if err := r.Runtime.Reader.Get(ctx, request.NamespacedName, set); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !set.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	err := r.reconcile(ctx, set)
	return r.Runtime.result(ctx, set, err)
}

// reconcile applies declarative dependencies, not a user-supplied execution sequence.
func (r *ContractReconciler) reconcile(ctx context.Context, set *stacks.StacksContractSet) error {
	rt := r.Runtime
	parent, err := rt.parent(ctx, set, set.Spec.NetworkName, set.Spec.NetworkUID, "StacksContractSet")
	if err != nil {
		return err
	}
	account, err := rt.account(ctx, parent, set.Spec.Policy.Account)
	if err != nil {
		return err
	}
	if err := rt.drain(ctx, set, account); err != nil {
		return err
	}
	if parent.Spec.Suspended || parent.Spec.Operation == nil || parent.Spec.Operation.Paused || set.Spec.Policy.Paused {
		return errPaused
	}
	target, err := rt.Ledger.Admission.Admit(ctx, account, string(set.UID), false)
	if err != nil {
		return err
	}
	info, err := rt.RPC.Info(ctx, target.Endpoint)
	if err != nil {
		return err
	}
	observedStatus := set.Status.DeepCopy()
	observedStatus.BurnHeight = info.BurnHeight
	if err := rt.status(ctx, set, *observedStatus); err != nil {
		return err
	}
	ordered, err := protocolcontracts.Order(set.Spec.Policy.Contracts)
	if err != nil {
		return err
	}
	sources := make(map[string]string, len(ordered))
	for _, contract := range ordered {
		source, err := rt.source(ctx, set.Namespace, contract.SourceRef)
		if err != nil {
			return err
		}
		sources[contract.Name] = source
	}
	for i, contract := range ordered {
		observed, found, err := rt.RPC.Source(ctx, target.Endpoint, account.Spec.Policy.Address, contract.Name)
		if err != nil {
			return err
		}
		if found {
			if observed != sources[contract.Name] {
				return fmt.Errorf("published contract %s differs from its declared source", contract.Name)
			}
			continue
		}
		minimum, err := languageHeight(parent, contract.ClarityVersion)
		if err != nil {
			return err
		}
		if info.BurnHeight < minimum {
			return fmt.Errorf("contract %s is waiting for its language activation", contract.Name)
		}
		key, err := rt.key(ctx, set.Namespace, account.Spec.Policy.KeySecretRef, account.Spec.Policy.Address)
		if err != nil {
			return err
		}
		input := map[string]any{"operation": "publish", "contractName": contract.Name, "clarityVersion": contract.ClarityVersion,
			"source": sources[contract.Name], "fee": strconv.Itoa(len(sources[contract.Name])*10 + 3000)}
		if err := rt.signed(ctx, set, account, int64(i+1), input, key); err != nil {
			return err
		}
		return accountledger.ErrWaiting
	}
	if set.Spec.Policy.Bridge != nil {
		return r.bridge(ctx, set, account, target.Endpoint, int64(len(ordered)+1))
	}
	return nil
}

// signed binds only public intent in the ledger and injects key and nonce solely into the SDK request.
func (r *Runtime) signed(ctx context.Context, consumer client.Object, account *stacks.StacksAccount, ordinal int64, public map[string]any, key stackssdk.Key) error {
	data, err := json.Marshal(public)
	if err != nil {
		return err
	}
	return r.execute(ctx, consumer, account, accountledger.Request{Ordinal: ordinal, Digest: digest(data), Sign: func(ctx context.Context, nonce int64) (stackstx.Transaction, error) {
		input := make(map[string]any, len(public)+2)
		for k, v := range public {
			input[k] = v
		}
		input["account"], input["nonce"] = key, strconv.FormatInt(nonce, 10)
		return r.SDK.Sign(ctx, "contract-sign.mjs", input)
	}})
}

// bridge establishes the explicitly declared 2-of-2 test registry before reporting readiness.
func (r *ContractReconciler) bridge(ctx context.Context, set *stacks.StacksContractSet, account *stacks.StacksAccount, endpoint string, ordinal int64) error {
	rt, bridge, address := r.Runtime, set.Spec.Policy.Bridge, account.Spec.Policy.Address
	if len(bridge.SignerPublicKeys) != 2 {
		return fmt.Errorf("bridge initialization requires two retained signer keys")
	}
	keys := []stackssdk.Argument{}
	expectedSet := "0x0b00000002"
	for _, key := range bridge.SignerPublicKeys {
		keys = append(keys, stackssdk.Argument{Type: "buffer", Value: key})
		expectedSet += "0200000021" + key
	}
	aggregate, err := rt.RPC.ReadOnly(ctx, endpoint, address, address, "sbtc-registry", "get-current-aggregate-pubkey", nil)
	if err != nil {
		return err
	}
	signers, err := rt.RPC.ReadOnly(ctx, endpoint, address, address, "sbtc-registry", "get-current-signer-set", nil)
	if err != nil {
		return err
	}
	threshold, err := rt.RPC.DataVariable(ctx, endpoint, address, "sbtc-registry", "current-signature-threshold")
	if err != nil {
		return err
	}
	if aggregate == "0x0200000021"+bridge.AggregatePublicKey && signers == expectedSet && threshold == fmt.Sprintf("0x01%032x", 2) {
		return nil
	}
	if account.Status.Transaction != nil && account.Status.Transaction.Ordinal >= ordinal && account.Status.Transaction.Receipt != nil {
		return fmt.Errorf("observed bridge state diverges from acknowledged initialization")
	}
	key, err := rt.key(ctx, set.Namespace, account.Spec.Policy.KeySecretRef, address)
	if err != nil {
		return err
	}
	input := map[string]any{"operation": "call", "contractAddress": address, "contractName": "sbtc-bootstrap-signers", "functionName": "rotate-keys-wrapper", "fee": "3000",
		"arguments": []stackssdk.Argument{{Type: "list", Items: keys}, {Type: "buffer", Value: bridge.AggregatePublicKey}, {Type: "uint", Value: "2"}}}
	if err := rt.signed(ctx, set, account, ordinal, input, key); err != nil {
		return err
	}
	return accountledger.ErrWaiting
}

// languageHeight resolves supported contract language activation from immutable genesis.
func languageHeight(parent *network.StacksNetwork, version int32) (int64, error) {
	genesis, err := profiles.ResolveGenesis(parent.Spec.Genesis)
	if err != nil {
		return 0, err
	}
	name := "3.0"
	if version == 6 {
		name = "4.0"
	} else if version != 3 {
		return 0, fmt.Errorf("unsupported contract language version")
	}
	for _, epoch := range genesis.Epochs {
		if epoch.Name == name {
			return epoch.StartHeight, nil
		}
	}
	return 0, fmt.Errorf("contract language activation is absent")
}

// errPaused distinguishes explicit desired pause from an observation failure.
var errPaused = errors.New("managed operation is paused")

// result publishes bounded convergence status and retries observations without error-log backoff.
func (r *Runtime) result(ctx context.Context, object client.Object, err error) (ctrl.Result, error) {
	status := operationStatus(object).DeepCopy()
	status.Phase, status.Message = "Ready", "Declared on-chain state is observed"
	if err != nil {
		status.Phase, status.Message = "Waiting", err.Error()
		if len(status.Message) > 1024 {
			status.Message = status.Message[:1024]
		}
		switch {
		case errors.Is(err, errPaused):
			status.Phase = "Paused"
		case errors.Is(err, accountledger.ErrWaiting):
			status.Phase = "Pending"
		case errors.Is(err, errExecutionFailed), errors.Is(err, accountledger.ErrSuperseded):
			status.Phase = "Blocked"
		}
	}
	return ctrl.Result{RequeueAfter: 2 * time.Second}, r.status(ctx, object, *status)
}
