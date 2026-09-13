package stacksoperation

import (
	"context"
	"errors"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/faucetrequest"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	errFaucetDeadline = errors.New("faucet deadline refused")
	errFaucetDeleted  = errors.New("faucet request deleted or replaced")
	errFaucetTerminal = errors.New("faucet request already has execution evidence")
)

// faucetEntry retains original request identity and outcome until a confirmed status acknowledgement.
type faucetEntry struct {
	request  *stacks.StacksFaucetRequest
	outcome  *stacks.FaucetExecution
	orphaned bool
}

// FaucetRole serializes immutable admitted requests using one surviving account nonce stream.
type FaucetRole struct {
	// Client is uncached, with request status writes and scoped public reads only.
	Client client.Client
	// Namespace, ParticipantUID and PodUID are fixed at worker creation.
	Namespace              string
	ParticipantUID, PodUID types.UID
	// Resolve fresh-validates source/target bindings for each new dispatch.
	Resolve func(context.Context, stacksworker.Snapshot, *stacks.FaucetAdmission) (FaucetInputs, error)
	// Now injects the wall clock without changing original request deadlines.
	Now                   func() time.Time
	key                   string
	stream                NonceStream
	processNonce, applied string
	pendingUID            types.UID
	entries               map[types.UID]*faucetEntry
	summary               stacks.FaucetWorkerSummary
	failed                bool
	mu                    sync.Mutex
	notices               map[types.UID]faucetNotice
	rescan                bool
}

// NewFaucetRole validates the mounted funding key before accepting any request notifications.
func NewFaucetRole(privateKey, address string) (*FaucetRole, error) {
	key, err := identity.CompressedPrivateKey(privateKey)
	if err != nil {
		return nil, errors.New("invalid faucet key")
	}
	public, err := identity.FromPrivate(key)
	if err != nil || public.Address != address {
		return nil, errors.New("faucet funding identity differs")
	}
	return &FaucetRole{key: key, stream: NonceStream{Address: address}, entries: map[types.UID]*faucetEntry{}, notices: map[types.UID]faucetNotice{}}, nil
}

// now returns the observation clock.
func (r *FaucetRole) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Step handles fresh request state; cached baselines never authorize faucet dispatch.
func (r *FaucetRole) Step(ctx context.Context, s stacksworker.Snapshot) (stacksworker.RoleResult, error) {
	if r.Client == nil || r.Resolve == nil || s.Network == nil || s.Participant == nil || s.Participant.UID != r.ParticipantUID || s.Participant.Status.Admission == nil || s.Participant.Status.Execution == nil || s.Participant.Status.Execution.PodUID != r.PodUID || s.Participant.Status.Execution.ProcessNonce == "" {
		return r.result(reasonWorkerIdentityUnavailable), nil
	}
	if r.processNonce == "" {
		r.processNonce = s.Participant.Status.Execution.ProcessNonce
	}
	if r.processNonce != s.Participant.Status.Execution.ProcessNonce {
		r.failed = true
		return r.result(reasonWorkerProcessChanged), nil
	}
	r.applied = s.Participant.Status.Admission.PolicyDigest
	_ = r.ObservePending(ctx)
	if err := r.flush(ctx, s); err != nil {
		return r.result(reasonRequestPublicationUnavailable), nil
	}
	if r.failed {
		return r.result(reasonRequestStateLost), nil
	}
	if err := r.rescanNotices(ctx); err != nil {
		return r.result(reasonRequestListUnavailable), nil
	}
	var entry *faucetEntry
	for _, candidate := range r.entries {
		if candidate.outcome == nil {
			entry = candidate
			break
		}
	}
	if entry == nil {
		notice, ok := r.takeNotice(r.stream.Pending() != 0)
		if !ok {
			return r.result(reasonWaitingRequests), nil
		}
		var request stacks.StacksFaucetRequest
		if err := r.Client.Get(ctx, notice.key, &request); err != nil {
			if apierrors.IsNotFound(err) {
				r.forgetNotice(notice.uid)
				return r.result(reasonRequestAbsent), nil
			}
			return r.result(reasonRequestReadUnavailable), nil
		}
		if request.UID != notice.uid || !r.matches(&request, s) {
			r.forgetNotice(notice.uid)
			return r.result(reasonRequestBindingUnavailable), nil
		}
		if faucetrequest.MatchingExecution(&request) && faucetrequest.TerminalExecution(request.Status.Execution) {
			r.forgetNotice(notice.uid)
			return r.result(reasonRequestAlreadySettled), nil
		}
		if request.Status.Execution != nil {
			r.failed = true
			return r.result(reasonRequestStateLost), nil
		}
		entry = &faucetEntry{request: request.DeepCopy()}
		r.putEntry(entry)
	}
	if entry.request.DeletionTimestamp != nil {
		r.noSend(entry, stacks.FaucetExecutionExpired, reasonDeletedBeforeSend)
		_ = r.flush(ctx, s)
		return r.result(reasonRequestExpired), nil
	}
	deadline, err := faucetrequest.Deadline(entry.request)
	if err != nil {
		return r.result(reasonInvalidRequestDeadline), nil
	}
	if !r.now().Before(deadline) {
		r.noSend(entry, stacks.FaucetExecutionExpired, reasonDeadlineBeforeSend)
		_ = r.flush(ctx, s)
		return r.result(reasonRequestExpired), nil
	}
	if s.Paused || s.CachedApplied {
		return r.result(reasonPaused), nil
	}
	if r.stream.Pending() != 0 {
		return r.result(reasonAwaitingInclusion), nil
	}
	input, err := r.Resolve(ctx, s, entry.request.Status.Admission)
	if err != nil || input.Node == nil {
		return r.result(reasonDependenciesUnavailable), nil
	}
	info, err := input.Node.Info(ctx)
	if err != nil {
		return r.result(reasonChainUnavailable), nil
	}
	if info.NetworkID != 0x80000000 || input.StartHeight == 0 || info.BurnHeight < input.StartHeight {
		return r.result(reasonAwaitingNakamoto), nil
	}
	amount, err := strconv.ParseUint(string(entry.request.Status.Admission.AmountMicroSTX), 10, 64)
	if err != nil || amount == 0 {
		return r.result(reasonInvalidRequestAmount), nil
	}
	fee, err := strconv.ParseUint(string(entry.request.Status.Admission.FeeMicroSTX), 10, 64)
	if err != nil || fee != faucetrequest.FeeMicroSTX {
		return r.result(reasonInvalidRequestFee), nil
	}
	funds := new(big.Int).Add(new(big.Int).SetUint64(amount), new(big.Int).SetUint64(fee))
	reason, offerErr := r.stream.Offer(ctx, r.now, input.Node, funds, func(ctx context.Context) error { return r.authorize(ctx, s, entry) }, func(nonce uint64) (transaction.Transaction, error) {
		return transaction.Transfer(transaction.Options{Version: transaction.Testnet, ChainID: 0x80000000, Nonce: nonce, Fee: fee, PostConditionMode: transaction.Deny, PrivateKey: r.key}, entry.request.Status.Admission.Destination, amount, "")
	})
	if r.stream.Pending() != 0 {
		r.pendingUID = entry.request.UID
		entry.outcome = r.execution(entry, stacks.FaucetExecutionSubmitted, reason, false)
		entry.outcome.TxID = r.stream.pending.transaction.TxID
		if reason != reasonAccepted {
			entry.outcome.Phase = stacks.FaucetExecutionInconclusive
		}
	} else {
		switch {
		case strings.HasPrefix(reason, stacks.RejectionReasonPrefix) && rpc.ValidationRejectionReason(strings.TrimPrefix(reason, stacks.RejectionReasonPrefix)):
			entry.outcome = r.execution(entry, stacks.FaucetExecutionRejected, reason, false)
			entry.outcome.TxID = r.stream.facts.LastTxID
			r.increment(&r.summary.Rejected)
		case errors.Is(offerErr, errFaucetDeadline):
			r.noSend(entry, stacks.FaucetExecutionExpired, reasonDeadlineBeforeSend)
		case errors.Is(offerErr, errFaucetDeleted):
			r.noSend(entry, stacks.FaucetExecutionExpired, reasonDeletedBeforeSend)
		case errors.Is(offerErr, errFaucetTerminal):
			r.forgetEntry(entry.request.UID)
		case reason == reasonInsufficientFunds:
			r.noSend(entry, stacks.FaucetExecutionRejected, reasonInsufficientFunds)
		case reason == reasonConstructionFailed:
			r.noSend(entry, stacks.FaucetExecutionRejected, reasonConstructionFailed)
		}
	}
	if err := r.flush(ctx, s); err != nil {
		return r.result(reasonRequestPublicationUnavailable), nil
	}
	return r.result(reason), nil
}

// matches verifies immutable request admission independently of informer delivery.
func (r *FaucetRole) matches(request *stacks.StacksFaucetRequest, s stacksworker.Snapshot) bool {
	a := request.Status.Admission
	if a == nil || a.Decision != stacks.FaucetDecisionAdmitted || a.Faucet == nil || a.Worker == nil || a.SourceAccount == nil || a.Target == nil || a.NetworkUID != request.Spec.NetworkUID || a.NetworkUID != s.Network.UID || a.Faucet.UID != r.ParticipantUID || a.Faucet.Name != s.Participant.Name || a.Worker.UID != r.PodUID || request.Namespace != r.Namespace || a.AmountMicroSTX != request.Spec.AmountMicroSTX || !faucetrequest.ValidAddress(a.Destination) {
		return false
	}
	deadline, err := faucetrequest.Deadline(request)
	if err != nil || a.ExpiresAt != deadline.Format(time.RFC3339Nano) {
		return false
	}
	session, err := stacksworker.Session(s.Network, s.Participant)
	return err == nil && session.Worker != nil && session.Worker.Pod.UID == a.Worker.UID && session.Worker.Pod.Name == a.Worker.Name && session.Worker.ProfileDigest == a.ProfileDigest
}

// authorize fresh-reads root/prerequisites and then the exact request immediately before the only send.
func (r *FaucetRole) authorize(ctx context.Context, s stacksworker.Snapshot, entry *faucetEntry) error {
	if s.CachedApplied || s.Paused || s.Authorize == nil {
		return errors.New("faucet fresh authority unavailable")
	}
	if err := s.Authorize(ctx); err != nil {
		return err
	}
	if _, err := r.Resolve(ctx, s, entry.request.Status.Admission); err != nil {
		return err
	}
	var current stacks.StacksFaucetRequest
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(entry.request), &current); err != nil {
		if apierrors.IsNotFound(err) {
			return errFaucetDeleted
		}
		return err
	}
	if current.UID != entry.request.UID || current.DeletionTimestamp != nil {
		return errFaucetDeleted
	}
	if !r.matches(&current, s) || !equality.Semantic.DeepEqual(current.Status.Admission, entry.request.Status.Admission) {
		return errors.New("faucet admission changed")
	}
	if current.Status.Execution != nil || current.Status.Phase == stacks.FaucetCompleted || current.Status.Phase == stacks.FaucetRejected || current.Status.Phase == stacks.FaucetExpired {
		return errFaucetTerminal
	}
	deadline, err := faucetrequest.Deadline(&current)
	if err != nil || !r.now().Before(deadline) {
		return errFaucetDeadline
	}
	return nil
}

// ObservePending polls only the original submitted TxID, including during API/status outages.
func (r *FaucetRole) ObservePending(ctx context.Context) error {
	if r.pendingUID == "" {
		return nil
	}
	entry := r.entries[r.pendingUID]
	if entry == nil || entry.outcome == nil {
		r.failed = true
		return errors.New("faucet pending identity lost")
	}
	reason, err := r.stream.Observe(ctx, r.now())
	if err != nil {
		return err
	}
	if r.stream.Pending() == 0 {
		inclusion := r.stream.facts.LastInclusion
		if inclusion == nil || inclusion.TxID != entry.outcome.TxID {
			r.failed = true
			return errors.New("faucet inclusion identity differs")
		}
		entry.outcome.Phase = stacks.FaucetExecutionCompleted
		entry.outcome.Reason = reasonIncluded
		if !inclusion.Success {
			entry.outcome.Phase = stacks.FaucetExecutionRejected
			entry.outcome.Reason = reasonExecutionRejected
			r.increment(&r.summary.Rejected)
		} else {
			r.increment(&r.summary.Completed)
		}
		entry.outcome.InclusionBlockID = inclusion.BlockID
		entry.outcome.ObservedAt = inclusion.ObservedAt
		r.pendingUID = ""
		return nil
	}
	deadline, _ := faucetrequest.Deadline(entry.request)
	if !r.now().Before(deadline) && entry.outcome.Phase != stacks.FaucetExecutionInconclusive {
		entry.outcome.Phase = stacks.FaucetExecutionInconclusive
		entry.outcome.Reason = api.ReasonDeadlineOutcomeUnknown
		entry.outcome.ObservedAt = metav1.NewTime(r.now().UTC())
	}
	_ = reason
	return nil
}

// execution binds every outcome to this process and the original immutable transfer inputs.
func (r *FaucetRole) execution(entry *faucetEntry, phase stacks.FaucetExecutionPhase, reason string, noSend bool) *stacks.FaucetExecution {
	a := entry.request.Status.Admission
	return &stacks.FaucetExecution{Phase: phase, Reason: reason, NetworkUID: a.NetworkUID, FaucetUID: r.ParticipantUID, WorkerUID: r.PodUID, ProcessNonce: r.processNonce, NoSend: noSend, Destination: a.Destination, AmountMicroSTX: a.AmountMicroSTX, ObservedAt: metav1.NewTime(r.now().UTC())}
}

// noSend records affirmative process-local refusal, never an inference from a failed balance read.
func (r *FaucetRole) noSend(entry *faucetEntry, phase stacks.FaucetExecutionPhase, reason string) {
	if entry.outcome != nil {
		return
	}
	entry.outcome = r.execution(entry, phase, reason, true)
	if phase == stacks.FaucetExecutionExpired {
		r.increment(&r.summary.Expired)
	} else {
		r.increment(&r.summary.Rejected)
	}
}

// increment fails closed rather than wrapping cumulative request evidence.
func (r *FaucetRole) increment(counter *uint64) {
	if *counter == math.MaxUint64 {
		r.failed = true
		return
	}
	*counter++
}

// flush retains each outcome through lost acknowledgements and never recreates deleted requests.
func (r *FaucetRole) flush(ctx context.Context, s stacksworker.Snapshot) error {
	ids := make([]string, 0, len(r.entries))
	for uid := range r.entries {
		ids = append(ids, string(uid))
	}
	sort.Strings(ids)
	for _, id := range ids {
		entry := r.entries[types.UID(id)]
		if entry.outcome == nil {
			continue
		}
		var current stacks.StacksFaucetRequest
		err := r.Client.Get(ctx, client.ObjectKeyFromObject(entry.request), &current)
		if apierrors.IsNotFound(err) || err == nil && current.UID != entry.request.UID {
			entry.orphaned = true
			retained := &stacks.FaucetRetainedOutcome{Request: stacks.FaucetBinding{Kind: stacks.KindStacksFaucetRequest, Name: entry.request.Name, UID: entry.request.UID}, Execution: *entry.outcome.DeepCopy()}
			r.summary.LastDeletedOutcome = retained
			acknowledged := s.Participant.Status.Execution.Faucet != nil && equality.Semantic.DeepEqual(s.Participant.Status.Execution.Faucet.LastDeletedOutcome, retained)
			if !acknowledged {
				return errors.New("deleted request outcome awaits participant acknowledgement")
			}
			if faucetrequest.TerminalExecution(entry.outcome) {
				r.forgetEntry(entry.request.UID)
			}
			continue
		}
		if err != nil {
			return err
		}
		if !equality.Semantic.DeepEqual(current.Status.Admission, entry.request.Status.Admission) {
			return errors.New("request admission changed before outcome acknowledgement")
		}
		if !equality.Semantic.DeepEqual(current.Status.Execution, entry.outcome) {
			if err = faucetrequest.ApplyStatus(ctx, r.Client, &current, stacks.FaucetRequestStatus{Execution: entry.outcome}, faucetrequest.ExecutionManager); err != nil {
				return err
			}
		}
		if faucetrequest.TerminalExecution(entry.outcome) {
			r.forgetEntry(entry.request.UID)
		}
	}
	return nil
}

// result publishes bounded capacity and cumulative facts without signed bytes or request nonce history.
func (r *FaucetRole) result(reason string) stacksworker.RoleResult {
	r.mu.Lock()
	active := len(r.entries) + len(r.notices)
	r.mu.Unlock()
	r.summary.Active = int32(active)
	r.summary.Orphaned = 0
	for _, entry := range r.entries {
		if entry.orphaned && !faucetrequest.TerminalExecution(entry.outcome) {
			r.summary.Orphaned++
		}
	}
	return stacksworker.RoleResult{Faucet: r.summary.DeepCopy(), Transactions: r.stream.Facts(), AppliedPolicyDigest: r.applied, Pending: int32(active), Reason: reason, Failed: r.failed, Blocked: reason == reasonWorkerIdentityUnavailable || reason == reasonDependenciesUnavailable || reason == reasonRequestPublicationUnavailable || reason == reasonRequestStateLost, RequeueAfter: time.Second}
}

// Drain refuses queued unsent work and preserves submitted coordination until exact settlement.
func (r *FaucetRole) Drain(ctx context.Context, s stacksworker.Snapshot) (stacksworker.DrainResult, error) {
	if r.processNonce == "" && s.Participant != nil && s.Participant.Status.Execution != nil {
		r.processNonce = s.Participant.Status.Execution.ProcessNonce
	}
	_ = r.ObservePending(ctx)
	for _, entry := range r.entries {
		if entry.outcome == nil {
			r.noSend(entry, stacks.FaucetExecutionExpired, reasonWorkerStoppedBeforeSend)
		}
	}
	err := r.flush(ctx, s)
	for err == nil {
		notice, ok := r.takeNotice(false)
		if !ok {
			break
		}
		var request stacks.StacksFaucetRequest
		err = r.Client.Get(ctx, notice.key, &request)
		if apierrors.IsNotFound(err) {
			r.forgetNotice(notice.uid)
			err = nil
			continue
		}
		if err != nil {
			break
		}
		if request.UID != notice.uid || !r.matches(&request, s) || faucetrequest.MatchingExecution(&request) && faucetrequest.TerminalExecution(request.Status.Execution) {
			r.forgetNotice(notice.uid)
			continue
		}
		if request.Status.Execution != nil {
			r.failed = true
			break
		}
		entry := &faucetEntry{request: request.DeepCopy()}
		r.putEntry(entry)
		r.noSend(entry, stacks.FaucetExecutionExpired, reasonWorkerStoppedBeforeSend)
		err = r.flush(ctx, s)
	}
	result := r.result(reasonDraining)
	done := result.Pending == 0 && !r.failed
	return stacksworker.DrainResult{Done: done, Settled: done, Pending: result.Pending, Faucet: result.Faucet, Transactions: result.Transactions}, err
}
