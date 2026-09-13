package stacksoperation

import (
	"context"
	"errors"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

// memoryNode supplies protocol facts while counting mutation attempts.
type memoryNode struct {
	account              rpc.Account
	inclusion            rpc.Inclusion
	submitErr, errorRead error
	sends, reads         int
	onSubmit             func()
}

func (n *memoryNode) Account(context.Context, string) (rpc.Account, error) {
	return n.account, n.errorRead
}

func (n *memoryNode) Submit(context.Context, transaction.Transaction) error {
	n.sends++
	if n.onSubmit != nil {
		n.onSubmit()
	}
	return n.submitErr
}

func (n *memoryNode) Inclusion(context.Context, string) (rpc.Inclusion, error) {
	n.reads++
	return n.inclusion, n.errorRead
}

// nodeFixture includes locked funds separately: native balance is already unlocked.
func nodeFixture() *memoryNode {
	return &memoryNode{account: rpc.Account{Nonce: 7, Balance: clarity.Uint(100), Locked: clarity.Uint(1000)}}
}

func buildFixture(n uint64) (transaction.Transaction, error) {
	// Synthetic transactions use the low nonce byte, including the MaxUint64 boundary case.
	b := []byte{byte(n & 0xff), 1, 2}
	return transaction.Transaction{Bytes: b, TxID: transaction.ID(b)}, nil
}
func permit(context.Context) error { return nil }

func TestUncertainSendNeverReplaysAndPinsObservationTarget(t *testing.T) {
	ctx := context.Background()
	node := nodeFixture()
	node.submitErr = errors.New("connection lost")
	s := NonceStream{Address: "sender"}
	reason, err := s.Offer(ctx, time.Now, node, big.NewInt(100), permit, buildFixture)
	if err != nil || reason != "SubmissionUncertain" || node.sends != 1 || s.Pending() != 1 {
		t.Fatalf("%s %v %+v", reason, err, s)
	}
	peer := nodeFixture()
	for i := 0; i < 3; i++ {
		_, _ = s.Offer(ctx, time.Now, peer, big.NewInt(1), permit, buildFixture)
		_, _ = s.Observe(ctx, time.Now())
	}
	if peer.sends != 0 || peer.reads != 0 || node.sends != 1 || node.reads != 3 {
		t.Fatal("pending transaction replayed or retargeted")
	}
	node.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: transaction.ID([]byte("block"))}
	now := time.Unix(1234, 0)
	if _, err := s.Observe(ctx, now); err != nil {
		t.Fatal(err)
	}
	facts := s.Facts()
	if facts.Offered != 1 || facts.Accepted != 0 || facts.Uncertain != 1 || facts.Included != 1 || s.next != 8 ||
		s.Pending() != 0 ||
		!facts.LastInclusion.ObservedAt.Time.Equal(now) {
		t.Fatalf("lost receipt facts: %+v", facts)
	}
	facts.LastInclusion.BlockID = "tampered"
	if s.Facts().LastInclusion.BlockID == "tampered" {
		t.Fatal("facts alias internal state")
	}
	// A lagging or independently advanced account holds the next send.
	for _, nonce := range []uint64{7, 9} {
		peer.account.Nonce = nonce
		reason, err = s.Offer(ctx, time.Now, peer, big.NewInt(1), permit, buildFixture)
		if err != nil || reason != "NonceMismatch" || peer.sends != 0 {
			t.Fatal("nonce mismatch authorized")
		}
	}
	peer.account.Nonce = 8
	if _, err = s.Offer(ctx, time.Now, peer, big.NewInt(1), permit, buildFixture); err != nil || peer.sends != 1 {
		t.Fatal("matching nonce did not resume")
	}
}

func TestUnsentFailuresDoNotReserveNonceOrSend(t *testing.T) {
	for _, kind := range []string{"funds", "read", "build", "authorization"} {
		t.Run(kind, func(t *testing.T) {
			node := nodeFixture()
			s := NonceStream{Address: "sender"}
			amount := big.NewInt(1)
			build := buildFixture
			authorize := permit
			switch kind {
			case "funds":
				amount = big.NewInt(101)
			case "read":
				node.errorRead = errors.New("read")
			case "build":
				build = func(uint64) (transaction.Transaction, error) {
					return transaction.Transaction{}, errors.New("build")
				}
			case "authorization":
				authorize = func(context.Context) error { return errors.New("changed") }
			}
			_, _ = s.Offer(context.Background(), time.Now, node, amount, authorize, build)
			if s.Pending() != 0 || node.sends != 0 || s.Facts().Offered != 0 {
				t.Fatal("unsent failure reserved or sent")
			}
		})
	}
}

func TestLastNonceAndFailedExecutionAreAccountedWithoutOverflow(t *testing.T) {
	node := nodeFixture()
	node.account.Nonce = math.MaxUint64
	s := NonceStream{Address: "sender"}
	if _, err := s.Offer(context.Background(), time.Now, node, big.NewInt(1), permit, buildFixture); err != nil {
		t.Fatal(err)
	}
	node.inclusion = rpc.Inclusion{Found: true, Success: false, BlockID: transaction.ID([]byte("block"))}
	reason, err := s.Observe(context.Background(), time.Now())
	if err != nil || reason != "ExecutionRejected" || s.Pending() != 0 || s.Facts().Included != 1 ||
		s.Facts().LastInclusion.Success {
		t.Fatal("failed execution was not accounted")
	}
	_, _ = s.Offer(context.Background(), time.Now, node, big.NewInt(1), permit, buildFixture)
	if node.sends != 1 {
		t.Fatal("nonce overflow authorized another send")
	}
}
