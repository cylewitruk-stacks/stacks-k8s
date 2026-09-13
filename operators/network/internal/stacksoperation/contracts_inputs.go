package stacksoperation

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
)

// ContractNode exposes only native deployment, registry observation and the shared nonce stream.
type ContractNode interface {
	Node
	ChainView(context.Context) (rpc.ChainView, error)
	NakamotoTip(context.Context, rpc.ChainView) (bool, error)
	AccountAt(context.Context, string, string) (rpc.Account, error)
	SourceAt(context.Context, string, string, string) (string, bool, error)
	ReadOnlyAt(context.Context, string, string, string, string, string, []clarity.Value) (clarity.Value, error)
}

// ContractInputs binds one pinned source bundle and exact public test registry to frozen genesis.
type ContractInputs struct {
	// Node is the currently admitted native mutation ingress.
	Node ContractNode
	// Deployer is verified against the sole mounted signing key.
	Deployer string
	// Bundle identifies the pinned profile source manifest.
	Bundle string
	// SourceHashes captures every exact source hash from immutable genesis.
	SourceHashes map[string]string
	// SignerPublicKeys retains the explicitly declared registry key order.
	SignerPublicKeys []string
	// AggregatePublicKey is the explicit test registry aggregate key.
	AggregatePublicKey string
	// Threshold is the captured registry quorum.
	Threshold uint64
	// Epoch3Height gates Clarity-3 publication by the frozen activation schedule.
	Epoch3Height uint64
}

// Contracts resolves public source, deployer and registry identities without reading private keys.
func (r PublicInputs) Contracts(ctx context.Context, s stacksworker.Snapshot) (ContractInputs, error) {
	var in ContractInputs
	if r.Reader == nil || s.Network == nil || s.Participant == nil || s.Participant.Status.Admission == nil {
		return in, errors.New("contract admission unavailable")
	}
	policy := s.Participant.Status.Admission.Configuration.StacksContractSet
	if policy == nil || policy.DeployerAccountRef == nil || policy.TargetNodeRef == nil || policy.Bundle == nil || policy.Initialization == nil || policy.Initialization.Mode != "ExplicitTestRegistry" {
		return in, errors.New("contract policy incomplete")
	}
	deployer, err := r.account(ctx, s.Participant, policy.DeployerAccountRef.Name)
	if err != nil {
		return in, err
	}
	if deployer.Status.Identity.Address != r.Sender {
		return in, errors.New("contract deployer changed")
	}
	target, err := r.target(ctx, s, policy.TargetNodeRef.Name)
	if err != nil {
		return in, err
	}
	endpoint := ""
	for _, ep := range target.Status.Runtime.Endpoints {
		if ep.Name == "rpc" && ep.Host != "" && ep.Port > 0 && ep.Port <= 65535 {
			endpoint = (&url.URL{Scheme: "http", Host: net.JoinHostPort(ep.Host, strconv.Itoa(int(ep.Port)))}).String()
		}
	}
	node, err := rpc.New(rpc.Config{Endpoint: endpoint, Timeout: 10 * time.Second, MaxResponseBytes: 2 << 20})
	if err != nil {
		return in, err
	}
	genesis, err := r.inputGenesis(ctx, s, api.ParticipantStacksContractSet)
	if err != nil {
		return in, err
	}
	frozen := genesis.Spec.Chain.Contracts
	if frozen.Deployer != r.Sender || frozen.Bundle != *policy.Bundle {
		return in, errors.New("frozen contract binding differs")
	}
	captured, err := initialRequirement(genesis, s.Participant)
	if err != nil {
		return in, err
	}
	initial := captured != nil
	if captured == nil {
		for i := range genesis.Spec.Bootstrap.Requirements {
			required := &genesis.Spec.Bootstrap.Requirements[i]
			if required.Kind != api.ParticipantStacksContractSet {
				continue
			}
			if captured != nil {
				return in, errors.New("ambiguous frozen registry requirements")
			}
			captured = required
		}
	}
	if captured == nil || captured.RegistryInitialization == nil || initial && foundation.Digest(captured.RegistryInitialization) != foundation.Digest(policy.Initialization) {
		return in, errors.New("captured registry policy unavailable")
	}
	accountCaptured := func(name string) (string, error) {
		account, err := r.account(ctx, s.Participant, name)
		if err != nil {
			return "", err
		}
		if !initial {
			return account.Status.Identity.PublicKey, nil
		}
		for _, bound := range captured.Accounts {
			if bound.Binding.UID == account.UID && bound.Binding.Fingerprint == account.Status.Digest && bound.Identity == *account.Status.Identity {
				return account.Status.Identity.PublicKey, nil
			}
		}
		return "", errors.New("captured registry account differs")
	}
	if _, err = accountCaptured(policy.DeployerAccountRef.Name); err != nil {
		return in, err
	}
	keys := make([]string, 0, len(policy.Initialization.SignerAccountRefs))
	for _, ref := range policy.Initialization.SignerAccountRefs {
		key, err := accountCaptured(ref.Name)
		if err != nil {
			return in, err
		}
		keys = append(keys, key)
	}
	aggregate, err := accountCaptured(policy.Initialization.AggregateKeyAccountRef.Name)
	if err != nil {
		return in, err
	}
	// Replacement instances may use new account aliases, but the public registry remains frozen.
	registry := captured.RegistryInitialization
	publicKey := func(name string) string {
		for _, account := range captured.Accounts {
			if account.Binding.Name == name {
				return account.Identity.PublicKey
			}
		}
		return ""
	}
	if registry.Mode != policy.Initialization.Mode || registry.Threshold != policy.Initialization.Threshold || len(registry.SignerAccountRefs) != len(keys) || aggregate == "" || aggregate != publicKey(registry.AggregateKeyAccountRef.Name) {
		return in, errors.New("frozen registry public values differ")
	}
	for i, ref := range registry.SignerAccountRefs {
		if keys[i] == "" || keys[i] != publicKey(ref.Name) {
			return in, errors.New("frozen registry signer order or identity differs")
		}
	}
	in = ContractInputs{Node: node, Deployer: r.Sender, Bundle: frozen.Bundle, SourceHashes: frozen.SourceHashes, SignerPublicKeys: keys, AggregatePublicKey: aggregate, Threshold: uint64(policy.Initialization.Threshold)}
	for _, epoch := range genesis.Spec.Chain.Epochs {
		if epoch.Name == "3.0" && epoch.StartHeight > 0 {
			in.Epoch3Height = uint64(epoch.StartHeight)
		}
	}
	if in.Epoch3Height == 0 {
		return ContractInputs{}, errors.New("frozen Clarity-3 activation unavailable")
	}
	return in, nil
}
