package stacksoperation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/faucetrequest"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// faucetTestNode separates accepted sends, balance observations and exact inclusion.
type faucetTestNode struct {
	*memoryNode
	beforeAccount func()
	afterSend     func()
	rejected      bool
	sent          []transaction.Transaction
}

func (n *faucetTestNode) Info(context.Context) (rpc.Info, error) {
	return rpc.Info{NetworkID: 0x80000000, BurnHeight: 300}, nil
}
func (n *faucetTestNode) Account(ctx context.Context, address string) (rpc.Account, error) {
	if n.beforeAccount != nil {
		n.beforeAccount()
	}
	return n.memoryNode.Account(ctx, address)
}
func (n *faucetTestNode) Submit(ctx context.Context, tx transaction.Transaction) error {
	n.sent = append(n.sent, tx)
	err := n.memoryNode.Submit(ctx, tx)
	if n.afterSend != nil {
		n.afterSend()
	}
	if n.rejected {
		body, _ := json.Marshal(map[string]string{"error": "transaction rejected", "txid": tx.TxID, "reason": "BadNonce"})
		return rpc.ClassifySubmissionRejection(400, body, tx.TxID)
	}
	return err
}

// faucetTestClient models uncertain status acknowledgements; real SSA ownership is covered by envtest.
type faucetTestClient struct {
	client.Client
	readFailure, writeFailure, commitLost bool
	patches                               int
}

func (c *faucetTestClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if c.readFailure {
		return apierrors.NewServiceUnavailable("API unavailable")
	}
	return c.Client.Get(ctx, key, obj, opts...)
}
func (c *faucetTestClient) Status() client.SubResourceWriter {
	return &faucetTestStatus{SubResourceWriter: c.Client.Status(), parent: c}
}

type faucetTestStatus struct {
	client.SubResourceWriter
	parent *faucetTestClient
}

func (w *faucetTestStatus) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	p, ok := obj.(*stacks.StacksFaucetRequest)
	if !ok || p.Status.Execution == nil || p.Status.Admission != nil || p.Status.Phase != "" || patch.Type() != types.ApplyPatchType {
		return errors.New("faucet wrote outside execution subtree")
	}
	c := w.parent
	c.patches++
	if c.writeFailure && !c.commitLost {
		return apierrors.NewServiceUnavailable("status unavailable")
	}
	var current stacks.StacksFaucetRequest
	if err := c.Client.Get(ctx, client.ObjectKeyFromObject(p), &current); err != nil {
		return err
	}
	if current.UID != p.UID || current.ResourceVersion != p.ResourceVersion {
		return apierrors.NewConflict(schema.GroupResource{Group: stacks.GroupVersion.Group, Resource: "stacksfaucetrequests"}, p.Name, errors.New("identity changed"))
	}
	current.Status.Execution = p.Status.Execution.DeepCopy()
	if err := c.Client.Status().Update(ctx, &current); err != nil {
		return err
	}
	*p = current
	if c.writeFailure {
		return apierrors.NewServiceUnavailable("status acknowledgement lost")
	}
	return nil
}

// faucetFixture binds one immutable request to the same worker identity used for all retries.
func faucetFixture(t *testing.T) (*FaucetRole, *faucetTestNode, *faucetTestClient, *stacks.StacksFaucetRequest, *stacksworker.Snapshot, *time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	key := strings.Repeat("0", 63) + "1"
	sender, _ := identity.FromPrivate(key)
	destination, _ := identity.FromPrivate(strings.Repeat("0", 63) + "2")
	r, err := NewFaucetRole(key, sender.Address)
	if err != nil {
		t.Fatal(err)
	}
	r.Namespace, r.ParticipantUID, r.PodUID = "test", "faucet", "pod"
	r.Now = func() time.Time { return now }
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "root"}, Spec: api.StacksNetworkSpec{Operation: "Running", Participants: []api.Participant{{Name: "faucet", Kind: "StacksFaucet"}}}, Status: api.StacksNetworkStatus{Identities: []api.InstanceIdentity{{Name: "faucet", UID: "faucet", Worker: &api.WorkerSession{Pod: api.WorkerPodBinding{Kind: "Pod", Name: "worker", UID: "pod"}, ProfileDigest: "sha256:" + strings.Repeat("a", 64)}}}}}
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "faucet-instance", Namespace: "test", UID: "faucet", OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetwork", Name: root.Name, UID: root.UID, Controller: ptr.To(true)}}}, Spec: api.StacksNetworkParticipantSpec{Kind: "StacksFaucet", NetworkUID: "root", ParticipantName: "faucet"}, Status: api.ParticipantStatus{Admission: &api.Admission{PolicyDigest: "policy"}, Execution: &api.WorkerExecutionStatus{PodUID: "pod", ProcessNonce: "process"}}}
	request := &stacks.StacksFaucetRequest{ObjectMeta: metav1.ObjectMeta{Name: "request", Namespace: "test", UID: "request", CreationTimestamp: metav1.NewTime(now)}, Spec: stacks.StacksFaucetRequestSpec{NetworkUID: "root", FaucetRef: common.NameRef{Name: "faucet"}, Destination: stacks.Recipient{Address: &destination.Address}, AmountMicroSTX: "100", Timeout: "5m"}}
	request.Status.Admission = &stacks.FaucetAdmission{Decision: "Admitted", Reason: "WorkerBound", ExpiresAt: now.Add(5 * time.Minute).Format(time.RFC3339Nano), NetworkUID: "root", Faucet: &stacks.FaucetBinding{Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID}, Worker: &stacks.FaucetBinding{Kind: "Pod", Name: "worker", UID: "pod"}, ProfileDigest: root.Status.Identities[0].Worker.ProfileDigest, SourceAccount: &stacks.FaucetBinding{Kind: "StacksAccount", Name: "sender", UID: "source"}, Target: &stacks.FaucetBinding{Kind: "StacksNetworkParticipant", Name: "node", UID: "node"}, Destination: destination.Address, AmountMicroSTX: "100", FeeMicroSTX: "3000"}
	scheme := runtime.NewScheme()
	_ = api.AddToScheme(scheme)
	_ = stacks.AddToScheme(scheme)
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjectTracker(clienttesting.NewObjectTracker(scheme, serializer.NewCodecFactory(scheme).UniversalDecoder())).WithStatusSubresource(&stacks.StacksFaucetRequest{}, &api.StacksNetworkParticipant{}).WithObjects(root, p, request).Build()
	c := &faucetTestClient{Client: base}
	r.Client = c
	if err := c.Get(ctx, client.ObjectKeyFromObject(request), request); err != nil {
		t.Fatal(err)
	}
	node := &faucetTestNode{memoryNode: nodeFixture()}
	node.account.Balance = clarity.Uint(1000000)
	r.Resolve = func(context.Context, stacksworker.Snapshot, *stacks.FaucetAdmission) (FaucetInputs, error) {
		return FaucetInputs{Node: node, StartHeight: 252}, nil
	}
	snapshot := &stacksworker.Snapshot{Network: root, Participant: p, Authorize: func(context.Context) error { return nil }}
	notifyFaucet(t, r, request, false)
	return r, node, c, request, snapshot, &now
}

func notifyFaucet(t *testing.T, r *FaucetRole, request *stacks.StacksFaucetRequest, deleted bool) {
	t.Helper()
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(request)
	if err != nil {
		t.Fatal(err)
	}
	r.CollectionChanged(r.CollectionWatches()[0], &unstructured.Unstructured{Object: object}, deleted)
}
func faucetStep(t *testing.T, r *FaucetRole, s *stacksworker.Snapshot) stacksworker.RoleResult {
	t.Helper()
	result, err := r.Step(context.Background(), *s)
	if err != nil {
		t.Fatal(err)
	}
	s.Participant.Status.Execution.Faucet = result.Faucet.DeepCopy()
	return result
}

func TestFaucetDuplicateAndRelistedPendingNeverResend(t *testing.T) {
	r, n, c, request, s, _ := faucetFixture(t)
	first := faucetStep(t, r, s)
	if first.Pending != 1 || n.sends != 1 || !transaction.Valid(n.sent[0]) {
		t.Fatalf("first request missing: %+v", first)
	}
	for range 3 {
		notifyFaucet(t, r, request, false)
		_ = faucetStep(t, r, s)
	}
	if n.sends != 1 {
		t.Fatal("stale Pending event replayed transfer")
	}
	n.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	final := faucetStep(t, r, s)
	if final.Pending != 0 || final.Faucet.Completed != 1 || final.Transactions.Included != 1 {
		t.Fatalf("exact inclusion not accounted: %+v", final)
	}
	var saved stacks.StacksFaucetRequest
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(request), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Status.Execution.Phase != "Completed" || saved.Status.Execution.TxID != n.sent[0].TxID {
		t.Fatal("request missing exact completion")
	}
	notifyFaucet(t, r, request, false)
	_ = faucetStep(t, r, s)
	if n.sends != 1 || len(r.entries) != 0 {
		t.Fatal("settled UID replayed after slot release")
	}
}

func TestFaucetLostRequestStatusAcknowledgementRetainsOriginalOutcome(t *testing.T) {
	r, n, c, request, s, now := faucetFixture(t)
	c.writeFailure, c.commitLost = true, true
	first := faucetStep(t, r, s)
	if first.Pending != 1 || n.sends != 1 {
		t.Fatal("uncertain publication lost submission")
	}
	original := r.entries[request.UID].outcome.ObservedAt
	*now = now.Add(20 * time.Second)
	notifyFaucet(t, r, request, false)
	_ = faucetStep(t, r, s)
	if c.patches != 1 || !r.entries[request.UID].outcome.ObservedAt.Equal(&original) {
		t.Fatal("readback rewrote original acknowledged outcome")
	}
	c.writeFailure = false
	n.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	final := faucetStep(t, r, s)
	if final.Pending != 0 || n.sends != 1 || final.Faucet.Completed != 1 {
		t.Fatal("publication recovery replayed or lost inclusion")
	}
}

func TestFaucetAPIOutageStillObservesOneExactPendingInclusion(t *testing.T) {
	r, n, c, _, s, now := faucetFixture(t)
	_ = faucetStep(t, r, s)
	c.readFailure = true
	*now = now.Add(time.Second)
	n.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("c", 64)}
	for range 3 {
		if err := r.ObservePending(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n.reads != 1 || n.sends != 1 || r.summary.Completed != 1 || r.stream.Pending() != 0 {
		t.Fatal("outage observation duplicated or lost the original pending transaction")
	}
	c.readFailure = false
	final := faucetStep(t, r, s)
	if final.Pending != 0 || final.Faucet.Completed != 1 || n.sends != 1 {
		t.Fatalf("API recovery did not publish retained completion: %+v", final)
	}
}

func TestFaucetDeletionRetainsUnknownNonceAndThenPublishesSummary(t *testing.T) {
	r, n, c, request, s, _ := faucetFixture(t)
	_ = faucetStep(t, r, s)
	if err := c.Delete(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	deleted := faucetStep(t, r, s)
	if deleted.Pending != 1 || deleted.Faucet.Orphaned != 1 || deleted.Faucet.LastDeletedOutcome == nil || r.stream.Pending() != 1 {
		t.Fatal("deletion cancelled submitted transfer or erased evidence")
	}
	n.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("d", 64)}
	observed := faucetStep(t, r, s)
	if observed.Pending != 1 || observed.Faucet.LastDeletedOutcome.Execution.Phase != "Completed" {
		t.Fatal("deleted outcome freed before participant acknowledgement")
	}
	confirmed := faucetStep(t, r, s)
	if confirmed.Pending != 0 || confirmed.Faucet.Completed != 1 || n.sends != 1 {
		t.Fatal("deleted settled outcome did not release after acknowledgement")
	}
	var missing stacks.StacksFaucetRequest
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(request), &missing); !apierrors.IsNotFound(err) {
		t.Fatal("worker recreated deleted request")
	}
}

func TestFaucetDeadlinesPauseDeletionAndFreshReadsPreventNewSend(t *testing.T) {
	for _, change := range []string{"expired", "pause", "cached", "delete-final-read", "deadline-final-read", "API-final-read"} {
		t.Run(change, func(t *testing.T) {
			r, n, c, request, s, now := faucetFixture(t)
			switch change {
			case "expired":
				*now = now.Add(5 * time.Minute)
			case "pause":
				s.Paused = true
			case "cached":
				s.CachedApplied = true
			case "delete-final-read":
				s.Authorize = func(context.Context) error { return c.Delete(context.Background(), request) }
			case "deadline-final-read":
				n.beforeAccount = func() { *now = now.Add(5 * time.Minute) }
			case "API-final-read":
				s.Authorize = func(context.Context) error { c.readFailure = true; return nil }
			}
			result := faucetStep(t, r, s)
			if n.sends != 0 || r.stream.Pending() != 0 {
				t.Fatalf("request sent without fresh eligibility: %+v", result)
			}
			if change == "expired" || change == "deadline-final-read" {
				if result.Faucet.Expired != 1 {
					t.Fatal("definite deadline refusal not recorded")
				}
			}
		})
	}
}

func TestFaucetSendBegunBeforeDeadlineMayCompleteAfterward(t *testing.T) {
	r, n, _, _, s, now := faucetFixture(t)
	n.afterSend = func() { *now = now.Add(6 * time.Minute) }
	sent := faucetStep(t, r, s)
	if sent.Pending != 1 || n.sends != 1 {
		t.Fatal("before-deadline send not retained")
	}
	waiting := faucetStep(t, r, s)
	if waiting.Pending != 1 || r.entries[r.pendingUID].outcome.Phase != "Inconclusive" {
		t.Fatal("post-send deadline invented no-send expiry")
	}
	n.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("d", 64)}
	final := faucetStep(t, r, s)
	if final.Pending != 0 || final.Faucet.Completed != 1 {
		t.Fatal("late inclusion could not refine deadline uncertainty")
	}
}

func TestFaucetFundsRejectionReadFailureAndNonceConflictDiffer(t *testing.T) {
	for _, change := range []string{"funds", "read", "nonce", "submitted-rejection"} {
		t.Run(change, func(t *testing.T) {
			r, n, _, _, s, _ := faucetFixture(t)
			switch change {
			case "funds":
				n.account.Balance = clarity.Uint(1)
			case "read":
				n.errorRead = errors.New("account observation unavailable")
			case "nonce":
				r.stream.initialized = true
				r.stream.next = n.account.Nonce - 1
			case "submitted-rejection":
				n.rejected = true
			}
			result := faucetStep(t, r, s)
			if change == "funds" {
				if result.Faucet.Rejected != 1 || result.Pending != 0 || n.sends != 0 {
					t.Fatal("definite insufficient balance not rejected before send")
				}
			} else if change == "submitted-rejection" {
				if result.Pending != 0 || n.sends != 1 || result.Transactions.Rejected != 1 || result.Faucet.Rejected != 1 {
					t.Fatal("native refusal did not settle request")
				}
			} else if result.Faucet.Rejected != 0 || n.sends != 0 || result.Pending != 1 {
				t.Fatalf("unknown read or nonce conflict invented rejection: %+v", result)
			}
		})
	}
}

func TestFaucetQueueIsBoundedAndOrderedWithoutLifetimeDedup(t *testing.T) {
	r, n, c, request, s, _ := faucetFixture(t)
	for i := 0; i < faucetrequest.Capacity+5; i++ {
		copy := request.DeepCopy()
		copy.Name = fmt.Sprintf("r-%04d", i)
		copy.UID = types.UID(copy.Name)
		copy.ResourceVersion = ""
		copy.CreationTimestamp = metav1.NewTime(request.CreationTimestamp.Add(-time.Second))
		copy.Status.Admission.ExpiresAt = copy.CreationTimestamp.Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
		status := copy.Status.DeepCopy()
		if err := c.Create(context.Background(), copy); err != nil {
			t.Fatal(err)
		}
		copy.Status = *status
		if err := c.Client.Status().Update(context.Background(), copy); err != nil {
			t.Fatal(err)
		}
		if i == 0 && !r.matches(copy, *s) {
			t.Fatalf("queue fixture invalid: %+v admission=%+v", copy.ObjectMeta, copy.Status.Admission)
		}
		notifyFaucet(t, r, copy, false)
	}
	if len(r.notices) != faucetrequest.Capacity || !r.rescan {
		t.Fatal("notification capacity unbounded or overflow forgotten")
	}
	result := faucetStep(t, r, s)
	if n.sends != 1 || r.pendingUID != "r-0000" || len(r.notices)+len(r.entries) > faucetrequest.Capacity {
		t.Fatalf("best-effort order or local capacity violated: sends=%d pending=%s notices=%d entries=%d result=%+v", n.sends, r.pendingUID, len(r.notices), len(r.entries), result)
	}
}

func TestFaucetDrainRefusesQueuedWorkAndRetainsSubmittedWork(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(fmt.Sprint(sent), func(t *testing.T) {
			r, n, c, request, s, _ := faucetFixture(t)
			if sent {
				faucetStep(t, r, s)
			}
			result, err := r.Drain(context.Background(), *s)
			if err != nil {
				t.Fatal(err)
			}
			if sent {
				if result.Done || result.Pending != 1 || n.sends != 1 {
					t.Fatal("drain discarded unknown submission")
				}
				n.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("a", 64)}
				result, err = r.Drain(context.Background(), *s)
				if err != nil || !result.Done || !result.Settled || result.Faucet.Completed != 1 || n.sends != 1 {
					t.Fatal("exact settled send failed drain")
				}
			} else {
				var current stacks.StacksFaucetRequest
				if err := c.Get(context.Background(), client.ObjectKeyFromObject(request), &current); err != nil {
					t.Fatal(err)
				}
				if !result.Done || n.sends != 0 || !current.Status.Execution.NoSend || current.Status.Execution.Reason != "WorkerStoppedBeforeSend" {
					t.Fatal("queued shutdown did not retain affirmative refusal")
				}
			}
		})
	}
}

func TestFaucetFinalDependencyReadPreventsSendAndLostTerminalWriteRetainsOutcome(t *testing.T) {
	r, n, _, _, s, _ := faucetFixture(t)
	resolve := r.Resolve
	calls := 0
	r.Resolve = func(ctx context.Context, s stacksworker.Snapshot, a *stacks.FaucetAdmission) (FaucetInputs, error) {
		calls++
		if calls == 2 {
			return FaucetInputs{}, errors.New("source replaced")
		}
		return resolve(ctx, s, a)
	}
	faucetStep(t, r, s)
	if n.sends != 0 || calls != 2 {
		t.Fatal("final dependency loss allowed send")
	}
	r, n, c, _, s, _ := faucetFixture(t)
	faucetStep(t, r, s)
	c.writeFailure = true
	n.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	first := faucetStep(t, r, s)
	if first.Faucet.Completed != 1 || first.Pending != 1 || n.sends != 1 {
		t.Fatal("failed terminal write lost retained evidence")
	}
	c.writeFailure = false
	final := faucetStep(t, r, s)
	if final.Faucet.Completed != 1 || final.Pending != 0 || n.sends != 1 {
		t.Fatal("recovery repeated send or completion")
	}
}

func TestFaucetNativeRejectionSurvivesPublicationLossWithoutResend(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			r, n, c, request, s, now := faucetFixture(t)
			n.rejected = true
			c.writeFailure = true
			c.commitLost = committed
			got := faucetStep(t, r, s)
			if got.Pending != 1 || r.stream.Pending() != 0 || n.sends != 1 {
				t.Fatal("lost rejection report was not retained")
			}
			for range 3 {
				*now = now.Add(time.Minute)
				notifyFaucet(t, r, request, false)
				_ = faucetStep(t, r, s)
			}
			if n.sends != 1 {
				t.Fatal("lost rejection acknowledgement replayed")
			}
			c.writeFailure = false
			got = faucetStep(t, r, s)
			var saved stacks.StacksFaucetRequest
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(request), &saved); err != nil {
				t.Fatal(err)
			}
			e := saved.Status.Execution
			if got.Pending != 0 || e == nil || e.Phase != "Rejected" || e.Reason != "RejectedBadNonce" || e.NoSend || e.TxID != n.sent[0].TxID || e.InclusionBlockID != "" {
				t.Fatalf("rejection evidence: %+v", e)
			}
			notifyFaucet(t, r, request, false)
			_ = faucetStep(t, r, s)
			drained, err := r.Drain(context.Background(), *s)
			if err != nil || !drained.Done || !drained.Settled || n.sends != 1 {
				t.Fatalf("rejected request replayed or stranded drain: %+v %v", drained, err)
			}
		})
	}
}
