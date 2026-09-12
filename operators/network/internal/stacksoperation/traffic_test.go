package stacksoperation

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
)

// trafficNode adds chain identity to the counting native test node.
type trafficNode struct {
	*memoryNode
	info       rpc.Info
	viewErr    error
	movingView bool
	viewReads  uint64
}

func (n *trafficNode) ChainView(context.Context) (rpc.ChainView, error) {
	n.viewReads++
	view := rpc.ChainView{Info: n.info, FullySynced: true, IndexBlockID: strings.Repeat("a", 64)}
	if n.movingView {
		view.BurnHeight += n.viewReads
	}
	if n.viewErr != nil {
		return view, n.viewErr
	}
	return view, n.errorRead
}

func (n *trafficNode) Info(context.Context) (rpc.Info, error) { return n.info, n.errorRead }

func TestTrafficGatesPauseCadenceAndPendingPolicyBoundary(t *testing.T) {
	key := strings.Repeat("0", 63) + "1"
	public, err := identity.FromPrivate(key)
	if err != nil {
		t.Fatal(err)
	}
	role, err := NewTransferRole(key, public.Address)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(500, 0)
	role.Now = func() time.Time { return now }
	node := &trafficNode{memoryNode: nodeFixture(), info: rpc.Info{NetworkID: 0x80000000, BurnHeight: 230}}
	p := &api.StacksNetworkParticipant{Status: api.ParticipantStatus{Admission: &api.Admission{PolicyDigest: "policy-one"}}}
	snapshot := stacksworker.Snapshot{Participant: p, Authorize: permit}
	role.Resolve = func(context.Context, stacksworker.Snapshot) (TransferInputs, error) {
		return TransferInputs{Node: node, Recipient: public.Address, Amount: 1, Fee: 1, Interval: 10 * time.Second, StartHeight: 231}, nil
	}
	result, err := role.Step(context.Background(), snapshot)
	if err != nil || result.Reason != "AwaitingEpoch3" || node.sends != 0 {
		t.Fatal("premature traffic")
	}
	node.info.BurnHeight = 231
	result, err = role.Step(context.Background(), snapshot)
	if err != nil || result.Reason != "Accepted" || node.sends != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	snapshot.Paused = true
	p.Status.Admission.PolicyDigest = "policy-two"
	node.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	result, err = role.Step(context.Background(), snapshot)
	if err != nil || result.Transactions.Included != 1 || result.AppliedPolicyDigest != "policy-one" || node.sends != 1 {
		t.Fatal("pending policy or paused receipt lost")
	}
	now = now.Add(time.Hour)
	_, _ = role.Step(context.Background(), snapshot)
	if node.sends != 1 {
		t.Fatal("pause submitted")
	}
	snapshot.Paused = false
	node.account.Nonce = 8
	result, err = role.Step(context.Background(), snapshot)
	if err != nil || result.AppliedPolicyDigest != "policy-two" || node.sends != 2 {
		t.Fatal("resume failed")
	}
	node.account.Nonce = 9
	_, _ = role.Step(context.Background(), snapshot) // Observe exact second inclusion.
	for i := 0; i < 10; i++ {
		_, _ = role.Step(context.Background(), snapshot)
	}
	if node.sends != 2 {
		t.Fatal("missed cadence caused catchup burst")
	}
	now = now.Add(10 * time.Second)
	_, _ = role.Step(context.Background(), snapshot)
	if node.sends != 3 {
		t.Fatal("next current opportunity missing")
	}
}

func TestTransferKeyCannotSilentlyChangeSender(t *testing.T) {
	if _, err := NewTransferRole(strings.Repeat("0", 63)+"1", "ST000000000000000000002AMW42H"); err == nil {
		t.Fatal("wrong sender accepted")
	}
}

func TestRejectedTrafficWaitsCadenceAndDrainsWithoutInclusion(t *testing.T) {
	key := strings.Repeat("0", 63) + "1"
	public, err := identity.FromPrivate(key)
	if err != nil {
		t.Fatal(err)
	}
	role, err := NewTransferRole(key, public.Address)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(500, 0)
	role.Now = func() time.Time { return now }
	node := &trafficNode{memoryNode: nodeFixture(), info: rpc.Info{NetworkID: 0x80000000, BurnHeight: 231}}
	tx, err := transaction.Transfer(transaction.Options{Version: transaction.Testnet, ChainID: 0x80000000, Nonce: node.account.Nonce, Fee: 1, PostConditionMode: transaction.Deny, PrivateKey: role.key}, public.Address, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	node.submitErr = rpc.ClassifySubmissionRejection(400, []byte(fmt.Sprintf(`{"txid":%q,"error":"transaction rejected","reason":"FeeTooLow"}`, tx.TxID)), tx.TxID)
	role.Resolve = func(context.Context, stacksworker.Snapshot) (TransferInputs, error) {
		return TransferInputs{Node: node, Recipient: public.Address, Amount: 1, Fee: 1, Interval: 10 * time.Second, StartHeight: 231}, nil
	}
	snapshot := stacksworker.Snapshot{Participant: &api.StacksNetworkParticipant{Status: api.ParticipantStatus{Admission: &api.Admission{PolicyDigest: "policy"}}}, Authorize: permit}
	for range 10 {
		result, err := role.Step(context.Background(), snapshot)
		if err != nil || !result.Blocked || result.Reason != "RejectedFeeTooLow" || result.Pending != 0 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	now = now.Add(9 * time.Second)
	_, _ = role.Step(context.Background(), snapshot)
	if node.sends != 1 {
		t.Fatal("rejection bypassed cadence")
	}
	drained, err := role.Drain(context.Background(), snapshot)
	if err != nil || !drained.Done || !drained.Settled || drained.Transactions.LastTxID != tx.TxID {
		t.Fatalf("drain=%+v err=%v", drained, err)
	}
	now = now.Add(time.Second)
	node.submitErr = fmt.Errorf("response lost")
	result, err := role.Step(context.Background(), snapshot)
	if err != nil || result.Reason != "SubmissionUncertain" || node.sends != 2 || result.Transactions.Rejected != 1 {
		t.Fatalf("new opportunity=%+v err=%v sends=%d", result, err, node.sends)
	}
	node.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: tx.TxID}
	result, err = role.Step(context.Background(), snapshot)
	if err != nil || result.Reason != "Included" || result.Pending != 0 || result.Transactions.Included != 1 {
		t.Fatalf("inclusion=%+v error=%v", result, err)
	}
	result, err = role.Step(context.Background(), snapshot)
	if err != nil || result.Reason != "WaitingCadence" || result.Transactions.Rejected != 1 || result.Transactions.Uncertain != 1 || node.sends != 2 {
		t.Fatalf("stale refusal describes included attempt: %+v error=%v", result, err)
	}
}
