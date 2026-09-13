// Package networkruntime projects runtime observations through the aggregate's sole root writer.
package networkruntime

import (
	"context"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoincontrol"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reconciler allocates shared execution records and projects supported runtime facts.
// It never publishes root status itself or changes participant admission.
type Reconciler struct {
	// Client creates network-owned records and reads cached public observations.
	Client client.Client
	// Reader validates current record identities before publication.
	Reader client.Reader
	// Scheme establishes controller ownership on shared records.
	Scheme *runtime.Scheme
	// ActorKinds identifies installed actor domains; nil selects BitcoinNode only.
	ActorKinds []api.ParticipantKind
}

// Reconcile advances runtime allocation independently of unrelated composition errors.
func (r *Reconciler) Reconcile(ctx context.Context, root *api.StacksNetwork) (result ctrl.Result, reconcileErr error) {
	// Completion describes immutable bootstrap history and remains valid when
	// mutable membership or operation changes; it does not re-run the gates.
	if initialized := meta.FindStatusCondition(root.Status.Conditions, api.ConditionInitialized); initialized != nil && initialized.Status == metav1.ConditionTrue {
		initialized.ObservedGeneration = root.Generation
	}
	defer func() {
		if reconcileErr != nil {
			observationUnavailable(root, api.ReasonObservationUnavailable, "Current runtime observations are unavailable")
		}
	}()
	policy := foundation.ObservationPolicy()
	root.Status.ObservationPolicy = &policy
	failed := root.Status.Phase == api.NetworkPhaseFailed || meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionFailed)
	if failed && !meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionFailed) {
		set(root, api.ConditionFailed, metav1.ConditionTrue, reasonExperimentFailed, "A terminal network failure is latched; stop or recreate the network")
	}
	var participants api.StacksNetworkParticipantList
	if err := r.Client.List(ctx, &participants, client.InNamespace(root.Namespace), client.Limit(1001)); err != nil {
		set(root, api.ConditionRunning, metav1.ConditionUnknown, api.ReasonObservationUnavailable, "Current participant observations are unavailable")
		projectKnownOperation(root)
		set(root, api.ConditionOperational, metav1.ConditionUnknown, api.ReasonObservationUnavailable, "Current participant observations are unavailable")
		return ctrl.Result{}, err
	}
	// Cached lists use a non-pagination marker in Continue. The extra item detects
	// truncation without interpreting that marker as an API-server page token.
	if len(participants.Items) > 1000 {
		set(root, api.ConditionRunning, metav1.ConditionUnknown, reasonObservationIncomplete, "Participant inventory is incomplete")
		observationUnavailable(root, reasonObservationIncomplete, "Participant inventory is incomplete")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	workerPending, unstartedWorkers, err := r.projectWorkers(ctx, root, participants.Items)
	if err != nil {
		return ctrl.Result{}, err
	}
	failed = failed || meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionFailed)
	if root.DeletionTimestamp != nil {
		root.Status.Phase = api.NetworkPhaseDestroying
		set(root, api.ConditionRunning, metav1.ConditionFalse, reasonDeleting, "Network disposal is in progress")
		set(root, api.ConditionOperational, metav1.ConditionFalse, reasonDeleting, "Network disposal is in progress")
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	activated, terminated := false, true
	observed := map[string]bool{}
	for i := range participants.Items {
		p := &participants.Items[i]
		if p.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(p, root) {
			continue
		}
		observed[string(p.UID)] = true
		if managementKind(p.Spec.Kind) {
			id, err := stacksworker.Session(root, p)
			if (err != nil && !unstartedWorkers[p.UID]) || (err == nil && id.Worker != nil && (id.Worker.Disposal == nil || !id.Worker.Disposal.Terminated)) {
				terminated = false
			}
		}
		state := p.Status.Runtime
		if state != nil && len(state.WorkloadRefs) > 0 {
			activated = true
		}
		if r.actorKind(p.Spec.Kind) && (state == nil || !state.Terminated || state.ObservedGeneration != p.Generation) {
			terminated = false
		}
		if p.Spec.Kind == api.ParticipantBitcoinNode {
			control := p.Status.BitcoinControl
			if control == nil || !control.Terminated || control.ObservedGeneration != p.Generation || control.NetworkGeneration != root.Generation {
				terminated = false
			}
		}
	}
	for _, identity := range root.Status.Identities {
		if !identity.Removing && !observed[string(identity.UID)] {
			// Missing an allocated instance cannot establish that its processes exited.
			terminated = false
		}
	}
	if root.Spec.Operation == api.NetworkOperationStopped {
		root.Status.Phase = api.NetworkPhaseStopping
		if terminated {
			root.Status.Phase = api.NetworkPhaseStopped
		}
		set(root, api.ConditionRunning, metav1.ConditionFalse, string(root.Status.Phase), "Terminal shutdown requires acknowledgement from each activated workload")
		set(root, api.ConditionOperational, metav1.ConditionFalse, api.ReasonStopped, "Terminal shutdown is requested")
		if !terminated || workerPending {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, nil
	}
	if failed {
		root.Status.Phase = api.NetworkPhaseFailed
		set(root, api.ConditionRunning, metav1.ConditionFalse, reasonExperimentFailed, "A terminal network failure is latched")
		set(root, api.ConditionOperational, metav1.ConditionFalse, reasonExperimentFailed, "A terminal network failure is latched")
		return ctrl.Result{}, nil
	}
	if !meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionInitialized) {
		set(root, api.ConditionInitialized, metav1.ConditionFalse, api.ReasonBootstrapPending, "The complete frozen protocol gates have not completed")
	}
	set(root, api.ConditionRunning, metav1.ConditionFalse, reasonInitializing, "Full protocol initialization has not completed")
	projectKnownOperation(root)
	set(root, api.ConditionOperational, metav1.ConditionFalse, reasonInitializing, "Full protocol initialization has not completed")
	if root.Status.GenesisRef == nil {
		if root.Spec.Operation == api.NetworkOperationPaused && meta.IsStatusConditionTrue(root.Status.Conditions, common.ConditionResolved) {
			root.Status.Phase = api.NetworkPhaseUninitialized
		}
		return ctrl.Result{}, nil
	}
	refs, initialization, err := bitcoincontrol.EnsureRecords(ctx, r.Client, r.Reader, r.Scheme, root)
	// Preserve successful independent allocations if a later allocation needs another pass.
	root.Status.Bitcoin = &api.BitcoinRuntimeStatus{ExecutionRefs: refs, InitializationRef: initialization}
	if err != nil {
		set(root, api.ConditionBitcoinPrepared, metav1.ConditionUnknown, reasonRecordsUnavailable, "Bitcoin execution records could not be validated")
		return ctrl.Result{}, err
	}
	root.Status.Phase = api.NetworkPhaseInitializing
	projectKnownOperation(root)
	if root.Spec.Operation == api.NetworkOperationPaused {
		root.Status.Phase = api.NetworkPhaseUninitialized
		if activated {
			root.Status.Phase = api.NetworkPhasePausing
		}
		set(root, api.ConditionRunning, metav1.ConditionFalse, api.ReasonDesiredPause, "Network pause holds new managed activation and production")
	}
	if initialization == nil {
		return ctrl.Result{}, nil
	}
	var record bitcoin.BitcoinInitialization
	if err := r.observations().Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: initialization.Name}, &record); err != nil {
		set(root, api.ConditionBitcoinPrepared, metav1.ConditionUnknown, api.ReasonObservationUnavailable, "Bitcoin preparation observation is unavailable")
		return ctrl.Result{}, err
	}
	if record.UID != initialization.UID || record.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(&record, root) {
		set(root, api.ConditionBitcoinPrepared, metav1.ConditionUnknown, api.ReasonIdentityUnavailable, "Bitcoin preparation identity differs from the captured binding")
		observationUnavailable(root, api.ReasonIdentityUnavailable, "Bitcoin preparation identity differs from the captured binding")
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	if projectPreparation(root.DeepCopy(), &record) {
		current, err := r.currentInitialization(ctx, root, &record)
		if err != nil {
			return ctrl.Result{}, err
		}
		record = *current
	}
	if projectPreparation(root, &record) {
		return ctrl.Result{}, nil
	}
	gateFailed, err := r.reconcileGates(ctx, root, &record, participants.Items)
	if err != nil {
		return ctrl.Result{}, err
	}
	if gateFailed {
		set(root, api.ConditionRunning, metav1.ConditionFalse, reasonBootstrapFailed, "Frozen initialization requirements failed")
		set(root, api.ConditionOperational, metav1.ConditionFalse, reasonBootstrapFailed, "Frozen initialization requirements failed")
		return ctrl.Result{}, nil
	}
	if root.Spec.Operation == api.NetworkOperationPaused && activated {
		acknowledged, err := r.bitcoinPaused(ctx, root)
		if err != nil {
			return ctrl.Result{}, err
		}
		if acknowledged && workersPaused(root, participants.Items, time.Now()) {
			root.Status.Phase = api.NetworkPhasePaused
		}
	}

	if err := r.projectOperation(ctx, root, participants.Items, time.Now()); err != nil {
		set(root, api.ConditionOperational, metav1.ConditionUnknown, api.ReasonObservationUnavailable, "Current protocol observations are unavailable")
		return ctrl.Result{}, err
	}
	if workerPending || root.Status.Initialization != nil && !root.Status.Initialization.Completed {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
}

// set updates one aggregate-owned condition without changing its transition time on a stable result.
func set(root *api.StacksNetwork, kind string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&root.Status.Conditions, metav1.Condition{Type: kind, Status: status, Reason: reason, Message: message, ObservedGeneration: root.Generation})
}

// bitcoinPaused requires each retained control process to acknowledge the current pause.
// The acknowledgement excludes new managed sends, not independent mining or chain activity.
func (r *Reconciler) bitcoinPaused(ctx context.Context, root *api.StacksNetwork) (bool, error) {
	if root.Status.Bitcoin == nil || len(root.Status.Bitcoin.ExecutionRefs) == 0 {
		return false, nil
	}
	selected := map[string]bool{}
	for _, entry := range root.Spec.Participants {
		selected[entry.Name] = true
	}
	active := map[string]bool{}
	for _, id := range root.Status.Identities {
		if selected[id.Name] && !id.Removing {
			active[string(id.UID)] = true
		}
	}
	for _, ref := range root.Status.Bitcoin.ExecutionRefs {
		var record bitcoin.BitcoinExecution
		if err := r.observations().Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: ref.Name}, &record); err != nil {
			return false, err
		}
		if record.UID != ref.UID || record.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(&record, root) {
			return false, nil
		}
		if !active[string(record.Spec.Participant.UID)] && cleanDrain(record.Status) {
			continue
		}
		ack := record.Status.Control
		if record.Status.Armed != nil || ack == nil || ack.NetworkGeneration != root.Generation || ack.Operation != bitcoin.ControlPaused || ack.ProcessNonce == "" || ack.ObservedAt.IsZero() || ack.ObservedAt.After(ack.HeartbeatAt.Time) || ack.HeartbeatAt.After(time.Now()) || time.Since(ack.HeartbeatAt.Time) > foundation.ObservationFreshness() {
			return false, nil
		}
	}
	return true, nil
}

// cleanDrain recognizes terminal acknowledgement without treating abandoned uncertainty as quiescence.
func cleanDrain(status bitcoin.BitcoinExecutionStatus) bool {
	d := status.Drain
	if status.Armed != nil || d == nil || d.ProcessNonce == "" || d.Outcome != bitcoin.DrainDrained {
		return false
	}
	switch d.Reason {
	case api.ReasonParticipantRemoved, api.ReasonNetworkStopped, api.ReasonNetworkDeleting, api.ReasonNetworkFailed:
		return true
	default:
		return false
	}
}

// actorKind distinguishes installed domains from admitted-but-unsupported participant kinds.
func (r *Reconciler) actorKind(kind api.ParticipantKind) bool {
	if len(r.ActorKinds) == 0 {
		return kind == api.ParticipantBitcoinNode
	}
	for _, supported := range r.ActorKinds {
		if supported == kind {
			return true
		}
	}
	return false
}

// projectPreparation keeps the first Bitcoin gate distinct from full network initialization.
func projectPreparation(root *api.StacksNetwork, record *bitcoin.BitcoinInitialization) bool {
	switch record.Status.Reason {
	case api.ReasonPrepareBitcoinObservationDeadline, api.ReasonFrozenCeilingExceeded, api.ReasonPreparedChainRegressed:
		root.Status.Phase = api.NetworkPhaseFailed
		set(root, api.ConditionFailed, metav1.ConditionTrue, record.Status.Reason, "Frozen Bitcoin preparation requirements cannot be satisfied; recreate the network")
		set(root, api.ConditionRunning, metav1.ConditionFalse, record.Status.Reason, "Bitcoin initialization failed")
		set(root, api.ConditionOperational, metav1.ConditionFalse, record.Status.Reason, "Bitcoin initialization failed")
		return true
	}
	prepared := metav1.ConditionFalse
	reason, message := reasonPreparing, "Bitcoin wallets, maturity and initial cohort convergence are pending"
	if record.Status.PreparedAt != nil {
		prepared, reason, message = metav1.ConditionTrue, reasonPrepareBitcoinComplete, "The first frozen Bitcoin gate completed; later protocol gates remain pending"
	}
	set(root, api.ConditionBitcoinPrepared, prepared, reason, message)
	return false
}

// projectKnownOperation preserves historical initialization independently of current protocol reads.
func projectKnownOperation(root *api.StacksNetwork) {
	if root.DeletionTimestamp != nil || root.Spec.Operation != api.NetworkOperationRunning || meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionFailed) {
		return
	}
	if meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionInitialized) {
		root.Status.Phase = api.NetworkPhaseRunning
		set(root, api.ConditionRunning, metav1.ConditionTrue, api.ReasonRunning, "Initialization is complete and running operation is requested")
	}
}

// observationUnavailable preserves known lifecycle facts when current protocol reads fail.
func observationUnavailable(root *api.StacksNetwork, reason, message string) {
	status := metav1.ConditionFalse
	switch {
	case root.DeletionTimestamp != nil:
		reason, message = reasonDeleting, "Network disposal is in progress"
		root.Status.Phase = api.NetworkPhaseDestroying
	case root.Spec.Operation == api.NetworkOperationStopped:
		reason, message = api.ReasonStopped, "Terminal shutdown is requested"
	case meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionFailed):
		reason, message = reasonExperimentFailed, "A terminal network failure is latched"
		root.Status.Phase = api.NetworkPhaseFailed
	case root.Spec.Operation == api.NetworkOperationPaused:
		reason, message = api.ReasonDesiredPause, "Network pause holds managed production"
	case !meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionInitialized):
		reason, message = reasonInitializing, "Full protocol initialization has not completed"
	default:
		status = metav1.ConditionUnknown
	}
	if status == metav1.ConditionFalse {
		set(root, api.ConditionRunning, metav1.ConditionFalse, reason, message)
	} else {
		projectKnownOperation(root)
	}
	set(root, api.ConditionOperational, status, reason, message)
}

// observations reads projection-only public facts from the informer cache when installed.
// Callers still verify identity and original observation freshness; this is not dispatch authority.
func (r *Reconciler) observations() client.Reader {
	if r.Client != nil {
		return r.Client
	}
	return r.Reader
}
