package stacksoperation

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// poxNode models public native state independently of transaction acknowledgement/inclusion.
type poxNode struct {
	holder, signer                    string
	nonce, burn, cycle, first, period uint64
	present                           bool
	amount                            *big.Int
	sent                              []transaction.Transaction
	submitErr                         error
	included                          bool
	executionSuccess                  bool
	changedTip                        bool
	changedConsensus                  bool
	infos                             int
	mismatch                          string
}

func (n *poxNode) Account(context.Context, string) (rpc.Account, error) {
	locked := clarity.Uint(0)
	if n.present {
		locked, _ = clarity.Uint128(n.amount.String())
	}
	return rpc.Account{Nonce: n.nonce, Balance: clarity.Uint(1000000), Locked: locked, UnlockHeight: (n.first + n.period) * 20}, nil
}
func (n *poxNode) Submit(_ context.Context, tx transaction.Transaction) error {
	n.sent = append(n.sent, tx)
	return n.submitErr
}
func (n *poxNode) Inclusion(context.Context, string) (rpc.Inclusion, error) {
	return rpc.Inclusion{Found: n.included, Success: n.executionSuccess, BlockID: strings.Repeat("b", 64)}, nil
}
func (n *poxNode) Info(context.Context) (rpc.Info, error) {
	n.infos++
	tip := strings.Repeat("a", 64)
	if n.changedTip && n.infos%2 == 0 {
		tip = strings.Repeat("b", 64)
	}
	return rpc.Info{NetworkID: 0x80000000, BurnHeight: n.burn, StacksHeight: 10, Tip: tip}, nil
}

// ChainView supplies distinct sortition and header identity even when the header is unchanged.
func (n *poxNode) ChainView(ctx context.Context) (rpc.ChainView, error) {
	info, e := n.Info(ctx)
	consensus := strings.Repeat("c", 40)
	index := strings.Repeat("d", 64)
	if n.changedConsensus && n.infos%2 == 0 {
		consensus = strings.Repeat("e", 40)
		index = strings.Repeat("f", 64)
	}
	return rpc.ChainView{Info: info, ConsensusHash: consensus, BurnConsensusHash: strings.Repeat("b", 40), IndexBlockID: index, FullySynced: true}, e
}
func (n *poxNode) AccountAt(ctx context.Context, address, tip string) (rpc.Account, error) {
	if tip != strings.Repeat("d", 64) {
		return rpc.Account{}, errors.New("account read was not pinned")
	}
	return n.Account(ctx, address)
}
func (n *poxNode) PoXAt(ctx context.Context, tip string) (rpc.PoX, error) {
	if tip != strings.Repeat("d", 64) {
		return rpc.PoX{}, errors.New("PoX read was not pinned")
	}
	return n.PoX(ctx)
}
func (n *poxNode) ReadOnlyAt(ctx context.Context, tip, sender, address, contract, method string, args []clarity.Value) (clarity.Value, error) {
	if tip != strings.Repeat("d", 64) {
		return clarity.Value{}, errors.New("contract read was not pinned")
	}
	return n.ReadOnly(ctx, sender, address, contract, method, args)
}
func (n *poxNode) PoX(context.Context) (rpc.PoX, error) {
	return rpc.PoX{Contract: PoX4Contract, BurnHeight: n.burn, RewardCycle: n.cycle, CycleLength: 20, MinThreshold: clarity.Uint(1), BlocksUntilPrepare: 5}, nil
}
func (n *poxNode) ReadOnly(_ context.Context, _, _, _, method string, args []clarity.Value) (clarity.Value, error) {
	if !n.present {
		return clarity.Value{Type: clarity.None}, nil
	}
	payout, _ := pox4Payout(n.holder)
	address, _ := payout.Value()
	if n.mismatch == "payout" {
		address.Fields["hashbytes"] = clarity.Value{Type: clarity.Buffer, Bytes: make([]byte, 20)}
	}
	if method == "get-stacker-info" {
		indexes := []clarity.Value{}
		for i := uint64(0); i < n.period; i++ {
			indexes = append(indexes, clarity.Uint(100+i))
		}
		fields := map[string]clarity.Value{"pox-addr": address, "first-reward-cycle": clarity.Uint(n.first), "lock-period": clarity.Uint(n.period), "reward-set-indexes": {Type: clarity.List, Items: indexes}, "delegated-to": {Type: clarity.None}}
		if n.mismatch == "delegated" {
			fields["delegated-to"] = clarity.Value{Type: clarity.Some, Items: []clarity.Value{{Type: clarity.StandardPrincipal, Text: n.holder}}}
		}
		return clarity.Value{Type: clarity.Some, Items: []clarity.Value{{Type: clarity.Tuple, Fields: fields}}}, nil
	}
	if method != "get-reward-set-pox-address" {
		return clarity.Value{}, errors.New("unexpected native method")
	}
	cycle, _ := unsigned(args[0])
	index, _ := unsigned(args[1])
	if cycle < n.first || cycle >= n.first+n.period || index != 100+cycle-n.first {
		return clarity.Value{}, errors.New("wrong cycle/index lookup")
	}
	public, _ := hex.DecodeString(n.signer)
	if n.mismatch == "signer" {
		public[1] ^= 1
	}
	amount, _ := clarity.Uint128(n.amount.String())
	if n.mismatch == "amount" {
		amount = clarity.Uint(1)
	}
	holder, _ := clarity.Principal(n.holder)
	if n.mismatch == "holder" {
		holder.Text = "ST000000000000000000002AMW42H"
	}
	fields := map[string]clarity.Value{"pox-addr": address, "total-ustx": amount, "stacker": {Type: clarity.Some, Items: []clarity.Value{holder}}, "signer": {Type: clarity.Buffer, Bytes: public}}
	return clarity.Value{Type: clarity.Some, Items: []clarity.Value{{Type: clarity.Tuple, Fields: fields}}}, nil
}

// newPoXFixture binds local signing keys, current policy and independent native state.
func newPoXFixture(t *testing.T) (*PoX4Role, *poxNode, stacksworker.Snapshot) {
	t.Helper()
	holderKey := strings.Repeat("0", 63) + "1"
	signerKey := strings.Repeat("0", 63) + "2"
	holder, _ := identity.FromPrivate(holderKey)
	signer, _ := identity.FromPrivate(signerKey)
	role, err := NewPoX4Role(holderKey, holder.Address, signerKey, signer.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	role.Now = func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }
	node := &poxNode{holder: holder.Address, signer: signer.PublicKey, nonce: 7, burn: 210, cycle: 10, first: 11, period: 2, amount: big.NewInt(100000)}
	role.Resolve = func(context.Context, stacksworker.Snapshot) (PoX4Inputs, error) {
		return PoX4Inputs{InitialCohort: true, Node: node, Holder: holder.Address, SignerPublicKey: signer.PublicKey, Amount: big.NewInt(100000), LockCycles: 2, RenewWhenRemainingCycles: 1, TargetCycle: 12, EnrollmentCeiling: 234, Epoch3Height: 252}, nil
	}
	snapshot := stacksworker.Snapshot{Participant: &api.StacksNetworkParticipant{Status: api.ParticipantStatus{Admission: &api.Admission{PolicyDigest: "policy"}}}, Authorize: func(context.Context) error { return nil }}
	return role, node, snapshot
}

func TestPoX4LegacyPostconditionSettlesWithoutInclusionAttribution(t *testing.T) {
	role, node, snapshot := newPoXFixture(t)
	ctx := context.Background()
	first, _ := role.Step(ctx, snapshot)
	if first.Pending != 1 || first.Reason != "Accepted" || len(node.sent) != 1 || !transaction.Valid(node.sent[0]) {
		t.Fatalf("enrollment not sent once: %+v", first)
	}
	for range 2 {
		_, _ = role.Step(ctx, snapshot)
	}
	if len(node.sent) != 1 {
		t.Fatal("pending enrollment replayed")
	}
	node.present = true
	node.nonce = 8
	snapshot.Paused = true
	settled, _ := role.Step(ctx, snapshot)
	if settled.Pending != 0 || settled.Reason != "StateObserved" || settled.PoX4 == nil || settled.PoX4.TargetCycle != 12 || settled.Transactions.PostconditionObserved != 1 || settled.Transactions.Included != 0 || settled.Transactions.LastInclusion != nil {
		t.Fatalf("state convergence falsely attributed inclusion: %+v", settled)
	}
	next, _ := role.Step(ctx, snapshot)
	if next.PoX4 == nil || next.PoX4.TargetCycle != 12 || len(node.sent) != 1 {
		t.Fatal("paused observation lost frozen cycle or submitted")
	}
}

func TestPoX4PostconditionRejectsNonceInterferenceAndMismatchedFacts(t *testing.T) {
	for _, mismatch := range []string{"nonce-low", "nonce-high", "signer", "holder", "amount", "payout", "delegated", "tip", "consensus"} {
		t.Run(mismatch, func(t *testing.T) {
			role, node, snapshot := newPoXFixture(t)
			ctx := context.Background()
			_, _ = role.Step(ctx, snapshot)
			node.present = true
			node.nonce = 8
			switch mismatch {
			case "nonce-low":
				node.nonce = 7
			case "nonce-high":
				node.nonce = 9
			case "tip":
				node.changedTip = true
			case "consensus":
				node.changedConsensus = true
			default:
				node.mismatch = mismatch
			}
			state, _ := role.Step(ctx, snapshot)
			if state.Pending != 1 || state.Transactions.PostconditionObserved != 0 || len(node.sent) != 1 {
				t.Fatalf("mismatched state settled/resubmitted: %+v", state)
			}
		})
	}
}

func TestPoX4RenewalUsesSameStreamAndNativeEpoch3Inclusion(t *testing.T) {
	role, node, snapshot := newPoXFixture(t)
	ctx := context.Background()
	_, _ = role.Step(ctx, snapshot)
	node.present = true
	node.nonce = 8
	_, _ = role.Step(ctx, snapshot)
	node.burn = 252
	node.cycle = 12
	renewal, _ := role.Step(ctx, snapshot)
	if renewal.Pending != 1 || len(node.sent) != 2 || role.goal.kind != "PoX4Extension" || role.goal.first != 12 || role.goal.end != 14 {
		t.Fatalf("incorrect renewal: %+v", renewal)
	}
	node.included = true
	node.executionSuccess = true
	node.nonce = 9
	node.first = 12
	node.period = 2
	completed, _ := role.Step(ctx, snapshot)
	if completed.Pending != 0 || completed.Transactions.Included != 1 || completed.Transactions.PostconditionObserved != 1 || completed.PoX4 == nil || completed.PoX4.TargetCycle != 13 {
		t.Fatalf("renewal did not confirm exact inclusion and state: %+v", completed)
	}
}

func TestPoX4HoldAndAuthorizationDoNotSend(t *testing.T) {
	for _, blocked := range []string{"paused", "ceiling", "authorization", "coverage", "existing-lock"} {
		t.Run(blocked, func(t *testing.T) {
			role, node, snapshot := newPoXFixture(t)
			switch blocked {
			case "paused":
				snapshot.Paused = true
			case "ceiling":
				node.burn = 234
				node.cycle = 11
			case "authorization":
				snapshot.Authorize = func(context.Context) error { return errors.New("changed identity") }
			case "coverage":
				node.cycle = 9
				node.burn = 190
			case "existing-lock":
				node.present = true
				node.mismatch = "signer"
			}
			result, _ := role.Step(context.Background(), snapshot)
			if len(node.sent) != 0 || result.Pending != 0 {
				t.Fatalf("held policy submitted: %+v", result)
			}
		})
	}
}

func TestNoncePostconditionPreservesPendingOnInvalidEvidence(t *testing.T) {
	role, node, snapshot := newPoXFixture(t)
	_, _ = role.Step(context.Background(), snapshot)
	id := node.sent[0].TxID
	proof := api.TransactionPostcondition{TxID: id, Kind: "PoX4Enrollment", StateDigest: "sha256:" + strings.Repeat("a", 64), StacksTip: strings.Repeat("b", 64), ObservedAt: metav1.NewTime(role.now())}
	for _, change := range []string{"txid", "nonce", "digest", "tip", "kind"} {
		invalid := proof
		nonce := uint64(8)
		switch change {
		case "txid":
			invalid.TxID = strings.Repeat("c", 64)
		case "nonce":
			nonce = 9
		case "digest":
			invalid.StateDigest = "sha256:" + strings.Repeat("z", 64)
		case "tip":
			invalid.StacksTip = strings.Repeat("Z", 64)
		case "kind":
			invalid.Kind = "Unbounded"
		}
		_, _ = role.stream.SettleObserved(id, nonce, invalid)
		if role.stream.Pending() != 1 || role.stream.Facts().PostconditionObserved != 0 {
			t.Fatal("invalid public proof released pending stream")
		}
	}
}
