package stacksoperation

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
)

func TestNativeRejectionSettlesOnlyAllowlistedAttempts(t *testing.T) {
	for _, reason := range []string{
		"BadNonce",
		"FeeTooLow",
		"ConflictingNonceInMempool",
		"NotEnoughFunds",
		"ServerFailureDatabase",
		"ServerFailureOther",
		"unknown",
	} {
		t.Run(reason, func(t *testing.T) {
			node := nodeFixture()
			tx, _ := buildFixture(node.account.Nonce)
			node.submitErr = rpc.ClassifySubmissionRejection(
				400,
				[]byte(fmt.Sprintf(`{"txid":%q,"error":"transaction rejected","reason":%q}`, tx.TxID, reason)),
				tx.TxID,
			)
			stream := NonceStream{Address: "sender"}
			ctx := context.Background()
			now := time.Unix(500, 0)
			got, err := stream.Offer(ctx, fixedClock(now), node, big.NewInt(1), permit, buildFixture)
			if err != nil || node.sends != 1 || stream.next != 7 || stream.Facts().LastTxID != tx.TxID {
				t.Fatalf("offer: %s %v %+v", got, err, stream.Facts())
			}
			definite := rpc.ValidationRejectionReason(reason)
			if definite {
				if got != "Rejected"+reason || stream.Pending() != 0 || stream.Facts().Rejected != 1 ||
					stream.Facts().Uncertain != 0 {
					t.Fatalf("refusal: %s %+v", got, stream.Facts())
				}
				for range 3 {
					_, _ = stream.Offer(
						ctx,
						fixedClock(now.Add(4*time.Second)),
						node,
						big.NewInt(1),
						permit,
						buildFixture,
					)
					_, _ = stream.Observe(ctx, now)
				}
				if node.sends != 1 || node.reads != 0 {
					t.Fatal("cooldown sent or polled settled refusal")
				}
				node.account.Nonce++
				got, _ = stream.Offer(
					ctx,
					fixedClock(now.Add(5*time.Second)),
					node,
					big.NewInt(1),
					permit,
					buildFixture,
				)
				if got != "NonceMismatch" || node.sends != 1 {
					t.Fatal("shared writer silently resynchronized nonce")
				}
				node.account.Nonce--
				node.submitErr = nil
				denied := func(context.Context) error { return fmt.Errorf("paused") }
				_, _ = stream.Offer(ctx, fixedClock(now.Add(5*time.Second)), node, big.NewInt(1), denied, buildFixture)
				if node.sends != 1 {
					t.Fatal("new attempt skipped authorization")
				}
				got, _ = stream.Offer(
					ctx,
					fixedClock(now.Add(5*time.Second)),
					node,
					big.NewInt(1),
					permit,
					buildFixture,
				)
				if got != "Accepted" || node.sends != 2 || stream.Pending() != 1 || stream.Facts().Rejected != 1 {
					t.Fatal("new authorized attempt missing or rejection evidence lost")
				}
			} else {
				if got != "SubmissionUncertain" || stream.Pending() != 1 || stream.Facts().Rejected != 0 ||
					stream.Facts().Uncertain != 1 {
					t.Fatalf("server error freed pending: %+v", stream.Facts())
				}
				_, _ = stream.Offer(ctx, fixedClock(now.Add(time.Hour)), node, big.NewInt(1), permit, buildFixture)
				if node.sends != 1 {
					t.Fatal("uncertain attempt replayed")
				}
			}
			node.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: tx.TxID}
			_, _ = stream.Observe(ctx, now.Add(time.Hour))
			if stream.Pending() != 0 || stream.next != 8 || stream.Facts().Included != 1 {
				t.Fatal("exact inclusion was not accounted")
			}
		})
	}
}

// fixedClock supplies a stable time for nonce admission tests.
func fixedClock(now time.Time) func() time.Time { return func() time.Time { return now } }

func TestRejectionBackoffStartsWhenDelayedSubmissionReturns(t *testing.T) {
	node := nodeFixture()
	tx, _ := buildFixture(node.account.Nonce)
	node.submitErr = rpc.ClassifySubmissionRejection(
		400,
		[]byte(fmt.Sprintf(`{"txid":%q,"error":"transaction rejected","reason":"FeeTooLow"}`, tx.TxID)),
		tx.TxID,
	)
	now := time.Unix(500, 0)
	node.onSubmit = func() { now = now.Add(10 * time.Second) }
	stream := NonceStream{Address: "sender"}
	clock := func() time.Time { return now }
	reason, err := stream.Offer(context.Background(), clock, node, big.NewInt(1), permit, buildFixture)
	if err != nil || reason != "RejectedFeeTooLow" || !stream.retryAt.Equal(now.Add(5*time.Second)) {
		t.Fatalf("reason=%s error=%v retryAt=%v now=%v", reason, err, stream.retryAt, now)
	}
	node.onSubmit = nil
	node.submitErr = nil
	now = now.Add(4 * time.Second)
	reason, _ = stream.Offer(context.Background(), clock, node, big.NewInt(1), permit, buildFixture)
	if reason != "RejectionBackoff" || node.sends != 1 {
		t.Fatal("slow refusal consumed the backoff")
	}
	now = now.Add(time.Second)
	reason, _ = stream.Offer(context.Background(), clock, node, big.NewInt(1), permit, buildFixture)
	if reason != "Accepted" || node.sends != 2 {
		t.Fatal("new authorized attempt did not resume after full backoff")
	}
}
