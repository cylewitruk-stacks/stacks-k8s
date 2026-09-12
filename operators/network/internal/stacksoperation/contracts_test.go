package stacksoperation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
)

// contractNode models native facts independently of submission acknowledgements and transaction IDs.
type contractNode struct {
	input                                      ContractInputs
	sources                                    map[string]string
	sent                                       []transaction.Transaction
	nonce, burn                                uint64
	infos                                      int
	movingTip, initialized, included, rejected bool
	submitErr                                  error
	mismatch                                   string
	principal                                  string
	legacy                                     bool
}

func (n *contractNode) Account(context.Context, string) (rpc.Account, error) {
	return rpc.Account{Nonce: n.nonce, Balance: clarity.Uint(10000000), Locked: clarity.Uint(0)}, nil
}
func (n *contractNode) Submit(_ context.Context, tx transaction.Transaction) error {
	n.sent = append(n.sent, tx)
	return n.submitErr
}
func (n *contractNode) Inclusion(context.Context, string) (rpc.Inclusion, error) {
	return rpc.Inclusion{Found: n.included, Success: !n.rejected, BlockID: strings.Repeat("c", 64)}, nil
}
func (n *contractNode) Info(context.Context) (rpc.Info, error) {
	n.infos++
	tip := strings.Repeat("a", 64)
	if n.movingTip && n.infos%2 == 0 {
		tip = strings.Repeat("b", 64)
	}
	return rpc.Info{NetworkID: 0x80000000, BurnHeight: n.burn, StacksHeight: 3, Tip: tip}, nil
}
func (n *contractNode) Source(_ context.Context, _, name string) (string, bool, error) {
	s, ok := n.sources[name]
	return s, ok, nil
}
func (n *contractNode) ReadOnly(_ context.Context, _, _, contract, method string, args []clarity.Value) (clarity.Value, error) {
	if method == "pubkeys-to-principal" {
		if contract != "sbtc-bootstrap-signers" || len(args) != 2 || args[0].Type != clarity.List || len(args[0].Items) != len(n.input.SignerPublicKeys) {
			return clarity.Value{}, errors.New("invalid principal derivation")
		}
		return clarity.Principal(n.principal)
	}
	if method != "get-current-signer-data" || contract != "sbtc-registry" || len(args) != 0 {
		return clarity.Value{}, errors.New("unexpected native method")
	}
	keys := clarity.Value{Type: clarity.List}
	aggregate := clarity.Value{Type: clarity.Buffer, Bytes: []byte{0}}
	threshold := clarity.Uint(0)
	principal, _ := clarity.Principal(n.input.Deployer)
	if n.initialized {
		desired, _ := registryArguments(n.input)
		keys, aggregate, threshold = desired[0], desired[1], desired[2]
		principal, _ = clarity.Principal(n.principal)
		switch n.mismatch {
		case "keys":
			keys.Items = keys.Items[:1]
		case "aggregate":
			aggregate.Bytes = []byte{0}
		case "threshold":
			threshold = clarity.Uint(1)
		case "principal":
			principal, _ = clarity.Principal(n.input.Deployer)
		}
	}
	return clarity.Value{Type: clarity.Tuple, Fields: map[string]clarity.Value{"current-signer-set": keys, "current-aggregate-pubkey": aggregate, "current-signature-threshold": threshold, "current-signer-principal": principal}}, nil
}

// contractFixture uses original minimal test sources, never claimed as real sBTC compatibility proof.
func contractFixture(t *testing.T) (*ContractRole, *contractNode, stacksworker.Snapshot) {
	t.Helper()
	key := strings.Repeat("0", 63) + "1"
	deployer, _ := identity.FromPrivate(key)
	key, _ = identity.CompressedPrivateKey(key)
	pub2, _ := identity.FromPrivate(strings.Repeat("0", 63) + "2")
	pub3, _ := identity.FromPrivate(strings.Repeat("0", 63) + "3")
	sources := []protocolcontracts.Source{}
	hashes := map[string]string{}
	for _, name := range []string{"sbtc-registry", "sbtc-token", "sbtc-bootstrap-signers", "sbtc-deposit", "sbtc-withdrawal"} {
		source := ";; original test source for " + name + "\n(define-read-only (test) true)"
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
		sources = append(sources, protocolcontracts.Source{Name: name, SHA256: hash, ClarityVersion: 3, Source: source})
		hashes[name] = hash
	}
	in := ContractInputs{Deployer: deployer.Address, Bundle: "sbtc-regtest-v1", SourceHashes: hashes, SignerPublicKeys: []string{deployer.PublicKey, pub2.PublicKey}, AggregatePublicKey: pub3.PublicKey, Threshold: 2, Epoch3Height: 252}
	principal, _ := identity.EncodeAddress(21, [20]byte{1})
	n := &contractNode{input: in, sources: map[string]string{}, burn: 252, principal: principal}
	in.Node = n
	r := &ContractRole{key: key, sources: sources, stream: NonceStream{Address: deployer.Address}, Now: func() time.Time { return time.Unix(1000, 0) }, Resolve: func(context.Context, stacksworker.Snapshot) (ContractInputs, error) { return in, nil }}
	snapshot := stacksworker.Snapshot{Participant: &api.StacksNetworkParticipant{Status: api.ParticipantStatus{Admission: &api.Admission{PolicyDigest: "sha256:policy"}}}, Authorize: func(context.Context) error { return nil }}
	return r, n, snapshot
}

func TestContractDeploymentAndRegistryLostAcknowledgementNeverReplay(t *testing.T) {
	r, n, s := contractFixture(t)
	n.submitErr = errors.New("lost acknowledgement")
	ctx := context.Background()
	for i, source := range r.sources {
		result, err := r.Step(ctx, s)
		if err != nil || result.Pending != 1 || len(n.sent) != i+1 {
			t.Fatalf("offer %d: %+v %v", i, result, err)
		}
		s.Paused = true
		for j := 0; j < 3; j++ {
			result, _ = r.Step(ctx, s)
			if result.Pending != 1 || len(n.sent) != i+1 {
				t.Fatal("unknown deployment was replayed")
			}
		}
		n.sources[source.Name] = source.Source
		n.nonce++
		result, _ = r.Step(ctx, s)
		if result.Pending != 0 || result.Transactions.Included != 0 || result.Transactions.PostconditionObserved != uint64(i+1) || result.Transactions.LastPostcondition.Kind != "ContractDeployment" {
			t.Fatalf("distinct canonical source proof missing: %+v", result)
		}
		s.Paused = false
	}
	result, _ := r.Step(ctx, s)
	if result.Pending != 1 || len(n.sent) != 6 {
		t.Fatalf("registry offer missing: %+v", result)
	}
	n.initialized = true
	n.nonce++
	s.Paused = true
	result, _ = r.Step(ctx, s)
	if result.Pending != 0 || result.Contracts == nil || !result.Contracts.Complete || result.Contracts.SignerPrincipal != n.principal || result.Transactions.Included != 0 || result.Transactions.PostconditionObserved != 6 || result.Transactions.LastPostcondition.Kind != "RegistryInitialization" {
		t.Fatalf("registry proof missing: %+v", result)
	}
	s.Paused = false
	result, _ = r.Step(ctx, s)
	if result.Contracts == nil || len(n.sent) != 6 {
		t.Fatal("converged contract state was replayed")
	}
}

func TestContractObservationRequiresStableTipAndExactNextNonce(t *testing.T) {
	r, n, s := contractFixture(t)
	ctx := context.Background()
	n.movingTip = true
	result, _ := r.Step(ctx, s)
	if len(n.sent) != 0 || result.Failed {
		t.Fatal("moving initial observation authorized or failed")
	}
	n.movingTip = false
	r.Step(ctx, s)
	n.sources[r.sources[0].Name] = r.sources[0].Source
	n.nonce = 2
	for i := 0; i < 3; i++ {
		result, _ = r.Step(ctx, s)
		if result.Pending != 1 || result.Transactions.PostconditionObserved != 0 || len(n.sent) != 1 {
			t.Fatal("nonce jump attributed ambiguous operation")
		}
	}
	n.nonce = 1
	n.movingTip = true
	result, _ = r.Step(ctx, s)
	if result.Pending != 1 || result.Transactions.PostconditionObserved != 0 {
		t.Fatal("moving tip settled ambiguous operation")
	}
}

func TestContractSourceAndRegistryConflictsCannotAuthorize(t *testing.T) {
	ctx := context.Background()
	for _, mismatch := range []string{"source", "keys", "aggregate", "threshold", "principal"} {
		t.Run(mismatch, func(t *testing.T) {
			r, n, s := contractFixture(t)
			for _, source := range r.sources {
				n.sources[source.Name] = source.Source
			}
			n.initialized = true
			n.mismatch = mismatch
			if mismatch == "source" {
				n.sources[r.sources[0].Name] = "different immutable source"
			}
			n.movingTip = true
			result, _ := r.Step(ctx, s)
			if result.Failed || len(n.sent) != 0 {
				t.Fatal("unbracketed disagreement became a terminal fact")
			}
			n.movingTip = false
			result, _ = r.Step(ctx, s)
			if !result.Failed || result.Contracts != nil || len(n.sent) != 0 {
				t.Fatal("exact source/registry disagreement accepted")
			}
		})
	}
}

func TestContractActivationAuthorizationAndKeyBoundaries(t *testing.T) {
	r, n, s := contractFixture(t)
	ctx := context.Background()
	n.burn = 251
	r.Step(ctx, s)
	if len(n.sent) != 0 {
		t.Fatal("publication before frozen Clarity-3 activation")
	}
	n.burn = 252
	s.Paused = true
	r.Step(ctx, s)
	if len(n.sent) != 0 {
		t.Fatal("paused publication")
	}
	s.Paused = false
	s.Authorize = func(context.Context) error { return errors.New("withdrawn") }
	result, _ := r.Step(ctx, s)
	if len(n.sent) != 0 || result.Pending != 0 {
		t.Fatal("publication without fresh authorization")
	}
	key := strings.Repeat("0", 63) + "1"
	if _, err := NewContractRole(key, n.input.Deployer, t.TempDir()); err == nil {
		t.Fatal("unpinned external source directory accepted")
	}
	in := n.input
	in.Threshold = 1
	if _, err := registryArguments(in); err == nil {
		t.Fatal("non-majority registry accepted")
	}
	in = n.input
	in.SignerPublicKeys = in.SignerPublicKeys[:1]
	if _, err := registryArguments(in); err == nil {
		t.Fatal("single-key registry accepted")
	}
	// Native transaction encoding must retain an actual public-key-authenticated signature.
	s.Authorize = func(context.Context) error { return nil }
	r.Step(ctx, s)
	if len(n.sent) != 1 || !transaction.Valid(n.sent[0]) || !strings.Contains(hex.EncodeToString(n.sent[0].Bytes), hex.EncodeToString([]byte("sbtc-registry"))) {
		t.Fatal("deployment transaction not signed or wrong payload")
	}
}

// ChainView supplies full fork identity independently of the read-only result bytes.
func (n *contractNode) ChainView(ctx context.Context) (rpc.ChainView, error) {
	info, err := n.Info(ctx)
	return rpc.ChainView{Info: info, ConsensusHash: strings.Repeat("c", 40), BurnConsensusHash: strings.Repeat("d", 40), IndexBlockID: info.Tip, FullySynced: true}, err
}

// NakamotoTip models native header-family evidence separately from Bitcoin height.
func (n *contractNode) NakamotoTip(context.Context, rpc.ChainView) (bool, error) {
	return !n.legacy, nil
}

func TestContractsWaitForCanonicalNakamotoDespiteAdvancedBitcoin(t *testing.T) {
	r, n, s := contractFixture(t)
	n.burn, n.legacy = 260, true
	for i := 0; i < 3; i++ {
		result, err := r.Step(context.Background(), s)
		if err != nil || result.Reason != "AwaitingClarity3" || result.Pending != 0 || len(n.sent) != 0 {
			t.Fatalf("legacy tip authorized Clarity 3: %+v %v sends=%d", result, err, len(n.sent))
		}
	}
	n.legacy = false
	result, err := r.Step(context.Background(), s)
	if err != nil || result.Pending != 1 || len(n.sent) != 1 {
		t.Fatalf("Nakamoto tip did not permit one deployment: %+v %v sends=%d", result, err, len(n.sent))
	}
}
func (n *contractNode) AccountAt(ctx context.Context, address, tip string) (rpc.Account, error) {
	if tip != strings.Repeat("a", 64) {
		return rpc.Account{}, errors.New("account tip not pinned")
	}
	return n.Account(ctx, address)
}
func (n *contractNode) SourceAt(ctx context.Context, address, name, tip string) (string, bool, error) {
	if tip != strings.Repeat("a", 64) {
		return "", false, errors.New("source tip not pinned")
	}
	return n.Source(ctx, address, name)
}
func (n *contractNode) ReadOnlyAt(ctx context.Context, tip, sender, address, contract, method string, args []clarity.Value) (clarity.Value, error) {
	if tip != strings.Repeat("a", 64) {
		return clarity.Value{}, errors.New("registry tip not pinned")
	}
	return n.ReadOnly(ctx, sender, address, contract, method, args)
}
