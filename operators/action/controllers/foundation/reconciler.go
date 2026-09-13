// Package foundation projects finite v1alpha2 action lifecycles from shared execution records.
package foundation

import (
	"context"
	"fmt"
	"time"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Reconciler owns lifecycle status and retention for one closed action kind.
type Reconciler struct {
	// Client writes only action metadata and status.
	Client client.Client
	// Reader performs current identity reads without a controller cache.
	Reader client.Reader
	// Concurrency bounds parallel reconciles across separate action objects.
	Concurrency int
	// Prototype selects one supported action type; it is never used as a read destination.
	Prototype client.Object
	// Now supplies lifecycle observation timestamps.
	Now func() time.Time
}

// SetupWithManager installs a resource-focused lifecycle writer; polling observes exact execution bindings.
func (r *Reconciler) SetupWithManager(m ctrl.Manager) error {
	object, _, err := r.object()
	if err != nil {
		return err
	}
	gvk, err := apiutil.GVKForObject(object, m.GetScheme())
	if err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(m).Named("action-foundation-" + gvk.Kind).For(object).WithOptions(controller.Options{MaxConcurrentReconciles: r.Concurrency}).Complete(r)
}

// object returns the fixed request kind and its common status subtree.
func (r *Reconciler) object() (client.Object, *action.BitcoinBlockGenerationStatus, error) {
	switch prototype := r.Prototype.(type) {
	case *action.BitcoinBlockGeneration:
		if prototype != nil {
			o := prototype.DeepCopy()
			return o, &o.Status, nil
		}
	case *action.BitcoinReorganization:
		if prototype != nil {
			o := prototype.DeepCopy()
			return o, &o.Status.BitcoinBlockGenerationStatus, nil
		}
	}
	return nil, nil, fmt.Errorf("unsupported or nil finite action prototype %T", r.Prototype)
}

// Reconcile publishes durable receipts before releasing an execution reservation.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	object, status, err := r.object()
	if err != nil {
		return ctrl.Result{}, err
	}
	gvk, err := apiutil.GVKForObject(object, r.Client.Scheme())
	if err != nil {
		return ctrl.Result{}, err
	}
	if err = r.Reader.Get(ctx, request.NamespacedName, object); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}
	before := object.DeepCopyObject().(client.Object)
	uid, target, timeout := desired(object)
	if !controllerutil.ContainsFinalizer(object, action.CleanupFinalizer) && object.GetDeletionTimestamp() == nil && !action.IsTerminalPhase(status.Phase) {
		controllerutil.AddFinalizer(object, action.CleanupFinalizer)
		return ctrl.Result{RequeueAfter: time.Second}, r.Client.Patch(ctx, object, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	expiry := object.GetCreationTimestamp().Add(timeout)
	status.ExpiresAt = &metav1.Time{Time: expiry}
	status.ObservedGeneration = object.GetGeneration()
	record, available, err := r.execution(ctx, object, status, uid, target)
	if err != nil {
		return ctrl.Result{}, err
	}
	root := &api.StacksNetwork{}
	rootErr := r.Reader.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: "network"}, root)
	if rootErr != nil && !apierrors.IsNotFound(rootErr) {
		return ctrl.Result{}, rootErr
	}
	gone := apierrors.IsNotFound(rootErr) || root.UID != uid || root.DeletionTimestamp != nil
	bound := record != nil && record.Status.Action != nil && record.Status.Action.Request.UID == object.GetUID() && record.Status.Action.Request.Kind == gvk.Kind && record.Status.Action.Request.Name == object.GetName()
	if bound {
		state := record.Status.Action
		copyEvidence(object, status, record, state)
		if gone {
			finish(status, action.PhaseInconclusive, action.ReasonEnvironmentDisposed, now)
		} else {
			project(status, state, record, now, object.GetDeletionTimestamp())
		}
	} else if !action.IsTerminalPhase(status.Phase) {
		if status.AdmittedAt != nil {
			finish(status, action.PhaseInconclusive, action.ReasonExecutionUnavailable, now)
		} else if object.GetDeletionTimestamp() != nil || !now.Before(expiry) {
			if available {
				finish(status, action.PhaseFailed, action.ReasonNoDispatch, now)
			} else {
				finish(status, action.PhaseInconclusive, action.ReasonAdmissionUnavailable, now)
			}
		} else {
			status.Phase = action.PhasePending
		}
	}
	condition(status, action.ConditionAdmitted, status.AdmittedAt != nil, action.ReasonAdmissionObserved, now)
	if !action.IsTerminalPhase(status.Phase) || meta.FindStatusCondition(status.Conditions, action.ConditionProgressing) == nil {
		condition(status, action.ConditionProgressing, !action.IsTerminalPhase(status.Phase), string(status.Phase), now)
	}
	condition(status, action.ConditionEffectObserved, status.Phase == action.PhaseCompleted, action.ReasonReceiptEvidence, now)
	clean := !bound && available
	if bound {
		s := record.Status.Action
		clean = record.Status.Armed == nil && (s.Reorganization == nil || !s.InvalidationAcknowledged || s.CleanupAcknowledged) && !s.CleanupUnsafe
	}
	condition(status, action.ConditionCleanupComplete, clean, action.ReasonCleanupEvidence, now)
	if !equality.Semantic.DeepEqual(statusOf(before), statusOf(object)) {
		object.SetManagedFields(nil)
		object.GetObjectKind().SetGroupVersionKind(gvk)
		// A fresh minimal object preserves metadata preconditions without claiming fetched metadata.
		payload, _, err := r.object()
		if err != nil {
			return ctrl.Result{}, err
		}
		payload.SetName(object.GetName())
		payload.SetNamespace(object.GetNamespace())
		payload.SetUID(object.GetUID())
		payload.SetResourceVersion(object.GetResourceVersion())
		payload.GetObjectKind().SetGroupVersionKind(gvk)
		switch p := payload.(type) {
		case *action.BitcoinBlockGeneration:
			p.Status = object.(*action.BitcoinBlockGeneration).Status
		case *action.BitcoinReorganization:
			p.Status = object.(*action.BitcoinReorganization).Status
		}
		if err := r.Client.Status().Patch(ctx, payload, client.Apply, client.FieldOwner("stacks-action-foundation-"+gvk.Kind), client.ForceOwnership); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	if action.IsTerminalPhase(status.Phase) && (gone || !bound && available) && controllerutil.ContainsFinalizer(object, action.CleanupFinalizer) {
		previous := object.DeepCopyObject().(client.Object)
		controllerutil.RemoveFinalizer(object, action.CleanupFinalizer)
		return ctrl.Result{}, r.Client.Patch(ctx, object, client.MergeFromWithOptions(previous, client.MergeFromWithOptimisticLock{}))
	}
	if action.IsTerminalPhase(status.Phase) && (gone || !bound && available) {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

// desired reads the immutable finite request envelope.
func desired(object client.Object) (types.UID, string, time.Duration) {
	switch o := object.(type) {
	case *action.BitcoinBlockGeneration:
		return o.Spec.NetworkUID, o.Spec.BitcoinNodeRef.Name, o.Spec.Timeout.Duration
	case *action.BitcoinReorganization:
		return o.Spec.NetworkUID, o.Spec.BitcoinNodeRef.Name, o.Spec.Timeout.Duration
	}
	return "", "", 0
}

// statusOf returns complete owned status for idempotence comparisons.
func statusOf(object client.Object) any {
	switch o := object.(type) {
	case *action.BitcoinBlockGeneration:
		return o.Status
	case *action.BitcoinReorganization:
		return o.Status
	}
	return nil
}

// execution locates only root-pinned execution identities; missing admitted records never prove no send.
func (r *Reconciler) execution(ctx context.Context, object client.Object, status *action.BitcoinBlockGenerationStatus, uid types.UID, target string) (*bitcoin.BitcoinExecution, bool, error) {
	if status.AdmittedExecution != nil {
		record := &bitcoin.BitcoinExecution{}
		err := r.Reader.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: status.AdmittedExecution.Name}, record)
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if record.UID != status.AdmittedExecution.UID || record.Spec.NetworkUID != uid {
			return nil, false, nil
		}
		return record, true, nil
	}
	root := &api.StacksNetwork{}
	err := r.Reader.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: "network"}, root)
	if apierrors.IsNotFound(err) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if root.UID != uid {
		return nil, true, nil
	}
	if root.Status.Bitcoin == nil {
		return nil, true, nil
	}
	if len(root.Status.Bitcoin.ExecutionRefs) > 1000 {
		return nil, false, fmt.Errorf("execution inventory exceeds bound")
	}
	for _, ref := range root.Status.Bitcoin.ExecutionRefs {
		record := &bitcoin.BitcoinExecution{}
		if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: ref.Name}, record); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, false, nil
			}
			return nil, false, err
		}
		if record.UID != ref.UID || record.Spec.NetworkUID != uid || !metav1.IsControlledBy(record, root) {
			return nil, false, nil
		}
		if record.Status.Action != nil && record.Status.Action.Request.UID == object.GetUID() {
			return record, true, nil
		}
		p := &api.StacksNetworkParticipant{}
		if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: record.Spec.Participant.Name}, p); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, false, err
		}
		if p.UID == record.Spec.Participant.UID && p.Spec.ParticipantName == target {
			return record, true, nil
		}
	}
	return nil, true, nil
}

// copyEvidence preserves attribution independently of a frozen outcome.
func copyEvidence(object client.Object, status *action.BitcoinBlockGenerationStatus, record *bitcoin.BitcoinExecution, s *bitcoin.BitcoinActionReservation) {
	status.AdmittedExecution = &common.Binding{Kind: bitcoin.KindBitcoinExecution, Name: record.Name, UID: record.UID}
	status.AdmittedAt = s.AdmittedAt.DeepCopy()
	status.StartedAt = s.StartedAt.DeepCopy()
	status.ExpiresAt = s.ExpiresAt.DeepCopy()
	status.AdmittedNetwork = &s.Network
	status.AdmittedTarget = &s.Target
	status.AdmittedPolicy = &action.PolicyIdentity{UID: string(record.UID), Generation: record.Generation, Digest: s.Runtime.PolicyDigest}
	status.CorrelationID = s.CorrelationID
	status.BlocksGenerated = s.BlocksGenerated
	status.LastBlockHash = s.LastBlockHash
	status.LastDispatchID = s.LastDispatchID
	if o, ok := object.(*action.BitcoinReorganization); ok {
		o.Status.OriginalChain = s.OriginalChain.DeepCopy()
		o.Status.ForkParent = s.ForkParent.DeepCopy()
		o.Status.InvalidatedHash = s.InvalidatedHash
		o.Status.ReplacementBlockHashes = append([]string(nil), s.ReplacementBlockHashes...)
		o.Status.FinalChain = s.FinalChain.DeepCopy()
		o.Status.InvalidationAcknowledged = s.InvalidationAcknowledged
		o.Status.CleanupAcknowledged = s.CleanupAcknowledged
	}
}

// project freezes outcomes while allowing later receipts and compensation evidence to be copied.
func project(status *action.BitcoinBlockGenerationStatus, s *bitcoin.BitcoinActionReservation, record *bitcoin.BitcoinExecution, now time.Time, deleted *metav1.Time) {
	if action.IsTerminalPhase(status.Phase) {
		return
	}
	if s.EffectUncertain || s.CleanupUnsafe {
		finish(status, action.PhaseInconclusive, action.ReasonEffectUncertain, now)
		return
	}
	complete := s.Generation != nil && s.BlocksGenerated == s.Generation.Count || s.Reorganization != nil && s.FinalChain != nil && s.CleanupAcknowledged
	if complete && record.Status.Armed == nil && s.StopReason == "" && (s.Reorganization != nil || s.LastCompletedAt != nil && !s.LastCompletedAt.After(s.ExpiresAt.Time) && (deleted == nil || !s.LastCompletedAt.After(deleted.Time))) {
		finish(status, action.PhaseCompleted, action.ReasonReceiptsComplete, now)
		return
	}
	if (!now.Before(s.ExpiresAt.Time) || deleted != nil) && record.Status.Armed != nil {
		if s.Reorganization != nil && record.Status.Armed.Method == bitcoin.RPCReconsiderBlock && now.Before(s.ExpiresAt.Add(30*time.Second)) {
			status.Phase = action.PhaseRecovering
			return
		}
		finish(status, action.PhaseInconclusive, action.ReasonEffectUncertain, now)
		return
	}
	if s.Reorganization != nil && s.InvalidationAcknowledged && !s.CleanupAcknowledged && !now.Before(s.ExpiresAt.Add(30*time.Second)) {
		finish(status, action.PhaseInconclusive, action.ReasonCleanupDeadlineExceeded, now)
		return
	}
	if s.StopReason != "" && record.Status.Armed == nil {
		if s.Reorganization != nil && s.InvalidationAcknowledged && !s.CleanupAcknowledged {
			status.Phase = action.PhaseRecovering
			return
		}
		phase := action.PhaseFailed
		if s.StopReason == action.ReasonIdentityDiverged && s.StartedAt != nil {
			phase = action.PhaseInconclusive
		}
		finish(status, phase, s.StopReason, now)
		return
	}
	status.Phase = action.PhaseAdmitted
	if s.StartedAt != nil {
		status.Phase = action.PhaseActive
	}
}

// finish records the first terminal observation and its bounded reason.
func finish(s *action.BitcoinBlockGenerationStatus, phase action.Phase, reason string, now time.Time) {
	if action.IsTerminalPhase(s.Phase) {
		return
	}
	s.Phase = phase
	s.FinishedAt = &metav1.Time{Time: now}
	condition(s, action.ConditionProgressing, false, reason, now)
}

// condition updates keyed lifecycle facts without refreshing unchanged transition timestamps.
func condition(s *action.BitcoinBlockGenerationStatus, kind string, value bool, reason string, now time.Time) {
	status := metav1.ConditionFalse
	if value {
		status = metav1.ConditionTrue
	}
	if reason == "" {
		reason = action.ReasonPending
	}
	meta.SetStatusCondition(&s.Conditions, metav1.Condition{Type: kind, Status: status, ObservedGeneration: s.ObservedGeneration, Reason: reason, Message: reason, LastTransitionTime: metav1.NewTime(now)})
}
