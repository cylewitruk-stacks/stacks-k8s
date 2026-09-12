package bitcoincontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// branchRPC models only local canonical ancestry and acknowledged fixed-marker compensation.
type branchRPC struct {
	*fakeRPC
	headers     map[string]action.BitcoinChainPoint
	canonical   map[int64]string
	tip         action.BitcoinChainPoint
	marker      string
	sequence    []string
	headerError error
}

func newBranchRPC(base *fakeRPC) *branchRPC {
	r := &branchRPC{fakeRPC: base, headers: map[string]action.BitcoinChainPoint{}, canonical: map[int64]string{}}
	previous := ""
	for height := int64(0); height <= 234; height++ {
		h := action.BitcoinChainPoint{Hash: fmt.Sprintf("%064x", height+1), Height: height, PreviousBlockHash: previous, Chainwork: fmt.Sprintf("%064x", height+1)}
		r.headers[h.Hash] = h
		r.canonical[height] = h.Hash
		r.tip = h
		previous = h.Hash
	}
	return r
}

func (r *branchRPC) Call(ctx context.Context, endpoint, id, method string, args []any, out any) error {
	var value any
	switch method {
	case "getblockchaininfo":
		value = map[string]any{"chain": "regtest", "blocks": r.tip.Height, "bestblockhash": r.tip.Hash}
	case "getblockheader":
		if r.headerError != nil {
			return r.headerError
		}
		h, ok := r.headers[args[0].(string)]
		if !ok {
			return fmt.Errorf("header missing")
		}
		value = map[string]any{"hash": h.Hash, "height": h.Height, "previousblockhash": h.PreviousBlockHash, "chainwork": h.Chainwork}
	case "getblockhash":
		value = r.canonical[args[0].(int64)]
	case "getchaintips":
		value = []any{map[string]string{"status": "active", "hash": r.tip.Hash}}
	case "invalidateblock":
		r.sequence = append(r.sequence, method)
		r.marker = args[0].(string)
		r.tip = r.headers[r.headers[r.marker].PreviousBlockHash]
		value = nil
	case "reconsiderblock":
		r.sequence = append(r.sequence, method)
		if args[0].(string) != r.marker {
			return fmt.Errorf("foreign marker")
		}
		r.marker = ""
		value = nil
	default:
		return r.fakeRPC.Call(ctx, endpoint, id, method, args, out)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (r *branchRPC) Generate(context.Context, string, string, string) (string, error) {
	r.sequence = append(r.sequence, "Generate")
	height := r.tip.Height + 1
	h := action.BitcoinChainPoint{Hash: fmt.Sprintf("%064x", 10000+height), Height: height, PreviousBlockHash: r.tip.Hash, Chainwork: fmt.Sprintf("%064x", height+1)}
	r.headers[h.Hash] = h
	r.canonical[height] = h.Hash
	r.tip = h
	return h.Hash, nil
}

func reorganizationFixture(t *testing.T) (*testFixture, *action.BitcoinReorganization, *branchRPC) {
	t.Helper()
	ctx := context.Background()
	f := baselineFixture(t)
	if err := action.AddToScheme(f.c.Scheme()); err != nil {
		t.Fatal(err)
	}
	r := newBranchRPC(f.rpc)
	f.worker.RPC = r
	f.worker.Input.ReorganizationEnabled = true
	request := &action.BitcoinReorganization{ObjectMeta: metav1.ObjectMeta{Name: "suffix", Namespace: "test", UID: "suffix-uid", CreationTimestamp: metav1.NewTime(f.now), Finalizers: []string{action.CleanupFinalizer}}, Spec: action.BitcoinReorganizationSpec{NetworkUID: f.root.UID, BitcoinNodeRef: action.LocalReference{Name: "bitcoin"}, Depth: 2, Address: testAddress, Timeout: metav1.Duration{Duration: time.Minute}, BoundaryPolicy: action.ReorganizationBoundaryPolicy{AllowEpochBoundaryCrossing: true, AllowPreparePhaseBoundaryCrossing: true, AllowRewardCycleBoundaryCrossing: true}}}
	if err := f.c.Create(ctx, request); err != nil {
		t.Fatal(err)
	}
	a, err := f.worker.authorize(ctx, f.readRecord(t))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := f.worker.selectAction(ctx, a, f.readRecord(t))
	if err != nil || !selected {
		t.Fatalf("select reorganization %v %v", selected, err)
	}
	record := f.readRecord(t)
	state := record.Status.Action
	execution := binding("BitcoinExecution", record)
	request.Status.AdmittedAt = state.AdmittedAt.DeepCopy()
	request.Status.AdmittedExecution = &execution
	request.Status.AdmittedNetwork = &state.Network
	request.Status.AdmittedTarget = &state.Target
	request.Status.Phase = "Admitted"
	if err := f.c.Update(ctx, request); err != nil {
		t.Fatal(err)
	}
	return f, request, r
}

func TestFiniteReorganizationUsesBoundedReplacementAndCapturedCleanup(t *testing.T) {
	f, _, rpc := reorganizationFixture(t)
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		if err := f.worker.Step(ctx); err != nil {
			t.Fatal(err)
		}
		f.worker.workers.Wait()
		f.now = f.now.Add(time.Second)
		if f.readRecord(t).Status.Action.FinalChain != nil {
			break
		}
	}
	state := f.readRecord(t).Status.Action
	if state.FinalChain == nil || !state.CleanupAcknowledged || !state.InvalidationAcknowledged || len(state.ReplacementBlockHashes) != 3 || rpc.marker != "" || state.FinalChain.Height != 235 {
		t.Fatalf("suffix incomplete %+v sequence %v", state, rpc.sequence)
	}
	expected := []string{"invalidateblock", "Generate", "Generate", "Generate", "reconsiderblock"}
	if fmt.Sprint(rpc.sequence) != fmt.Sprint(expected) {
		t.Fatalf("unexpected mechanism %v", rpc.sequence)
	}
	if state.FinalChain.Hash == state.OriginalChain.Hash || state.FinalChain.Chainwork <= state.OriginalChain.Chainwork {
		t.Fatal("old best chain restored")
	}
}

func TestFiniteReorganizationPauseCompensatesWithoutGeneration(t *testing.T) {
	f, _, rpc := reorganizationFixture(t)
	ctx := context.Background()
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	f.worker.workers.Wait()
	if !f.readRecord(t).Status.Action.InvalidationAcknowledged {
		t.Fatal("invalidation receipt absent")
	}
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.root), f.root); err != nil {
		t.Fatal(err)
	}
	f.root.Spec.Operation = "Paused"
	if err := f.c.Update(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := f.worker.Step(ctx); err != nil {
			t.Fatal(err)
		}
		f.worker.workers.Wait()
	}
	state := f.readRecord(t).Status.Action
	if !state.CleanupAcknowledged || state.BlocksGenerated != 0 || state.StopReason != "NetworkPaused" || fmt.Sprint(rpc.sequence) != "[invalidateblock reconsiderblock]" {
		t.Fatalf("pause compensation %+v %v", state, rpc.sequence)
	}
}

func TestFiniteReceiptRejectsDifferentReservation(t *testing.T) {
	f, _ := finiteFixture(t)
	record := f.readRecord(t)
	request := record.Status.Action.Request.DeepCopy()
	request.UID = "foreign"
	err := accountActionReceipt(record, &bitcoin.BitcoinRPCReceipt{Request: bitcoin.BitcoinArmedRPC{Action: request, Method: "Generate"}, BlockHash: fmt.Sprintf("%064x", 1)})
	if err == nil || record.Status.Action.BlocksGenerated != 0 {
		t.Fatal("foreign receipt accepted")
	}
}

// TestReorganizationExternalMovementStopsWithoutSend preserves exclusion until lifecycle acknowledgement.
func TestReorganizationExternalMovementStopsWithoutSend(t *testing.T) {
	for _, invalidated := range []bool{false, true} {
		t.Run(fmt.Sprint(invalidated), func(t *testing.T) {
			f, _, rpc := reorganizationFixture(t)
			ctx := context.Background()
			if invalidated {
				if err := f.worker.Step(ctx); err != nil {
					t.Fatal(err)
				}
				f.worker.workers.Wait()
			}
			rpc.tip = rpc.headers[rpc.tip.PreviousBlockHash]
			if err := f.worker.Step(ctx); err != nil {
				t.Fatal(err)
			}
			f.worker.workers.Wait()
			record := f.readRecord(t)
			state := record.Status.Action
			if state.StopReason != "ExternalChainMovement" || state.EffectUncertain || record.Status.Armed != nil || record.Status.Reservation == nil || state.BlocksGenerated != 0 {
				t.Fatalf("incorrect divergence outcome: %+v", record.Status)
			}
			if err := f.worker.Step(ctx); err != nil {
				t.Fatal(err)
			}
			f.worker.workers.Wait()
			want := "[]"
			if invalidated {
				want = "[invalidateblock reconsiderblock]"
			}
			if fmt.Sprint(rpc.sequence) != want || f.readRecord(t).Status.Reservation == nil {
				t.Fatalf("unexpected sends or premature release: %v", rpc.sequence)
			}
		})
	}
}

// TestReorganizationReadFailureDoesNotProveMovement keeps a failed preflight retryable.
func TestReorganizationReadFailureDoesNotProveMovement(t *testing.T) {
	f, _, rpc := reorganizationFixture(t)
	rpc.headerError = fmt.Errorf("transient read failure")
	if err := f.worker.Step(context.Background()); err == nil {
		t.Fatal("read failure lost")
	}
	record := f.readRecord(t)
	if record.Status.Action.StopReason != "" || record.Status.Armed != nil || len(rpc.sequence) != 0 {
		t.Fatalf("read failure became an outcome: %+v", record.Status)
	}
	rpc.headerError = nil
	if err := f.worker.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.worker.workers.Wait()
	if fmt.Sprint(rpc.sequence) != "[invalidateblock]" {
		t.Fatalf("recovered read did not progress: %v", rpc.sequence)
	}
}

// TestPostArmDivergenceRemainsStopped proves the second preflight cannot forget confirmed movement.
func TestPostArmDivergenceRemainsStopped(t *testing.T) {
	for _, invalidated := range []bool{false, true} {
		t.Run(fmt.Sprint(invalidated), func(t *testing.T) {
			f, _, rpc := reorganizationFixture(t)
			ctx := context.Background()
			if invalidated {
				if err := f.worker.Step(ctx); err != nil {
					t.Fatal(err)
				}
				f.worker.workers.Wait()
			}
			original := rpc.tip
			changed := false
			f.worker.Client = interceptor.NewClient(f.c.(client.WithWatch), interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, c client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					if err := c.SubResource(sub).Update(ctx, obj, opts...); err != nil {
						return err
					}
					record, ok := obj.(*bitcoin.BitcoinExecution)
					if ok && sub == "status" && record.Status.Armed != nil && !changed {
						changed = true
						rpc.tip = rpc.headers[original.PreviousBlockHash]
					}
					return nil
				},
			})
			if err := f.worker.Step(ctx); err != nil {
				t.Fatal(err)
			}
			f.worker.workers.Wait()
			record := f.readRecord(t)
			if !changed || record.Status.Armed != nil || record.Status.Action.StopReason != "ExternalChainMovement" || record.Status.Reservation == nil || record.Status.Action.EffectUncertain {
				t.Fatalf("post-CAS movement lost: %+v", record.Status)
			}
			rpc.tip = original
			for range 3 {
				if err := f.worker.Step(ctx); err != nil {
					t.Fatal(err)
				}
				f.worker.workers.Wait()
			}
			want := "[]"
			if invalidated {
				want = "[invalidateblock reconsiderblock]"
			}
			record = f.readRecord(t)
			if fmt.Sprint(rpc.sequence) != want || record.Status.Reservation == nil || record.Status.Action.BlocksGenerated != 0 || record.Status.Action.StopReason != "ExternalChainMovement" {
				t.Fatalf("returned tip resumed stopped action: %+v, calls %v", record.Status, rpc.sequence)
			}
		})
	}
}
