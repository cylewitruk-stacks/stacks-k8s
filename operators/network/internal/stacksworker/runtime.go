package stacksworker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"reflect"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Snapshot carries current controls and either live or explicitly cached applied baseline inputs.
// Authorize must run immediately before every send.
type Snapshot struct {
	// admissionError retains the fresh source/eligibility result without preventing observations.
	admissionError error
	// CachedApplied permits only this process's previously validated applied baseline inputs.
	CachedApplied bool
	// AppliedFallback returns that applied snapshot only for transient Kubernetes read failures.
	AppliedFallback func(string, error) (Snapshot, bool)
	// RememberApplied records a live, successfully validated typed policy before using it.
	RememberApplied func(string)
	// Network contains current desired control and durable worker identity.
	Network *api.StacksNetwork
	// Participant contains complete admitted policy, independent of rejected candidates.
	Participant *api.StacksNetworkParticipant
	// Profile contains immutable mounted-key/config and image identities.
	Profile Profile
	// Paused withdraws new submissions while permitting pending observations.
	Paused bool
	// Shutdown identifies aggregate-recorded intentional disposal, if requested.
	Shutdown *api.WorkerShutdown
	// Authorize checks exact identities and permits only surviving applied baselines during API outages.
	Authorize func(context.Context) error
}

// Role owns its ephemeral account nonce and pending submissions for this one process.
type Role interface {
	// Step observes current state and adopts policy only at safe operation boundaries.
	Step(context.Context, Snapshot) (RoleResult, error)
	// Drain stops new sends and settles or dispositions pending outcomes within ShutdownBound.
	Drain(context.Context, Snapshot) (DrainResult, error)
}

// RoleResult contains bounded public execution facts, never private keys or nonce history.
type RoleResult struct {
	// Traffic contains the current canonical transfer recheck.
	Traffic *api.TrafficObservation
	// Faucet retains cumulative bounded request accounting.
	Faucet *stacks.FaucetWorkerSummary
	// PoX5 contains the current native direct-manager enrollment observation.
	PoX5 *api.PoX5EnrollmentObservation
	// AdministratorTransactions retains a separate administrator stream's cumulative facts.
	AdministratorTransactions *api.TransactionExecutionStatus
	// PoX4 contains a bounded current native enrollment observation.
	PoX4 *api.PoX4EnrollmentObservation
	// Contracts contains exact public source and registry observations.
	Contracts *api.ContractSetObservation
	// Blocked holds new submissions while retaining process-local state.
	Blocked bool
	// Transactions contains bounded cumulative facts retained until publication succeeds.
	Transactions *api.TransactionExecutionStatus
	// AppliedPolicyDigest identifies the complete policy adopted by the role.
	AppliedPolicyDigest string
	// Pending counts unresolved local submissions.
	Pending int32
	// Reason supplies a public bounded classification.
	Reason string
	// Failed requests a durable experiment failure through the aggregate projection.
	Failed bool
	// RequeueAfter schedules observation of protocol state that Kubernetes does not watch.
	RequeueAfter time.Duration
}

// DrainResult distinguishes continued observation from final bounded disposition.
type DrainResult struct {
	// Traffic contains the current canonical transfer recheck.
	Traffic *api.TrafficObservation
	// Faucet retains cumulative bounded request accounting.
	Faucet *stacks.FaucetWorkerSummary
	// PoX5 optionally updates native enrollment facts during settlement.
	PoX5 *api.PoX5EnrollmentObservation
	// AdministratorTransactions optionally updates separate administrator accounting.
	AdministratorTransactions *api.TransactionExecutionStatus
	// PoX4 optionally updates native enrollment facts during settlement.
	PoX4 *api.PoX4EnrollmentObservation
	// Contracts contains exact public source and registry observations.
	Contracts *api.ContractSetObservation
	// Transactions optionally updates final canonical/submission facts during settlement.
	Transactions *api.TransactionExecutionStatus
	// Done ends settlement observations and publishes a terminal acknowledgement.
	Done bool
	// Settled confirms no unresolved local submissions remain.
	Settled bool
	// Pending retains the unresolved local count when uncertainty remains.
	Pending int32
}

// Result controls the worker process loop without replacing its role or ephemeral state.
type Result struct {
	// Exit follows durable aggregate disposal acknowledgement or unused-candidate shutdown.
	Exit bool
	// RequeueAfter schedules the next fresh reconciliation.
	RequeueAfter time.Duration
}

// Runtime executes one role in one exact non-restarting Pod.
type Runtime struct {
	// Now supplies the observation clock; nil uses wall time.
	Now func() time.Time
	// Client must be uncached and is restricted to named public reads and own execution writes.
	Client client.Client
	// Dynamic provides named list/watch notification streams, never cached authorization.
	Dynamic dynamic.Interface
	// Namespace scopes all Kubernetes operations.
	Namespace string
	// ParticipantName identifies the generated participant CR.
	ParticipantName string
	// NetworkUID pins the environment supplied at candidate creation.
	NetworkUID types.UID
	// ParticipantUID pins the generated instance supplied at creation.
	ParticipantUID types.UID
	// PodName is supplied through the downward API.
	PodName string
	// PodUID is supplied through the downward API.
	PodUID types.UID
	// Profile contains the frozen bootstrap parameters supplied at creation.
	Profile Profile
	// Role retains its nonce and pending state across watch reconnects and duplicate events.
	Role Role
	// Prerequisites validates role-specific current public dependencies without Secret API reads.
	Prerequisites func(context.Context, Snapshot) error
	// nonce identifies this operating-system process, distinct from an account transaction nonce.
	nonce string
	// activated remembers a successful durable binding within this surviving process.
	activated bool
	// processAcknowledged requires confirmed publication before any outage fallback.
	processAcknowledged bool
	// appliedSnapshot is the last policy explicitly validated by the surviving role.
	appliedSnapshot *Snapshot
	// lastControl retains the latest actually read root and participant control identities.
	lastControl *Snapshot
	// cacheHeld persists a definite denial until live validation succeeds.
	cacheHeld bool
	// cacheBlocked prevents fallback after definite API authorization or process identity loss.
	cacheBlocked bool
	// stepUsedCached records explicit role fallback during this invocation.
	stepUsedCached bool
	// acknowledged retains the original final drain report across uncertain status writes.
	acknowledged *api.WorkerExecutionStatus
	// pendingReport retains unconfirmed evidence; surviving baselines may coalesce cumulative outcomes.
	pendingReport *api.WorkerExecutionStatus
	// pendingDelay preserves the role's requested cadence after publication recovery.
	pendingDelay time.Duration
}

// fresh validates immutable identity even while activation is held.
func (r *Runtime) fresh(ctx context.Context) (snapshot Snapshot, readErr error) {
	defer func() {
		if readErr != nil && !TransientAPIError(readErr) {
			r.cacheHeld = true
			r.cacheBlocked = true
		}
	}()
	var root api.StacksNetwork
	var p api.StacksNetworkParticipant
	var pod corev1.Pod
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: r.Namespace, Name: "network"}, &root); err != nil {
		return Snapshot{}, err
	}
	r.observePartialRoot(&root)
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: r.Namespace,
		Name:      r.ParticipantName,
	}, &p); err != nil {
		return Snapshot{}, err
	}
	if root.UID != r.NetworkUID || p.UID != r.ParticipantUID || p.Spec.NetworkUID != r.NetworkUID {
		return Snapshot{}, fmt.Errorf("worker environment identity changed")
	}
	if p.DeletionTimestamp != nil {
		r.cacheHeld = true
	}
	admissionErr := foundation.ValidateAdmissionEligibility(ctx, r.Client, &p)
	if admissionErr != nil && !TransientAPIError(admissionErr) {
		r.cacheHeld = true
	}
	id, err := Session(&root, &p)
	if err != nil {
		return Snapshot{}, err
	}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: r.Namespace, Name: r.PodName}, &pod); err != nil {
		return Snapshot{}, err
	}
	if pod.UID != r.PodUID || r.PodName != Name(&p) || !ownedPod(&pod, &p) || pod.DeletionTimestamp != nil ||
		terminal(&pod) {
		return Snapshot{}, fmt.Errorf("worker Pod identity unavailable")
	}
	profile, err := profileFromPod(&pod)
	if err != nil || profile.Digest() != r.Profile.Digest() {
		return Snapshot{}, fmt.Errorf("worker fixed profile changed")
	}
	snapshot = Snapshot{
		admissionError: admissionErr,
		Network:        &root,
		Participant:    &p,
		Profile:        r.Profile,
		Paused:         paused(&root, &p),
	}
	if id.Worker != nil {
		if id.Worker.Pod.UID != r.PodUID || id.Worker.Pod.Name != r.PodName ||
			id.Worker.ProfileDigest != r.Profile.Digest() {
			return Snapshot{}, fmt.Errorf("worker durable binding differs")
		}
		snapshot.Shutdown = id.Worker.Shutdown
	} else if r.activated {
		return Snapshot{}, fmt.Errorf("activated worker binding disappeared")
	}
	control := copySnapshot(snapshot)
	r.lastControl = &control
	r.cacheBlocked = false
	if !controlAllows(snapshot) {
		r.cacheHeld = true
	}
	return r.decorate(snapshot), nil
}

// Authorize fresh-reads current membership, durable Pod identity and role prerequisites before sends.
func (r *Runtime) Authorize(ctx context.Context) error {
	snapshot, err := r.fresh(ctx)
	if err != nil {
		return err
	}
	return r.authorizeExpected(ctx, snapshot.Network, snapshot.Participant)
}

// authorizeExpected binds fresh callers to their observed policy/control view.
func (r *Runtime) authorizeExpected(
	ctx context.Context,
	root *api.StacksNetwork,
	p *api.StacksNetworkParticipant,
) error {
	return r.authorizeInvocation(ctx, Snapshot{Network: root, Participant: p}, false)
}

// authorizeInvocation permits transient outages only for this surviving applied baseline.
func (r *Runtime) authorizeInvocation(ctx context.Context, expected Snapshot, cached bool) error {
	snapshot, err := r.fresh(ctx)
	digest := ""
	if expected.Participant.Status.Admission != nil {
		digest = expected.Participant.Status.Admission.PolicyDigest
	}
	if cached && r.appliedSnapshot != nil {
		digest = r.appliedSnapshot.Participant.Status.Admission.PolicyDigest
	}
	if err != nil {
		if TransientAPIError(err) && r.cacheEligible(digest) {
			return nil
		}
		return err
	}
	if snapshot.Network.Generation != expected.Network.Generation ||
		snapshot.Participant.Generation != expected.Participant.Generation ||
		!reflect.DeepEqual(snapshot.Network.Status.GenesisRef, expected.Network.Status.GenesisRef) ||
		snapshot.Network.Status.GenesisDigest != expected.Network.Status.GenesisDigest ||
		!reflect.DeepEqual(snapshot.Participant.Status.Admission, expected.Participant.Status.Admission) {
		r.cacheHeld = true
		return fmt.Errorf("worker invocation policy or control changed")
	}
	if !controlAllows(snapshot) {
		r.cacheHeld = true
		return fmt.Errorf("current worker control withdraws submissions")
	}
	if snapshot.Participant.Status.Admission == nil ||
		foundation.Digest(
			snapshot.Participant.Status.Admission.Configuration,
		) != snapshot.Participant.Status.Admission.PolicyDigest {
		r.cacheHeld = true
		return fmt.Errorf("complete admitted worker policy unavailable")
	}
	if execution := snapshot.Participant.Status.Execution; execution == nil || execution.PodUID != r.PodUID ||
		execution.ProcessNonce != r.nonce ||
		execution.ProfileDigest != r.Profile.Digest() ||
		execution.Phase == api.WorkerPhaseFailed {
		r.cacheHeld = true
		return fmt.Errorf("worker process acknowledgement unavailable")
	}
	if r.Prerequisites == nil {
		return fmt.Errorf("role prerequisite validator is required")
	}
	if err := snapshot.admissionError; err != nil {
		if TransientAPIError(err) && r.cacheEligible(digest) {
			return nil
		}
		r.cacheHeld = true
		return err
	}
	if cached {
		applied, ok := r.cachedSnapshot(digest)
		if !ok {
			return fmt.Errorf("cached applied policy unavailable")
		}
		snapshot = applied
	}
	if err := r.Prerequisites(ctx, snapshot); err != nil {
		if TransientAPIError(err) && r.cacheEligible(digest) {
			return nil
		}
		r.cacheHeld = true
		return err
	}
	return nil
}

// Reconcile processes current desired state; errors never authorize a replacement or replay.
func (r *Runtime) Reconcile(ctx context.Context) (Result, error) {
	if r.Role == nil || r.Client == nil || r.PodUID == "" || r.ParticipantUID == "" || r.NetworkUID == "" {
		return Result{}, fmt.Errorf("worker runtime is incomplete")
	}
	if r.nonce == "" {
		normalized, err := r.Profile.Normalize()
		if err != nil {
			return Result{}, err
		}
		r.Profile = normalized
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			return Result{}, err
		}
		r.nonce = hex.EncodeToString(random[:])
	}
	snapshot, err := r.fresh(ctx)
	if err != nil {
		r.observePending(ctx)
		if !TransientAPIError(err) || r.appliedSnapshot == nil {
			return Result{RequeueAfter: time.Second}, err
		}
		cached, ok := r.cachedSnapshot(r.appliedSnapshot.Participant.Status.Admission.PolicyDigest)
		if !ok {
			return Result{RequeueAfter: time.Second}, err
		}
		snapshot = cached
	}
	p := snapshot.Participant
	root := snapshot.Network
	id, err := Session(root, p)
	if err != nil {
		return Result{RequeueAfter: time.Second}, err
	}
	if previous := p.Status.Execution; previous != nil && previous.ProcessNonce != "" &&
		previous.ProcessNonce != r.nonce {
		reusable := previous.PodUID != r.PodUID && previous.Phase == api.WorkerPhaseInactive &&
			(id.Worker == nil || id.Worker.Pod.UID == r.PodUID)
		if !reusable {
			return Result{Exit: true}, fmt.Errorf("worker process restart cannot recover a prior execution session")
		}
	}

	status := api.WorkerExecutionStatus{
		PodUID:             r.PodUID,
		ProcessNonce:       r.nonce,
		ProfileDigest:      r.Profile.Digest(),
		ObservedGeneration: p.Generation,
		NetworkGeneration:  root.Generation,
		ObservedAt:         metav1.NewTime(r.now().UTC().Truncate(time.Second)),
		Phase:              api.WorkerPhaseInactive,
	}
	previousEvidence := p.Status.Execution
	if r.pendingReport != nil {
		previousEvidence = r.pendingReport
	}
	if previous := previousEvidence; previous != nil && previous.PodUID == r.PodUID &&
		previous.ProcessNonce == r.nonce {
		status.AppliedPolicyDigest = previous.AppliedPolicyDigest
		status.Pending = previous.Pending
		status.Transactions = previous.Transactions.DeepCopy()
		status.AdministratorTransactions = previous.AdministratorTransactions.DeepCopy()
		status.PoX5 = previous.PoX5.DeepCopy()
		status.PoX4 = previous.PoX4.DeepCopy()
		status.Contracts = previous.Contracts.DeepCopy()
		status.Traffic = previous.Traffic.DeepCopy()
		status.Faucet = previous.Faucet.DeepCopy()
	}
	if id.Worker == nil {
		status.Reason = reasonAwaitingDurableBinding
		return Result{
			Exit:         stoppedReason(root, p, id) != "",
			RequeueAfter: 5 * time.Second,
		}, r.publish(ctx, p, &status)
	}
	r.activated = true
	if p.Status.Execution != nil && p.Status.Execution.PodUID == r.PodUID &&
		p.Status.Execution.ProcessNonce == r.nonce {
		r.processAcknowledged = true
	}
	if id.Worker.Disposal != nil {
		return Result{Exit: true}, nil
	}
	if r.pendingReport != nil {
		if err := r.publish(ctx, p, r.pendingReport); err != nil {
			if !TransientAPIError(err) {
				r.cacheHeld = true
				r.cacheBlocked = true
				return Result{RequeueAfter: time.Second}, err
			}
			cached, ok := r.cachedSnapshot(r.pendingReport.AppliedPolicyDigest)
			if !ok {
				return Result{RequeueAfter: time.Second}, err
			}
			snapshot = cached
		} else {
			r.pendingReport = nil
			return Result{RequeueAfter: r.pendingDelay}, nil
		}
	}
	if stoppedReason(root, p, id) != "" && id.Worker.Shutdown == nil {
		status.Phase, status.Reason = api.WorkerPhaseBlocked, reasonAwaitingShutdownRecord
		return Result{RequeueAfter: time.Second}, r.publish(ctx, p, &status)
	}
	if id.Worker.Shutdown != nil {
		if r.acknowledged != nil {
			return Result{RequeueAfter: time.Second}, r.publish(ctx, p, r.acknowledged)
		}
		status.Phase, status.Reason = api.WorkerPhaseDraining, string(id.Worker.Shutdown.Reason)
		status.NetworkGeneration = id.Worker.Shutdown.NetworkGeneration
		deadline := id.Worker.Shutdown.RequestedAt.Add(ShutdownBound)
		drainCtx, cancel := context.WithDeadline(ctx, deadline)
		outcome, drainErr := r.Role.Drain(drainCtx, snapshot)
		cancel()
		if outcome.Pending < 0 || outcome.Pending > 1000 {
			return Result{}, fmt.Errorf("role pending count exceeds bound")
		}
		status.Pending = outcome.Pending
		if outcome.Traffic != nil {
			status.Traffic = outcome.Traffic.DeepCopy()
		}
		if outcome.Faucet != nil {
			status.Faucet = outcome.Faucet.DeepCopy()
		}
		if outcome.PoX5 != nil {
			status.PoX5 = outcome.PoX5.DeepCopy()
		}
		if outcome.AdministratorTransactions != nil {
			status.AdministratorTransactions = outcome.AdministratorTransactions.DeepCopy()
		}
		if outcome.Contracts != nil {
			status.Contracts = outcome.Contracts.DeepCopy()
		}
		if outcome.PoX4 != nil {
			status.PoX4 = outcome.PoX4.DeepCopy()
		}
		if outcome.Transactions != nil {
			status.Transactions = outcome.Transactions.DeepCopy()
		}
		if drainErr == nil && outcome.Done || !time.Now().Before(deadline) {
			status.Phase = api.WorkerPhaseUnsettled
			if drainErr == nil && outcome.Done && outcome.Settled && outcome.Pending == 0 {
				status.Phase = api.WorkerPhaseSettled
			}
			r.acknowledged = status.DeepCopy()
		}
		return Result{RequeueAfter: time.Second}, r.publish(ctx, p, &status)
	}
	if previousEvidence != nil && previousEvidence.Phase == api.WorkerPhaseFailed {
		return Result{RequeueAfter: 5 * time.Second}, nil
	}
	// Persist this process identity before exposing an authorization callback to a role.
	if p.Status.Execution == nil || p.Status.Execution.ProcessNonce != r.nonce {
		status.Reason = reasonProcessBound
		if err := r.publish(ctx, p, &status); err != nil {
			return Result{RequeueAfter: time.Second}, err
		}
		r.processAcknowledged = true
	}
	r.stepUsedCached = snapshot.CachedApplied
	result, err := r.Role.Step(ctx, snapshot)
	if result.Pending < 0 || result.Pending > 1000 || len(result.Reason) > 128 ||
		len(result.AppliedPolicyDigest) > 128 {
		return Result{Exit: true}, fmt.Errorf("role execution summary exceeds bound")
	}
	status.Phase = api.WorkerPhaseActive
	if result.Blocked {
		status.Phase = api.WorkerPhaseBlocked
	}
	if snapshot.Paused {
		status.Phase = api.WorkerPhasePaused
	}
	if result.Failed {
		r.cacheHeld = true
		status.Phase = api.WorkerPhaseFailed
	}
	status.Reason = result.Reason
	status.Pending = result.Pending
	status.PoX4 = result.PoX4.DeepCopy()
	status.PoX5 = result.PoX5.DeepCopy()
	if result.AdministratorTransactions != nil {
		status.AdministratorTransactions = result.AdministratorTransactions.DeepCopy()
	}
	status.Contracts = result.Contracts.DeepCopy()
	status.Traffic = result.Traffic.DeepCopy()
	if result.Faucet != nil {
		status.Faucet = result.Faucet.DeepCopy()
	}
	if result.Transactions != nil {
		status.Transactions = result.Transactions.DeepCopy()
	}
	if previousEvidence != nil &&
		(!executionCountersAdvance(
			previousEvidence.Transactions,
			status.Transactions,
		) ||
			!executionCountersAdvance(
				previousEvidence.AdministratorTransactions,
				status.AdministratorTransactions,
			)) {
		r.cacheHeld = true
		return Result{Exit: true}, fmt.Errorf("baseline cumulative execution counters regressed")
	}
	if result.AppliedPolicyDigest != "" {
		if p.Status.Admission == nil || result.AppliedPolicyDigest != p.Status.Admission.PolicyDigest {
			previous := previousEvidence
			cachedPolicy := (snapshot.CachedApplied ||
				r.stepUsedCached) &&
				r.appliedSnapshot != nil &&
				r.appliedSnapshot.Participant.Status.Admission.PolicyDigest == result.AppliedPolicyDigest
			if !cachedPolicy &&
				(previous == nil ||
					previous.AppliedPolicyDigest != result.AppliedPolicyDigest ||
					(previous.Pending == 0 &&
						result.Pending == 0)) {
				return Result{Exit: true}, fmt.Errorf("role acknowledged an unadmitted policy")
			}
		}
		status.AppliedPolicyDigest = result.AppliedPolicyDigest
	}
	delay := result.RequeueAfter
	if err != nil {
		status.Phase, status.Reason = api.WorkerPhaseUnknown, reasonRoleObservationUnavailable
		delay = time.Second
	}
	if delay <= 0 {
		delay = 5 * time.Second
	}
	r.pendingReport = status.DeepCopy()
	r.pendingDelay = delay
	if err := r.publish(ctx, p, r.pendingReport); err != nil {
		if !TransientAPIError(err) {
			r.cacheHeld = true
			r.cacheBlocked = true
		}
		return Result{RequeueAfter: time.Second}, err
	}
	r.pendingReport = nil
	return Result{RequeueAfter: delay}, nil
}

// publish applies only execution and retains original acknowledgement timestamps on retries.
func (r *Runtime) publish(
	ctx context.Context,
	p *api.StacksNetworkParticipant,
	status *api.WorkerExecutionStatus,
) error {
	if previous := p.Status.Execution; previous != nil {
		stable := *status
		stable.ObservedAt = previous.ObservedAt
		if reflect.DeepEqual(*previous, stable) &&
			(status != r.pendingReport ||
				status.ObservedAt.Sub(previous.ObservedAt.Time) <
					time.Duration(foundation.ObservationPolicy().HeartbeatIntervalSeconds)*time.Second) {
			return nil
		}
	}
	if err := participantstatus.Apply(
		ctx,
		r.Client,
		p,
		api.ParticipantStatus{Execution: status},
		"stacks-network-worker-execution",
	); err != nil {
		r.observePending(ctx)
		return err
	}
	if r.lastControl != nil && r.lastControl.Participant.UID == p.UID {
		r.lastControl.Participant.Status.Execution = p.Status.Execution.DeepCopy()
		r.lastControl.Participant.ResourceVersion = p.ResourceVersion
	}
	return nil
}

// now resolves the observation clock without changing cached publication timestamps.
func (r *Runtime) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
