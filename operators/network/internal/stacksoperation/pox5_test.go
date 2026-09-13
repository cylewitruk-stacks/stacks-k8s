package stacksoperation

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
)

// pox5Node changes canonical state independently from send acknowledgements.
type pox5Node struct {
	*poxNode
	admin                                             string
	adminNonce                                        uint64
	source                                            string
	sourceErr                                         error
	sourceFound, registered, granted, prepare, legacy bool
}

func (n *pox5Node) PoX(ctx context.Context) (rpc.PoX, error) {
	p, e := n.poxNode.PoX(ctx)
	if !n.legacy {
		p.Contract = PoX5Contract
	}
	return p, e
}

func (n *pox5Node) PoXAt(ctx context.Context, tip string) (rpc.PoX, error) {
	if tip != strings.Repeat("d", 64) {
		return rpc.PoX{}, errors.New("unbound PoX query")
	}
	return n.PoX(ctx)
}

func (n *pox5Node) Account(ctx context.Context, address string) (rpc.Account, error) {
	account, e := n.poxNode.Account(ctx, address)
	if address == n.admin && n.admin != n.holder {
		account.Nonce = n.adminNonce
		account.Locked = clarity.Uint(0)
	}
	return account, e
}

func (n *pox5Node) AccountAt(ctx context.Context, address, tip string) (rpc.Account, error) {
	if tip != strings.Repeat("d", 64) {
		return rpc.Account{}, errors.New("unbound account query")
	}
	return n.Account(ctx, address)
}

func (n *pox5Node) SourceAt(_ context.Context, address, contract, tip string) (string, bool, error) {
	if address != n.admin || contract != "direct-signer" || tip != strings.Repeat("d", 64) {
		return "", false, errors.New("unbound source query")
	}
	return n.source, n.sourceFound, n.sourceErr
}

func (n *pox5Node) ReadOnlyAt(
	ctx context.Context,
	tip, sender, address, contract, method string,
	args []clarity.Value,
) (clarity.Value, error) {
	if tip != strings.Repeat("d", 64) || address != pox4Address {
		return clarity.Value{}, errors.New("unbound contract query")
	}
	if contract == "pox-4" {
		return n.poxNode.ReadOnlyAt(ctx, tip, sender, address, contract, method, args)
	}
	manager, _ := clarity.Principal(n.admin + ".direct-signer")
	optional := func(value clarity.Value) clarity.Value {
		return clarity.Value{Type: clarity.Some, Items: []clarity.Value{value}}
	}
	tuple := func(fields map[string]clarity.Value) clarity.Value {
		return optional(clarity.Value{Type: clarity.Tuple, Fields: fields})
	}
	amount, _ := clarity.Uint128(n.amount.String())
	switch method {
	case "is-in-prepare-phase":
		if n.prepare {
			return clarity.Value{Type: clarity.True}, nil
		}
		return clarity.Value{Type: clarity.False}, nil
	case "get-signer-info":
		if !n.registered {
			return clarity.Value{Type: clarity.None}, nil
		}
		key, _ := hex.DecodeString(n.signer)
		if n.mismatch == "key" {
			key[2] ^= 1
		}
		return optional(clarity.Value{Type: clarity.Buffer, Bytes: key}), nil
	case "verify-signer-key-grant":
		if n.granted {
			return clarity.Value{Type: clarity.ResponseOK, Items: []clarity.Value{{Type: clarity.True}}}, nil
		}
		return clarity.Value{Type: clarity.ResponseErr, Items: []clarity.Value{clarity.Uint(1)}}, nil
	case "get-staker-info":
		if !n.present {
			return clarity.Value{Type: clarity.None}, nil
		}
		if n.mismatch == "manager" {
			manager, _ = clarity.Principal(n.holder + ".other")
		}
		return tuple(
			map[string]clarity.Value{
				"amount-ustx":        amount,
				"first-reward-cycle": clarity.Uint(n.first),
				"num-cycles":         clarity.Uint(n.period),
				"signer":             manager,
			},
		), nil
	case "get-signer-cycle-membership":
		if n.mismatch == "member" {
			amount = clarity.Uint(1)
		}
		return tuple(map[string]clarity.Value{"amount-ustx": amount, "signer": manager}), nil
	case "get-amount-delegated-for-signer":
		if n.mismatch == "delegated" {
			return clarity.Uint(1), nil
		}
		return amount, nil
	case "signer-set-contains-for-cycle":
		if n.mismatch == "set" {
			return clarity.Value{Type: clarity.False}, nil
		}
		return clarity.Value{Type: clarity.True}, nil
	case "reward-cycle-to-burn-height":
		cycle, _ := unsigned(args[0])
		if n.mismatch == "unlock" {
			cycle++
		}
		return clarity.Uint(cycle * 20), nil
	}
	return clarity.Value{}, errors.New("unexpected native PoX5 method")
}

func newPoX5Fixture(t *testing.T, shared bool) (*StackerRole, *pox5Node, stacksworker.Snapshot) {
	t.Helper()
	legacy, node, snapshot := newPoXFixture(t)
	adminKey := strings.Repeat("0", 63) + "3"
	if shared {
		adminKey = strings.Repeat("0", 63) + "1"
	}
	admin, _ := identity.FromPrivate(adminKey)
	role, e := NewStackerRole(
		strings.Repeat("0", 63)+"1",
		node.holder,
		strings.Repeat("0", 63)+"2",
		node.signer,
		adminKey,
		admin.Address,
	)
	if e != nil {
		t.Fatal(e)
	}
	role.Now = legacy.Now
	n := &pox5Node{poxNode: node, admin: admin.Address, adminNonce: 20}
	n.burn, n.cycle, n.first, n.period = 284, 14, 15, 6
	role.ResolvePoX4 = func(ctx context.Context, s stacksworker.Snapshot) (PoX4Inputs, error) {
		in, e := legacy.Resolve(ctx, s)
		in.Node = n
		return in, e
	}
	role.ResolvePoX5 = func(context.Context, stacksworker.Snapshot) (PoX5Inputs, error) {
		return PoX5Inputs{
			InitialCohort:            true,
			Node:                     n,
			Holder:                   n.holder,
			Administrator:            n.admin,
			SignerPublicKey:          n.signer,
			Amount:                   big.NewInt(100000),
			LockCycles:               6,
			RenewWhenRemainingCycles: 3,
			TargetCycle:              15,
			Epoch4Height:             282,
		}, nil
	}
	return role, n, snapshot
}

// readyManager models preexisting exact source and active native registration.
func (n *pox5Node) readyManager() {
	n.source, _ = protocolcontracts.DirectManager(n.holder)
	n.sourceFound, n.registered, n.granted = true, true, true
}

func TestPoX5ManagerEnrollmentSequenceUsesIndependentNonceStreams(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "separate", true: "shared"}[shared], func(t *testing.T) {
			r, n, s := newPoX5Fixture(t, shared)
			ctx := context.Background()
			deployed, _ := r.Step(ctx, s)
			if deployed.Pending != 1 || len(n.sent) != 1 || !bytes.Contains(n.sent[0].Bytes, []byte("as-contract?")) ||
				!bytes.Contains(n.sent[0].Bytes, []byte(n.holder)) {
				t.Fatalf("manager deployment missing: %+v", deployed)
			}
			n.source, _ = protocolcontracts.DirectManager(n.holder)
			n.sourceFound = true
			if shared {
				n.nonce++
			} else {
				n.adminNonce++
			}
			observed, _ := r.Step(ctx, s)
			if observed.Pending != 0 || observed.Reason != "StateObserved" {
				t.Fatalf("source did not settle: %+v", observed)
			}
			registered, _ := r.Step(ctx, s)
			if registered.Pending != 1 || len(n.sent) != 2 ||
				!bytes.Contains(n.sent[1].Bytes, []byte("register-self")) {
				t.Fatalf("registration missing: %+v", registered)
			}
			n.registered, n.granted = true, true
			if shared {
				n.nonce++
			} else {
				n.adminNonce++
			}
			_, _ = r.Step(ctx, s)
			staked, _ := r.Step(ctx, s)
			if staked.Pending != 1 || len(n.sent) != 3 || !bytes.Contains(n.sent[2].Bytes, []byte("stake")) {
				t.Fatalf("stake missing: %+v", staked)
			}
			n.present = true
			n.nonce++
			final, _ := r.Step(ctx, s)
			if final.Pending != 0 || final.PoX5 == nil || final.PoX5.TargetCycle != 15 ||
				final.PoX5.EndCycleExclusive != 21 ||
				final.Transactions.Included != 0 ||
				final.Transactions.LastInclusion != nil {
				t.Fatalf("unattributed enrollment evidence incorrect: %+v", final)
			}
			if shared {
				if final.AdministratorTransactions != nil || final.Transactions.Offered != 3 ||
					final.Transactions.PostconditionObserved != 3 {
					t.Fatal("same account streams duplicated")
				}
			} else if final.Transactions.Offered != 1 ||
				final.AdministratorTransactions.Offered != 2 ||
				final.AdministratorTransactions.PostconditionObserved != 2 {
				t.Fatal("independent account facts mixed")
			}
		})
	}
}

func TestPoX5UnknownSendNeverReplaysAndExactStateRejectsInterference(t *testing.T) {
	for _, change := range []string{
		"unchanged-nonce",
		"higher-nonce",
		"source",
		"key",
		"grant",
		"manager",
		"member",
		"delegated",
		"set",
		"unlock",
		"consensus",
		"amount",
	} {
		t.Run(change, func(t *testing.T) {
			r, n, s := newPoX5Fixture(t, false)
			n.readyManager()
			n.submitErr = errors.New("response lost")
			ctx := context.Background()
			first, _ := r.Step(ctx, s)
			if first.Pending != 1 || first.Transactions.Uncertain != 1 {
				t.Fatalf("unknown send lost: %+v", first)
			}
			n.present = true
			n.nonce++
			switch change {
			case "unchanged-nonce":
				n.nonce--
			case "higher-nonce":
				n.nonce++
			case "source":
				n.source += "\n"
			case "grant":
				n.granted = false
			case "consensus":
				n.changedConsensus = true
			case "amount":
				n.amount = big.NewInt(2)
			default:
				n.mismatch = change
			}
			s.Paused = true
			for range 3 {
				got, _ := r.Step(ctx, s)
				if got.Pending != 1 || got.Transactions.PostconditionObserved != 0 ||
					got.PoX5 != nil && change != "higher-nonce" && change != "unchanged-nonce" {
					t.Fatalf("bad state cleared ambiguity: %+v", got)
				}
			}
			if len(n.sent) != 1 {
				t.Fatal("uncertain operation replayed")
			}
		})
	}
}

func TestPoX5RenewalRequiresNativeWindowAndPreservesAmountPolicy(t *testing.T) {
	r, n, s := newPoX5Fixture(t, false)
	n.readyManager()
	n.present = true
	ctx := context.Background()
	got, _ := r.Step(ctx, s)
	if got.PoX5 == nil || len(n.sent) != 0 {
		t.Fatal("existing enrollment not adopted")
	}
	n.burn, n.cycle = 360, 18
	n.prepare = true
	got, _ = r.Step(ctx, s)
	if got.Reason != "AwaitingMaintenanceWindow" || len(n.sent) != 0 {
		t.Fatal("renewal sent in native prepare phase")
	}
	n.prepare = false
	n.amount = big.NewInt(99999)
	got, _ = r.Step(ctx, s)
	if got.Reason != "AwaitingUnlockForAmountChange" || len(n.sent) != 0 {
		t.Fatal("obsolete amount renewed")
	}
	n.amount = big.NewInt(100000)
	got, _ = r.Step(ctx, s)
	if got.Pending != 1 || len(n.sent) != 1 || !bytes.Contains(n.sent[0].Bytes, []byte("stake-update")) {
		t.Fatalf("renewal missing: %+v", got)
	}
	n.period = 10
	n.nonce++
	n.included, n.executionSuccess = true, true
	got, _ = r.Step(ctx, s)
	if got.Pending != 0 || got.Transactions.Included != 1 || got.Transactions.PostconditionObserved != 0 ||
		got.PoX5.EndCycleExclusive != 25 {
		t.Fatalf("renewal evidence incorrect: %+v", got)
	}
}

func TestPoX5NoSendsWithoutActivationAuthorizationOrCompatibility(t *testing.T) {
	for _, change := range []string{
		"early",
		"paused",
		"source",
		"source-unavailable",
		"key",
		"authorization",
		"missed-cycle",
	} {
		t.Run(change, func(t *testing.T) {
			r, n, s := newPoX5Fixture(t, false)
			n.readyManager()
			switch change {
			case "source-unavailable":
				n.sourceErr = errors.New("source read unavailable")
			case "early":
				n.burn = 281
			case "paused":
				s.Paused = true
			case "source":
				n.source += "\n"
			case "key":
				n.mismatch = "key"
			case "authorization":
				s.Authorize = func(context.Context) error { return errors.New("policy changed") }
			case "missed-cycle":
				n.cycle, n.burn = 15, 300
			}
			got, _ := r.Step(context.Background(), s)
			if len(n.sent) != 0 || got.Pending != 0 {
				t.Fatalf("unauthorized send: %+v", got)
			}
		})
	}
}

func TestStackerTransitionRetainsUnknownPoX4AndSharesHolderNonce(t *testing.T) {
	r, n, s := newPoX5Fixture(t, true)
	n.legacy = true
	n.burn, n.cycle, n.first, n.period = 210, 10, 11, 2
	ctx := context.Background()
	first, _ := r.Step(ctx, s)
	if first.Pending != 1 || len(n.sent) != 1 {
		t.Fatalf("PoX4 first send missing: %+v", first)
	}
	original := r.legacy.stream.pending.transaction.TxID
	n.legacy = false
	n.burn, n.cycle, n.first, n.period = 284, 14, 15, 6
	for range 2 {
		got, _ := r.Step(ctx, s)
		if got.Pending != 1 || len(n.sent) != 1 || r.legacy.stream.pending.transaction.TxID != original {
			t.Fatal("PoX4 unknown state replaced at transition")
		}
	}
	n.nonce++
	n.included, n.executionSuccess = true, true
	settled, _ := r.Step(ctx, s)
	if settled.Pending != 0 || settled.Transactions.Included != 1 || settled.PoX4 != nil {
		t.Fatalf("old exact inclusion could not settle at transition: %+v", settled)
	}
	n.included = false
	next, _ := r.Step(ctx, s)
	if next.Pending != 1 || len(n.sent) != 2 || r.administrator != &r.legacy.stream ||
		r.legacy.stream.pending.nonce != 8 {
		t.Fatalf("transition lost surviving account stream: %+v", next)
	}
}

func TestPoX5ObservationPreservesFullWidthStake(t *testing.T) {
	r, n, s := newPoX5Fixture(t, false)
	n.readyManager()
	n.present = true
	amount := new(big.Int).Lsh(big.NewInt(1), 80)
	n.amount = new(big.Int).Set(amount)
	resolve := r.ResolvePoX5
	r.ResolvePoX5 = func(ctx context.Context, snapshot stacksworker.Snapshot) (PoX5Inputs, error) {
		input, err := resolve(ctx, snapshot)
		input.Amount = new(big.Int).Set(amount)
		return input, err
	}
	got, _ := r.Step(context.Background(), s)
	if got.PoX5 == nil || got.PoX5.AmountMicroSTX != amount.String() ||
		got.PoX5.DelegatedAmountMicroSTX != amount.String() ||
		len(n.sent) != 0 {
		t.Fatalf("full-width native stake truncated: %+v", got)
	}
}
