package bitcoincontrol

import (
	"context"
	"fmt"
	"sort"
	"time"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// actionView keeps the two finite request kinds behind one reservation protocol.
type actionView struct {
	object         client.Object
	kind           action.Kind
	status         *action.BitcoinBlockGenerationStatus
	generation     *action.BitcoinBlockGenerationSpec
	reorganization *action.BitcoinReorganizationSpec
}

// actionObject returns the closed finite kind catalog.
func actionObject(kind action.Kind) (client.Object, error) {
	switch kind {
	case action.KindBitcoinBlockGeneration:
		return &action.BitcoinBlockGeneration{}, nil
	case action.KindBitcoinReorganization:
		return &action.BitcoinReorganization{}, nil
	}
	return nil, fmt.Errorf("unsupported finite action kind %q", kind)
}

// actionFields decodes the closed request union without losing its immutable kind.
func actionFields(object client.Object) (actionView, error) {
	switch a := object.(type) {
	case *action.BitcoinBlockGeneration:
		if a != nil {
			return actionView{object: a, kind: action.KindBitcoinBlockGeneration, status: &a.Status, generation: &a.Spec}, nil
		}
	case *action.BitcoinReorganization:
		if a != nil {
			return actionView{object: a, kind: action.KindBitcoinReorganization, status: &a.Status.BitcoinBlockGenerationStatus, reorganization: &a.Spec}, nil
		}
	}
	return actionView{}, fmt.Errorf("unsupported or nil finite action object %T", object)
}

// reference captures the kind selected by actionFields together with current request metadata.
func (v actionView) reference() common.Binding {
	return common.Binding{Kind: string(v.kind), Name: v.object.GetName(), UID: v.object.GetUID()}
}

// desired exposes common public eligibility without creating generic mutation inputs.
func (v actionView) desired() (string, string, string, time.Duration) {
	if v.generation != nil {
		return string(v.generation.NetworkUID), v.generation.BitcoinNodeRef.Name, v.generation.Address, v.generation.Timeout.Duration
	}
	return string(v.reorganization.NetworkUID), v.reorganization.BitcoinNodeRef.Name, v.reorganization.Address, v.reorganization.Timeout.Duration
}

// selectAction chooses oldest eligible finite work without allowing unavailable requests to reserve a target.
func (w *Worker) selectAction(ctx context.Context, a admitted, record *bitcoin.BitcoinExecution) (bool, error) {
	if !w.Input.ActionsEnabled && !w.Input.ReorganizationEnabled {
		return false, nil
	}
	if record.Status.Reservation != nil && *record.Status.Reservation != objectref.BitcoinInitialization(a.initialization) {
		return true, nil
	}
	if record.Status.Action != nil || record.Status.Armed != nil {
		return true, nil
	}
	if err := foundation.ValidateAdmissionEligibility(ctx, w.Reader, a.participant); err != nil {
		return false, err
	}
	if err := foundation.ValidatePublicParticipantAdmission(ctx, w.Reader, a.root, a.participant); err != nil {
		return false, err
	}
	queue := []actionView{}
	if w.Input.ActionsEnabled {
		var list action.BitcoinBlockGenerationList
		if err := w.Reader.List(ctx, &list, client.InNamespace(record.Namespace), client.Limit(65)); err != nil {
			return false, err
		}
		if len(list.Items) > 64 || list.Continue != "" {
			return false, fmt.Errorf("finite generation inventory incomplete")
		}
		for i := range list.Items {
			view, err := actionFields(&list.Items[i])
			if err != nil {
				return false, err
			}
			queue = append(queue, view)
		}
	}
	if w.Input.ReorganizationEnabled {
		var list action.BitcoinReorganizationList
		if err := w.Reader.List(ctx, &list, client.InNamespace(record.Namespace), client.Limit(65)); err != nil {
			return false, err
		}
		if len(list.Items) > 64 || list.Continue != "" {
			return false, fmt.Errorf("reorganization inventory incomplete")
		}
		for i := range list.Items {
			view, err := actionFields(&list.Items[i])
			if err != nil {
				return false, err
			}
			queue = append(queue, view)
		}
	}
	sort.Slice(queue, func(i, j int) bool {
		x, y := queue[i].object.GetCreationTimestamp(), queue[j].object.GetCreationTimestamp()
		if !x.Equal(&y) {
			return x.Before(&y)
		}
		return queue[i].object.GetUID() < queue[j].object.GetUID()
	})
	for _, candidate := range queue {
		network, target, address, timeout := candidate.desired()
		if network != string(a.root.UID) || target != a.participant.Spec.ParticipantName || candidate.object.GetDeletionTimestamp() != nil || action.IsTerminalPhase(candidate.status.Phase) || candidate.status.AdmittedAt != nil || !controllerutil.ContainsFinalizer(candidate.object, action.CleanupFinalizer) || timeout <= 0 || timeout > 10*time.Minute || !w.Now().Before(candidate.object.GetCreationTimestamp().Add(timeout)) {
			continue
		}
		if candidate.generation != nil && validateActionCadence(candidate.generation.Cadence, candidate.generation.Count) != nil {
			continue
		}
		if err := w.RPC.Check(ctx, a.target.Endpoint, address); err != nil {
			continue
		}
		height, _, err := w.chain(ctx, a.target.Endpoint)
		if err != nil {
			return false, err
		}
		if err := w.actionCeiling(ctx, a, height); err != nil {
			continue
		}
		now := metav1.NewTime(w.Now().UTC())
		reservation := &bitcoin.BitcoinActionReservation{Request: candidate.reference(), Generation: candidate.generation.DeepCopy(), Reorganization: candidate.reorganization.DeepCopy(), Network: action.NetworkIdentity{Name: a.root.Name, UID: string(a.root.UID), ObservedGeneration: a.root.Generation}, Target: actionTarget(a), Runtime: a.target, AdmittedAt: now, ExpiresAt: metav1.NewTime(candidate.object.GetCreationTimestamp().Add(timeout)), CorrelationID: candidate.object.GetLabels()[action.CorrelationIDLabel]}
		if candidate.reorganization != nil {
			if err := w.captureReorganization(ctx, a, reservation); err != nil {
				continue
			}
		}
		fresh, err := w.authorize(ctx, record)
		if err != nil || !equality.Semantic.DeepEqual(a.target, fresh.target) {
			return false, fmt.Errorf("finite target changed before reservation")
		}
		currentRequest, err := objectref.Fresh(candidate.object, w.Client.Scheme())
		if err != nil {
			return false, err
		}
		if err := w.Reader.Get(ctx, client.ObjectKeyFromObject(candidate.object), currentRequest); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return false, err
		}
		currentView, err := actionFields(currentRequest)
		if err != nil {
			return false, err
		}
		if currentRequest.GetUID() != candidate.object.GetUID() || !equality.Semantic.DeepEqual(currentView.generation, candidate.generation) || !equality.Semantic.DeepEqual(currentView.reorganization, candidate.reorganization) || currentRequest.GetDeletionTimestamp() != nil || action.IsTerminalPhase(currentView.status.Phase) || currentView.status.AdmittedAt != nil || !controllerutil.ContainsFinalizer(currentRequest, action.CleanupFinalizer) || !w.Now().Before(reservation.ExpiresAt.Time) {
			continue
		}
		record.Status.Action = reservation
		record.Status.Reservation = reservation.Request.DeepCopy()
		return true, w.Client.Status().Update(ctx, record)
	}
	return false, nil
}

// actionTarget publishes the exact immutable configuration, credentials and actor process binding.
func actionTarget(a admitted) action.TargetIdentity {
	out := action.TargetIdentity{APIVersion: api.GroupVersion.String(), Kind: api.KindStacksNetworkParticipant, Name: a.participant.Name, UID: string(a.participant.UID), SpecDigest: a.target.PolicyDigest, ConfigDigest: a.participant.Status.Runtime.ConfigurationDigest, PodUID: string(a.target.Pod.UID), ContainerID: a.target.ContainerID, Configuration: a.target.Configuration, Credentials: a.target.Credentials}
	if a.pod != nil {
		out.Revision = a.pod.Labels[appsv1.ControllerRevisionHashLabelKey]
		for _, process := range a.pod.Status.ContainerStatuses {
			if process.Name == api.ContainerBitcoin && process.ContainerID == a.target.ContainerID {
				out.RuntimeImageID = process.ImageID
			}
		}
	}
	for _, ref := range a.participant.Status.Runtime.WorkloadRefs {
		if ref.Kind == common.KindStatefulSet {
			out.StatefulSetUID = string(ref.UID)
		}
	}
	return out
}

// actionCeiling applies the same immutable initialization gates to every finite generation path.
func (w *Worker) actionCeiling(ctx context.Context, a admitted, height int64) error {
	if a.root.Status.Initialization != nil && a.root.Status.Initialization.Completed {
		return baselineCompleted(ctx, w.Reader, a.root, a.initialization)
	}
	authority, err := currentGate(ctx, w.Reader, a.root, a.initialization)
	if err != nil {
		return err
	}
	if height >= authority.gate.BitcoinCeiling {
		return fmt.Errorf("finite generation reached initialization ceiling")
	}
	ready, err := advancementReady(ctx, w.Reader, a.root, a.initialization, authority, height, w.Now())
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("initialization advancement unavailable")
	}
	return nil
}

// readAction returns only the exact original request incarnation and immutable spec.
func (w *Worker) readAction(ctx context.Context, record *bitcoin.BitcoinExecution) (actionView, bool, error) {
	state := record.Status.Action
	object, err := actionObject(action.Kind(state.Request.Kind))
	if err != nil {
		return actionView{}, false, err
	}
	err = w.Reader.Get(ctx, client.ObjectKey{Namespace: record.Namespace, Name: state.Request.Name}, object)
	if apierrors.IsNotFound(err) {
		return actionView{}, false, nil
	}
	if err != nil {
		return actionView{}, false, err
	}
	view, err := actionFields(object)
	if err != nil {
		return actionView{}, false, err
	}
	same := object.GetUID() == state.Request.UID && equality.Semantic.DeepEqual(view.generation, state.Generation) && equality.Semantic.DeepEqual(view.reorganization, state.Reorganization)
	return view, same, nil
}

// stepAction retains exclusion until known receipts and terminal lifecycle status agree.
func (w *Worker) stepAction(ctx context.Context, record *bitcoin.BitcoinExecution) error {
	state := record.Status.Action
	view, same, err := w.readAction(ctx, record)
	if err != nil {
		return err
	}
	root := &api.StacksNetwork{}
	if err := w.Reader.Get(ctx, client.ObjectKey{Namespace: record.Namespace, Name: "network"}, root); err != nil {
		return err
	}
	if root.UID != record.Spec.NetworkUID || root.DeletionTimestamp != nil {
		_, err := w.stop(ctx, record)
		return err
	}
	if !failed(root) && root.Spec.Operation != api.NetworkOperationStopped && (record.Status.Observation == nil || w.Now().Sub(record.Status.Observation.ObservedAt.Time) >= 5*time.Second) {
		a, err := w.authorizeObservation(ctx, record)
		if err == nil {
			observation, _, observeErr := w.observe(ctx, a, record.DeepCopy())
			if observeErr == nil {
				fresh, freshErr := w.authorizeObservation(ctx, record)
				if freshErr == nil && equality.Semantic.DeepEqual(fresh.target, a.target) {
					record.Status.Observation = observation
					return w.Client.Status().Update(ctx, record)
				}
			}
		}
	}
	if record.Status.Armed != nil {
		w.mu.Lock()
		active := w.active
		w.mu.Unlock()
		if !active && !state.EffectUncertain {
			state.EffectUncertain = true
			return w.Client.Status().Update(ctx, record)
		}
		if failed(root) || root.Spec.Operation == api.NetworkOperationStopped {
			_, err := w.stop(ctx, record)
			return err
		}
		return nil
	}
	clean := state.Reorganization == nil || !state.InvalidationAcknowledged || state.CleanupAcknowledged
	acknowledged := same && action.IsTerminalPhase(view.status.Phase) && view.status.LastDispatchID == state.LastDispatchID && view.status.BlocksGenerated == state.BlocksGenerated
	if same && state.Reorganization != nil {
		acknowledged = acknowledged && view.object.(*action.BitcoinReorganization).Status.CleanupAcknowledged == state.CleanupAcknowledged
	}
	if clean && (!same || acknowledged) {
		record.Status.Action = nil
		record.Status.Reservation = nil
		return w.Client.Status().Update(ctx, record)
	}
	stop := ""
	switch {
	case state.Generation != nil && !w.Input.ActionsEnabled || state.Reorganization != nil && !w.Input.ReorganizationEnabled:
		stop = reasonCapabilityDisabled
	case !same || view.object.GetDeletionTimestamp() != nil:
		stop = action.ReasonActionCancelled
	case root.Spec.Operation == api.NetworkOperationPaused && state.Reorganization != nil && state.InvalidationAcknowledged && !state.CleanupAcknowledged:
		stop = reasonNetworkPaused
	case failed(root) || root.Spec.Operation == api.NetworkOperationStopped:
		stop = api.ReasonNetworkStopped
	case same && action.IsTerminalPhase(view.status.Phase):
		stop = action.ReasonEffectUncertain
	case !w.Now().Before(state.ExpiresAt.Time) && !actionFinished(state):
		stop = reasonDeadlineExceeded
	}
	if stop != "" && state.StopReason == "" {
		state.StopReason = stop
		return w.Client.Status().Update(ctx, record)
	}
	if state.CleanupUnsafe {
		if failed(root) || root.Spec.Operation == api.NetworkOperationStopped {
			_, err := w.stop(ctx, record)
			return err
		}
		return nil
	}
	if root.Spec.Operation == api.NetworkOperationPaused && (state.Reorganization == nil || !state.InvalidationAcknowledged || state.CleanupAcknowledged) {
		if _, err := w.acknowledgePause(ctx, record); err != nil {
			return err
		}
		return w.observePaused(ctx, record)
	}
	if state.Reorganization != nil {
		return w.stepReorganization(ctx, record, view, same)
	}
	if state.StopReason != "" || state.EffectUncertain || actionFinished(state) {
		return nil
	}
	if !same || !actionAdmissionAcknowledged(view, record) {
		return nil
	}
	admitted, err := w.authorize(ctx, record)
	if err != nil {
		return nil
	}
	if !equality.Semantic.DeepEqual(admitted.target, state.Runtime) {
		return w.stopAction(ctx, record, action.ReasonIdentityDiverged, false)
	}
	if state.NextDispatchAt != nil && w.Now().Before(state.NextDispatchAt.Time) {
		return nil
	}
	operation := bitcoin.BitcoinArmedRPC{Method: bitcoin.RPCGenerate, Action: state.Request.DeepCopy(), Address: state.Generation.Address}
	return w.arm(ctx, record, admitted, operation)
}

// actionFinished identifies complete receipts without asserting chain adoption.
func actionFinished(state *bitcoin.BitcoinActionReservation) bool {
	if state.Generation != nil {
		return state.BlocksGenerated >= state.Generation.Count
	}
	return state.FinalChain != nil
}

// actionAdmissionAcknowledged requires the lifecycle to retain exact admission before the first send.
func actionAdmissionAcknowledged(view actionView, record *bitcoin.BitcoinExecution) bool {
	state := record.Status.Action
	status := view.status
	return controllerutil.ContainsFinalizer(view.object, action.CleanupFinalizer) && status.AdmittedAt != nil && status.AdmittedAt.Equal(&state.AdmittedAt) && status.AdmittedExecution != nil && *status.AdmittedExecution == objectref.BitcoinExecution(record) && status.AdmittedTarget != nil && *status.AdmittedTarget == state.Target && status.AdmittedNetwork != nil && *status.AdmittedNetwork == state.Network
}

// stopAction withdraws further action work without changing any previous outcome or RPC evidence.
func (w *Worker) stopAction(ctx context.Context, record *bitcoin.BitcoinExecution, reason string, unsafe bool) error {
	before := record.DeepCopy()
	if record.Status.Action.StopReason == "" {
		record.Status.Action.StopReason = reason
	}
	record.Status.Action.CleanupUnsafe = record.Status.Action.CleanupUnsafe || unsafe
	if equality.Semantic.DeepEqual(before.Status, record.Status) {
		return nil
	}
	return w.Client.Status().Update(ctx, record)
}
