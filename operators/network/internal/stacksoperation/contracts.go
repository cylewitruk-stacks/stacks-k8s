package stacksoperation

import (
	"context"
	"errors"
	"math/big"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// contractGoal retains one exact public postcondition and original ingress until settlement.
type contractGoal struct {
	input ContractInputs
	kind  api.PostconditionKind
	name  string
}

// ContractRole deploys pinned real sBTC and converges the explicit registry with one nonce stream.
type ContractRole struct {
	// Resolve reads current admitted public policy and immutable captured prerequisites.
	Resolve func(context.Context, stacksworker.Snapshot) (ContractInputs, error)
	// Now injects the observation clock for deterministic tests.
	Now     func() time.Time
	key     string
	sources []protocolcontracts.Source
	stream  NonceStream
	goal    *contractGoal
	applied string
	// inputs retains the last locally validated public contract baseline.
	inputs  appliedInputs[ContractInputs]
	failed  bool
	current *api.ContractSetObservation
}

// NewContractRole validates the deployer key and every external pinned source before use.
func NewContractRole(key, deployer, sourceDirectory string) (*ContractRole, error) {
	compressed, err := identity.CompressedPrivateKey(key)
	if err != nil {
		return nil, errors.New("invalid contract deployer key")
	}
	public, err := identity.FromPrivate(compressed)
	if err != nil || public.Address != deployer {
		return nil, errors.New("contract deployer key differs")
	}
	sources, err := protocolcontracts.Load(sourceDirectory)
	if err != nil {
		return nil, err
	}
	return &ContractRole{key: compressed, sources: sources, stream: NonceStream{Address: deployer}}, nil
}

// now reads the injected observation clock.
func (r *ContractRole) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// validInputs requires the complete release bundle, captured public keys and frozen activation.
func (r *ContractRole) validInputs(in ContractInputs) bool {
	if in.Node == nil || in.Deployer != r.stream.Address || in.Bundle != "sbtc-regtest-v1" || in.Epoch3Height == 0 ||
		len(in.SourceHashes) != len(r.sources) ||
		len(r.sources) != 5 {
		return false
	}
	for _, source := range r.sources {
		if source.ClarityVersion != 3 || in.SourceHashes[source.Name] != source.SHA256 {
			return false
		}
	}
	_, err := registryArguments(in)
	return err == nil
}

// Step observes pending work while paused and authorizes each new send immediately beforehand.
func (r *ContractRole) Step(ctx context.Context, snapshot stacksworker.Snapshot) (stacksworker.RoleResult, error) {
	r.current = nil
	if r.failed {
		return r.result(reasonContractOperationFailed), nil
	}
	if r.goal != nil {
		return r.result(r.observeGoal(ctx)), nil
	}
	if r.Resolve == nil || snapshot.Participant == nil || snapshot.Participant.Status.Admission == nil {
		return r.result(reasonPolicyUnavailable), nil
	}
	in, resolved, err := r.inputs.resolve(ctx, snapshot, r.applied, r.Resolve, cloneContractInputs)
	if err != nil {
		//nolint:nilerr // Publish the reason and retry without failing the worker session.
		return r.result(reasonDependenciesUnavailable), nil
	}
	snapshot = resolved
	if !r.validInputs(in) {
		r.inputs.invalidate(snapshot, r.applied)
		return r.result(reasonInvalidContractPolicy), nil
	}
	r.inputs.remember(snapshot, in, cloneContractInputs)
	r.applied = snapshot.Participant.Status.Admission.PolicyDigest
	state, err := observeContracts(ctx, in, r.sources, r.now())
	if err != nil {
		if errors.Is(err, errContractConflict) {
			r.failed = true
		}
		return r.result(api.ReasonContractObservationUnavailable), nil
	}
	r.current = state.observation
	if !state.nakamoto {
		return r.result(reasonAwaitingClarity3), nil
	}
	if snapshot.Paused {
		return r.result(reasonPaused), nil
	}
	for _, source := range r.sources {
		if !state.sources[source.Name] {
			return r.offer(ctx, snapshot, in, api.PostconditionContractDeployment, source)
		}
	}
	if state.observation != nil {
		return r.result(reasonContractSetObserved), nil
	}
	if !state.registryEmpty {
		return r.result(reasonRegistryObservationUnavailable), nil
	}
	return r.offer(ctx, snapshot, in, api.PostconditionRegistryInitialization, protocolcontracts.Source{})
}

// offer signs once with explicit fee and preserves the original operation across uncertain sends.
func (r *ContractRole) offer(
	ctx context.Context,
	s stacksworker.Snapshot,
	in ContractInputs,
	kind api.PostconditionKind,
	source protocolcontracts.Source,
) (stacksworker.RoleResult, error) {
	fee := uint64(3000)
	if kind == api.PostconditionContractDeployment {
		fee += uint64(len(source.Source)) * 10
	}
	reason, _ := r.stream.Offer(
		ctx,
		r.now,
		in.Node,
		new(big.Int).SetUint64(fee),
		s.Authorize,
		func(nonce uint64) (transaction.Transaction, error) {
			options := transaction.Options{
				Version:           transaction.Testnet,
				ChainID:           0x80000000,
				Nonce:             nonce,
				Fee:               fee,
				PostConditionMode: transaction.Deny,
				PrivateKey:        r.key,
			}
			if kind == api.PostconditionContractDeployment {
				if source.ClarityVersion < 1 || source.ClarityVersion > 6 {
					return transaction.Transaction{}, errors.New("unsupported Clarity publication version")
				}
				return transaction.Deploy(options, source.Name, source.Source, byte(source.ClarityVersion))
			}
			args, err := registryArguments(in)
			if err != nil {
				return transaction.Transaction{}, err
			}
			return transaction.Call(options, in.Deployer, "sbtc-bootstrap-signers", "rotate-keys-wrapper", args)
		},
	)
	if r.stream.Pending() != 0 {
		in = cloneContractInputs(in)
		r.goal = &contractGoal{input: in, kind: kind, name: source.Name}
	}
	return r.result(reason), nil
}

// observeGoal never resends; native state and canonical nonce are separate from TxID inclusion.
func (r *ContractRole) observeGoal(ctx context.Context) string {
	goal := r.goal
	reason, _ := r.stream.Observe(ctx, r.now())
	if reason == reasonExecutionRejected {
		r.failed = true
		return reason
	}
	state, err := observeContracts(ctx, goal.input, r.sources, r.now())
	if err != nil {
		if errors.Is(err, errContractConflict) {
			r.failed = true
		}
		return api.ReasonContractObservationUnavailable
	}
	r.current = state.observation
	if goal.kind == api.PostconditionContractDeployment && !state.sources[goal.name] ||
		goal.kind == api.PostconditionRegistryInitialization && state.observation == nil {
		return reasonAwaitingContractPostcondition
	}
	if r.stream.Pending() != 0 {
		proof := struct {
			Kind, Deployer, Contract, SourceHash string
			Registry                             *api.ContractSetObservation
		}{
			Kind:       string(goal.kind),
			Deployer:   goal.input.Deployer,
			Contract:   goal.name,
			SourceHash: goal.input.SourceHashes[goal.name],
		}
		if goal.kind == api.PostconditionRegistryInitialization {
			proof.Registry = state.observation.DeepCopy()
			proof.Registry.ObservedAt = metav1.Time{}
		}
		evidence := api.TransactionPostcondition{
			TxID:        r.stream.pending.transaction.TxID,
			Kind:        goal.kind,
			StateDigest: foundation.Digest(proof),
			StacksTip:   state.info.IndexBlockID,
			ObservedAt:  metav1.NewTime(r.now().UTC()),
		}
		reason, err = r.stream.SettleObserved(evidence.TxID, state.account.Nonce, evidence)
		if err != nil || r.stream.Pending() != 0 {
			return reason
		}
	}
	r.goal = nil
	if reason == reasonIdle {
		return reasonContractPostconditionObserved
	}
	return reason
}

// Drain observes only the captured pending operation until framework-bounded disposal.
func (r *ContractRole) Drain(ctx context.Context, _ stacksworker.Snapshot) (stacksworker.DrainResult, error) {
	r.current = nil
	if r.goal != nil {
		r.observeGoal(ctx)
	}
	pending := r.pending()
	return stacksworker.DrainResult{
		Done:         pending == 0,
		Settled:      pending == 0,
		Pending:      pending,
		Transactions: r.stream.Facts(),
		Contracts:    r.current.DeepCopy(),
	}, nil
}

// pending includes included transactions whose public postcondition is still unobserved.
func (r *ContractRole) pending() int32 {
	if r.goal != nil {
		return 1
	}
	return r.stream.Pending()
}

// result returns independent bounded facts for retry-safe publication.
func (r *ContractRole) result(reason string) stacksworker.RoleResult {
	return stacksworker.RoleResult{
		Contracts:           r.current.DeepCopy(),
		Transactions:        r.stream.Facts(),
		AppliedPolicyDigest: r.applied,
		Pending:             r.pending(),
		Reason:              reason,
		Failed:              r.failed,
		RequeueAfter:        time.Second,
	}
}

// cloneContractInputs preserves immutable deployment prerequisites across later policy edits.
func cloneContractInputs(in ContractInputs) ContractInputs {
	in.SignerPublicKeys = append([]string(nil), in.SignerPublicKeys...)
	hashes := make(map[string]string, len(in.SourceHashes))
	for name, hash := range in.SourceHashes {
		hashes[name] = hash
	}
	in.SourceHashes = hashes
	return in
}
