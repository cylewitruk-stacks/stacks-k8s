package accountledger

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// fakeAdmission models current identity and pause independently of the ledger.
type fakeAdmission struct {
	paused bool
	target Target
	reads  int
}

func (a *fakeAdmission) Admit(_ context.Context, _ *stacks.StacksAccount, _ string, mutation bool) (Target, error) {
	a.reads++
	if mutation && a.paused {
		return Target{}, fmt.Errorf("paused")
	}
	return a.target, nil
}

// fakeRPC counts real submission attempts separately from receipt observations.
type fakeRPC struct {
	nonce     int64
	sends     int
	receipt   Inclusion
	submitErr error
}

func (r *fakeRPC) Nonce(context.Context, string, string) (int64, error) { return r.nonce, nil }
func (r *fakeRPC) Inclusion(context.Context, string, string) (Inclusion, error) {
	return r.receipt, nil
}
func (r *fakeRPC) Submit(context.Context, string, Transaction) error {
	r.sends++
	return r.submitErr
}

// fixture supplies an isolated account ledger and a deterministic offline signer.
func fixture(t *testing.T, hooks interceptor.Funcs) (*Ledger, *fakeRPC, *fakeAdmission, client.ObjectKey, Request) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := stacks.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	account := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "holder", Namespace: "test", UID: "account-uid", Finalizers: []string{Finalizer}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&stacks.StacksAccount{}).WithObjects(account).WithInterceptorFuncs(hooks).Build()
	rpc := &fakeRPC{}
	admission := &fakeAdmission{target: Target{Endpoint: "http://127.0.0.1:20443", ActorUID: "actor", PodUID: "pod", ContainerID: "container"}}
	l := &Ledger{Client: c, Reader: c, RPC: rpc, Admission: admission, Now: func() time.Time { return time.Unix(100, 0).UTC() }}
	request := Request{ConsumerUID: "consumer", Ordinal: 1, Digest: "sha256:" + strings.Repeat("a", 64), Sign: func(_ context.Context, nonce int64) (Transaction, error) {
		raw := make([]byte, 100)
		raw[0] = byte(nonce)
		hash := sha512.Sum512_256(raw)
		return Transaction{TxID: hex.EncodeToString(hash[:]), Bytes: hex.EncodeToString(raw)}, nil
	}}
	return l, rpc, admission, client.ObjectKeyFromObject(account), request
}

// readAccount inspects actual durable state after a simulated process boundary.
func readAccount(t *testing.T, l *Ledger, key client.ObjectKey) *stacks.StacksAccount {
	t.Helper()
	a := &stacks.StacksAccount{}
	if err := l.Reader.Get(context.Background(), key, a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRestartObservesWithoutReplayAndRequiresReceiptAcknowledgement(t *testing.T) {
	ctx := context.Background()
	l, rpc, admission, key, request := fixture(t, interceptor.Funcs{})
	if _, err := l.Ensure(ctx, key, request); !errors.Is(err, ErrWaiting) {
		t.Fatal(err)
	}
	armed := readAccount(t, l, key).Status.Transaction.DeepCopy()
	if rpc.sends != 1 || !armed.Accepted {
		t.Fatalf("send/ack: %d %+v", rpc.sends, armed)
	}
	// A replacement controller has no signing/submission state in memory.
	restarted := &Ledger{Client: l.Client, Reader: l.Reader, RPC: rpc, Admission: admission, Now: l.Now}
	request.Sign = func(context.Context, int64) (Transaction, error) {
		t.Fatal("restart signed an outstanding transaction")
		return Transaction{}, nil
	}
	if _, err := restarted.Ensure(ctx, key, request); !errors.Is(err, ErrWaiting) {
		t.Fatal(err)
	}
	admission.paused = true
	rpc.receipt = Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	done, err := restarted.Ensure(ctx, key, request)
	if err != nil {
		t.Fatal(err)
	}
	if done.Receipt == nil || done.TxID != armed.TxID || done.Acknowledged || rpc.sends != 1 {
		t.Fatalf("receipt changed authorization: %+v sends=%d", done, rpc.sends)
	}
	newer := request
	newer.Ordinal++
	if _, err := restarted.Ensure(ctx, key, newer); !errors.Is(err, ErrWaiting) {
		t.Fatalf("unacknowledged account released: %v", err)
	}
	if err := restarted.Acknowledge(ctx, key, request.ConsumerUID, done); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Ensure(ctx, key, newer); err == nil {
		t.Fatal("pause allowed new authorization")
	}
	account := readAccount(t, l, key)
	if account.Status.NextNonce == nil || *account.Status.NextNonce != 1 || !account.Status.Transaction.Acknowledged {
		t.Fatalf("wrong accounting: %+v", account.Status)
	}
}

func TestLostArmAcknowledgementNeverSubmitsOrReconstructsAuthority(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			failed := false
			hooks := interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				if sub == "status" && !failed {
					failed = true
					if committed {
						if err := c.Status().Patch(ctx, obj, patch, opts...); err != nil {
							return err
						}
					}
					return fmt.Errorf("lost acknowledgement")
				}
				return c.SubResource(sub).Patch(ctx, obj, patch, opts...)
			}}
			l, rpc, _, key, request := fixture(t, hooks)
			if _, err := l.Ensure(context.Background(), key, request); err == nil {
				t.Fatal("write failure hidden")
			}
			if rpc.sends != 0 {
				t.Fatal("send occurred without acknowledged authorization")
			}
			_, err := l.Ensure(context.Background(), key, request)
			if !errors.Is(err, ErrWaiting) {
				t.Fatal(err)
			}
			want := 1
			if committed {
				want = 0
			}
			if rpc.sends != want {
				t.Fatalf("sends=%d want=%d", rpc.sends, want)
			}
		})
	}
}

func TestReceiptAccountingLossPreservesTxIDAndNonce(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			failed := false
			hooks := interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				a := obj.(*stacks.StacksAccount)
				if sub == "status" && a.Status.Transaction != nil && a.Status.Transaction.Receipt != nil && !failed {
					failed = true
					if committed {
						if err := c.Status().Patch(ctx, obj, patch, opts...); err != nil {
							return err
						}
					}
					return fmt.Errorf("lost receipt write acknowledgement")
				}
				return c.SubResource(sub).Patch(ctx, obj, patch, opts...)
			}}
			l, rpc, _, key, request := fixture(t, hooks)
			_, _ = l.Ensure(context.Background(), key, request)
			id := readAccount(t, l, key).Status.Transaction.TxID
			rpc.receipt = Inclusion{Found: true, Success: false, BlockID: strings.Repeat("c", 64)}
			if _, err := l.Ensure(context.Background(), key, request); err == nil {
				t.Fatal("receipt write failure hidden")
			}
			done, err := l.Ensure(context.Background(), key, request)
			if err != nil {
				t.Fatal(err)
			}
			if done.TxID != id || done.Receipt == nil || done.Receipt.Success || done.Nonce != 0 || rpc.sends != 1 {
				t.Fatalf("receipt lost identity: %+v sends=%d", done, rpc.sends)
			}
			if *readAccount(t, l, key).Status.NextNonce != 1 {
				t.Fatal("unsuccessful execution did not account consumed nonce")
			}
		})
	}
}

func TestStaleConsumerOrdinalAndIntentCannotResubmit(t *testing.T) {
	l, rpc, _, key, request := fixture(t, interceptor.Funcs{})
	ctx := context.Background()
	_, _ = l.Ensure(ctx, key, request)
	rpc.receipt = Inclusion{Found: true, Success: true, BlockID: strings.Repeat("d", 64)}
	done, err := l.Ensure(ctx, key, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Acknowledge(ctx, key, request.ConsumerUID, done); err != nil {
		t.Fatal(err)
	}
	newer := request
	newer.Ordinal++
	rpc.nonce = 1
	rpc.receipt = Inclusion{}
	if _, err := l.Ensure(ctx, key, newer); !errors.Is(err, ErrWaiting) {
		t.Fatal(err)
	}
	if _, err := l.Ensure(ctx, key, request); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("stale ordinal admitted: %v", err)
	}
	for _, changed := range []Request{
		{ConsumerUID: "replacement", Ordinal: 2, Digest: request.Digest, Sign: request.Sign},
		{ConsumerUID: request.ConsumerUID, Ordinal: 2, Digest: "sha256:" + strings.Repeat("f", 64), Sign: request.Sign},
	} {
		if _, err := l.Ensure(ctx, key, changed); err == nil {
			t.Fatal("changed identity admitted")
		}
	}
	if err := l.Acknowledge(ctx, key, request.ConsumerUID, done); err == nil {
		t.Fatal("stale receipt acknowledged newer operation")
	}
	if rpc.sends != 2 {
		t.Fatalf("duplicate sends: %d", rpc.sends)
	}
}

func TestIdentityChangeDuringSigningAndMalformedSignerNeverSend(t *testing.T) {
	for _, change := range []string{"identity", "pause", "bytes"} {
		t.Run(change, func(t *testing.T) {
			l, rpc, admission, key, request := fixture(t, interceptor.Funcs{})
			sign := request.Sign
			request.Sign = func(ctx context.Context, nonce int64) (Transaction, error) {
				tx, err := sign(ctx, nonce)
				switch change {
				case "identity":
					admission.target.PodUID = "replacement"
				case "pause":
					admission.paused = true
				case "bytes":
					tx.Bytes = "00"
				}
				return tx, err
			}
			if _, err := l.Ensure(context.Background(), key, request); err == nil {
				t.Fatal("invalid authority accepted")
			}
			if rpc.sends != 0 || readAccount(t, l, key).Status.Transaction != nil {
				t.Fatal("invalid authority armed or sent")
			}
		})
	}
}

// TestConcurrentAccountWorkersAuthorizeOneSend proves that leader ownership is not the transaction fence.
func TestConcurrentAccountWorkersAuthorizeOneSend(t *testing.T) {
	l, firstRPC, _, key, request := fixture(t, interceptor.Funcs{})
	secondRPC := &fakeRPC{}
	other := *l
	other.RPC = secondRPC
	other.Admission = &fakeAdmission{target: Target{Endpoint: "http://127.0.0.1:20443", ActorUID: "actor", PodUID: "pod", ContainerID: "container"}}
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	sign := request.Sign
	request.Sign = func(ctx context.Context, nonce int64) (Transaction, error) {
		arrived <- struct{}{}
		select {
		case <-release:
			return sign(ctx, nonce)
		case <-ctx.Done():
			return Transaction{}, ctx.Err()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 2)
	for _, worker := range []*Ledger{l, &other} {
		go func(worker *Ledger) { _, err := worker.Ensure(ctx, key, request); done <- err }(worker)
	}
	for range 2 {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("workers did not reach concurrent authorization")
		}
	}
	close(release)
	for range 2 {
		<-done
	}
	if firstRPC.sends+secondRPC.sends != 1 {
		t.Fatalf("competing workers sent %d requests", firstRPC.sends+secondRPC.sends)
	}
	current := readAccount(t, l, key)
	if current.Status.Transaction == nil || !current.Status.Transaction.Accepted || current.Status.Transaction.Nonce != 0 {
		t.Fatalf("winning authorization was not retained: %+v", current.Status)
	}
}
