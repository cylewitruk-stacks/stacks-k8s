package faucetrequest

import (
	"context"
	"errors"
	"strconv"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reconciler owns only immutable request admission and evidence-based phase projection.
type Reconciler struct {
	// Client writes UID/revision-checked status and supplies the watch-routing cache.
	Client client.Client
	// Reader performs uncached identity and capacity reads.
	Reader client.Reader
	// Now injects deadline observations in tests.
	Now func() time.Time
	// uncertain retains possibly granted slots until a confirmed decision settles the write.
	uncertain map[types.UID]types.UID
}

// now reads the configured wall clock.
func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Reconcile never replaces an already granted admission, including after expiry or worker loss.
func (r *Reconciler) Reconcile(ctx context.Context, key ctrl.Request) (ctrl.Result, error) {
	request := &stacks.StacksFaucetRequest{}
	if err := r.Reader.Get(ctx, key.NamespacedName, request); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	admission := request.Status.Admission.DeepCopy()
	if admission == nil || admission.Decision == stacks.FaucetDecisionPending {
		var err error
		admission, err = r.admit(ctx, request)
		if err != nil {
			return ctrl.Result{}, err
		}
		// A lengthy identity/capacity read cannot extend the original dispatch window.
		if admission.Decision == stacks.FaucetDecisionAdmitted {
			deadline, _ := Deadline(request)
			if !r.now().Before(deadline) {
				admission = baseAdmission(request, deadline, stacks.FaucetDecisionExpired, reasonDeadlineBeforeAdmission)
			}
		}
	}
	projected := request.DeepCopy()
	projected.Status.Admission = admission
	phase, reason := ProjectPhase(projected, r.now())
	conditions := append([]metav1.Condition(nil), request.Status.Conditions...)
	status := metav1.ConditionFalse
	if phase == stacks.FaucetCompleted {
		status = metav1.ConditionTrue
	}
	if phase == stacks.FaucetPending || phase == stacks.FaucetSubmitted || phase == stacks.FaucetInconclusive {
		status = metav1.ConditionUnknown
	}
	meta.SetStatusCondition(&conditions, metav1.Condition{Type: stacks.ConditionCompleted, Status: status, ObservedGeneration: request.Generation, Reason: reason, Message: reason, LastTransitionTime: metav1.NewTime(r.now())})
	owned := stacks.FaucetRequestStatus{Admission: admission, Phase: phase, Conditions: conditions}
	if !equality.Semantic.DeepEqual(request.Status.Admission, owned.Admission) || request.Status.Phase != phase || !equality.Semantic.DeepEqual(request.Status.Conditions, conditions) {
		if admission != nil && admission.Decision == stacks.FaucetDecisionAdmitted && admission.Worker != nil {
			if r.uncertain == nil {
				r.uncertain = map[types.UID]types.UID{}
			}
			r.uncertain[request.UID] = admission.Worker.UID
		}
		if err := ApplyStatus(ctx, r.Client, request, owned, AdmissionManager); err != nil {
			return ctrl.Result{}, err
		}
		delete(r.uncertain, request.UID)
	}
	if phase == stacks.FaucetCompleted || phase == stacks.FaucetRejected || phase == stacks.FaucetExpired {
		return ctrl.Result{}, nil
	}
	delay := 5 * time.Second
	if deadline, err := Deadline(request); err == nil && deadline.After(r.now()) && deadline.Sub(r.now()) < delay {
		delay = deadline.Sub(r.now())
	}
	return ctrl.Result{RequeueAfter: delay}, nil
}

// baseAdmission retains the creation-derived deadline in every controller decision.
func baseAdmission(request *stacks.StacksFaucetRequest, deadline time.Time, decision stacks.FaucetDecision, reason string) *stacks.FaucetAdmission {
	return &stacks.FaucetAdmission{Decision: decision, Reason: reason, ExpiresAt: deadline.UTC().Format(time.RFC3339Nano), NetworkUID: request.Spec.NetworkUID}
}

// admit grants only a current participant/Pod/account/ingress identity with available capacity.
func (r *Reconciler) admit(ctx context.Context, request *stacks.StacksFaucetRequest) (*stacks.FaucetAdmission, error) {
	deadline, err := Deadline(request)
	if err != nil {
		return nil, err
	}
	decision := func(state stacks.FaucetDecision, reason string) (*stacks.FaucetAdmission, error) {
		return baseAdmission(request, deadline, state, reason), nil
	}
	if !r.now().Before(deadline) {
		return decision(stacks.FaucetDecisionExpired, reasonDeadlineBeforeAdmission)
	}
	if request.DeletionTimestamp != nil {
		return decision(stacks.FaucetDecisionExpired, reasonDeletedBeforeAdmission)
	}
	amount, err := strconv.ParseUint(string(request.Spec.AmountMicroSTX), 10, 64)
	if err != nil || amount == 0 {
		return decision(stacks.FaucetDecisionRejected, api.ReasonInvalidAmount)
	}
	root := &api.StacksNetwork{}
	if err = r.Reader.Get(ctx, client.ObjectKey{Namespace: request.Namespace, Name: "network"}, root); err != nil {
		if apierrors.IsNotFound(err) {
			return decision(stacks.FaucetDecisionPending, reasonNetworkUnavailable)
		}
		return nil, err
	}
	if root.UID != request.Spec.NetworkUID {
		return decision(stacks.FaucetDecisionRejected, reasonNetworkIdentityChanged)
	}
	if root.DeletionTimestamp != nil || root.Spec.Operation == api.NetworkOperationStopped || root.Status.Phase == api.NetworkPhaseFailed || meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionFailed) {
		return decision(stacks.FaucetDecisionRejected, api.ReasonNetworkStopped)
	}
	selected := false
	for _, entry := range root.Spec.Participants {
		selected = selected || entry.Name == request.Spec.FaucetRef.Name && entry.Kind == api.ParticipantStacksFaucet
	}
	if !selected {
		for _, id := range root.Status.Identities {
			if id.Name == request.Spec.FaucetRef.Name {
				return decision(stacks.FaucetDecisionRejected, reasonFaucetRemoved)
			}
		}
		return decision(stacks.FaucetDecisionPending, reasonFaucetUnavailable)
	}
	p := &api.StacksNetworkParticipant{}
	name := foundation.ParticipantName(string(root.UID), request.Spec.FaucetRef.Name)
	if err = r.Reader.Get(ctx, client.ObjectKey{Namespace: request.Namespace, Name: name}, p); err != nil {
		if apierrors.IsNotFound(err) {
			return decision(stacks.FaucetDecisionPending, reasonFaucetUnavailable)
		}
		return nil, err
	}
	if p.Spec.Kind != api.ParticipantStacksFaucet || p.Spec.ParticipantName != request.Spec.FaucetRef.Name || p.DeletionTimestamp != nil {
		return decision(stacks.FaucetDecisionRejected, reasonFaucetIdentityChanged)
	}
	session, err := stacksworker.Session(root, p)
	if err != nil || session.Removing {
		return decision(stacks.FaucetDecisionRejected, reasonFaucetIdentityChanged)
	}
	if p.Status.Admission == nil || p.Status.Admission.Configuration.StacksFaucet == nil {
		return decision(stacks.FaucetDecisionPending, reasonFaucetAdmissionUnavailable)
	}
	if err = foundation.ValidateParticipantAdmission(ctx, r.Reader, root, p); err != nil {
		return decision(stacks.FaucetDecisionPending, reasonFaucetDependenciesUnavailable)
	}
	policy := p.Status.Admission.Configuration.StacksFaucet
	if policy.AccountRef == nil || policy.TargetNodeRef == nil || policy.MaxRequestMicroSTX == nil {
		return decision(stacks.FaucetDecisionPending, reasonFaucetPolicyUnavailable)
	}
	limit, err := strconv.ParseUint(string(*policy.MaxRequestMicroSTX), 10, 64)
	if err != nil {
		return decision(stacks.FaucetDecisionRejected, reasonInvalidFaucetLimit)
	}
	if amount > limit {
		return decision(stacks.FaucetDecisionRejected, reasonRequestLimitExceeded)
	}
	if session.Worker == nil || session.Worker.Shutdown != nil || session.Worker.Disposal != nil {
		return decision(stacks.FaucetDecisionPending, reasonWorkerUnavailable)
	}
	execution := p.Status.Execution
	if execution == nil || execution.PodUID != session.Worker.Pod.UID || execution.ProfileDigest != session.Worker.ProfileDigest || execution.ProcessNonce == "" || execution.Phase == api.WorkerPhaseInactive || execution.Phase == api.WorkerPhaseFailed || execution.Phase == api.WorkerPhaseSettled || execution.Phase == api.WorkerPhaseUnsettled {
		return decision(stacks.FaucetDecisionPending, reasonWorkerUnavailable)
	}
	pod := &corev1.Pod{}
	if err = r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: session.Worker.Pod.Name}, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return decision(stacks.FaucetDecisionPending, reasonWorkerUnavailable)
		}
		return nil, err
	}
	if pod.UID != session.Worker.Pod.UID || !metav1.IsControlledBy(pod, p) || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return decision(stacks.FaucetDecisionPending, reasonWorkerUnavailable)
	}
	source, err := admittedBinding(p, stacks.KindStacksAccount, policy.AccountRef.Name)
	if err != nil {
		return decision(stacks.FaucetDecisionPending, reasonSourceAccountUnavailable)
	}
	target, err := admittedBinding(p, api.KindStacksNetworkParticipant, foundation.ParticipantName(string(root.UID), policy.TargetNodeRef.Name))
	if err != nil {
		return decision(stacks.FaucetDecisionPending, reasonTargetUnavailable)
	}
	destination := ""
	var destinationBinding *stacks.FaucetBinding
	if request.Spec.Destination.Address != nil {
		destination = *request.Spec.Destination.Address
	}
	if request.Spec.Destination.AccountRef != nil {
		if destination != "" {
			return decision(stacks.FaucetDecisionRejected, reasonInvalidDestination)
		}
		account := &stacks.StacksAccount{}
		if err = r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: request.Spec.Destination.AccountRef.Name}, account); err != nil {
			if apierrors.IsNotFound(err) {
				return decision(stacks.FaucetDecisionPending, reasonDestinationUnavailable)
			}
			return nil, err
		}
		if account.DeletionTimestamp != nil || account.Status.Identity == nil || account.Status.ObservedGeneration != account.Generation || !meta.IsStatusConditionTrue(account.Status.Conditions, common.ConditionResolved) {
			return decision(stacks.FaucetDecisionPending, reasonDestinationUnavailable)
		}
		destination = account.Status.Identity.Address
		destinationBinding = &stacks.FaucetBinding{Kind: stacks.KindStacksAccount, Name: account.Name, UID: account.UID, Fingerprint: account.Status.Digest}
	}
	if !ValidAddress(destination) {
		return decision(stacks.FaucetDecisionRejected, reasonInvalidDestination)
	}
	count, err := r.activeCount(ctx, p.Namespace, session.Worker.Pod.UID)
	if err != nil {
		return nil, err
	}
	if execution.Faucet != nil {
		count += int(execution.Faucet.Orphaned)
		if int(execution.Faucet.Active) > count {
			count = int(execution.Faucet.Active)
		}
	}
	if count >= Capacity {
		return decision(stacks.FaucetDecisionRejected, reasonCapacityExceeded)
	}
	admitted := baseAdmission(request, deadline, stacks.FaucetDecisionAdmitted, api.ReasonWorkerBound)
	admitted.Faucet = &stacks.FaucetBinding{Kind: api.KindStacksNetworkParticipant, Name: p.Name, UID: p.UID}
	admitted.Worker = &stacks.FaucetBinding{Kind: common.KindPod, Name: pod.Name, UID: pod.UID}
	admitted.ProfileDigest = session.Worker.ProfileDigest
	admitted.SourceAccount, admitted.Target, admitted.DestinationAccount = source, target, destinationBinding
	admitted.Destination = destination
	admitted.AmountMicroSTX = request.Spec.AmountMicroSTX
	admitted.FeeMicroSTX = common.Amount(strconv.FormatUint(FeeMicroSTX, 10))
	return admitted, nil
}

// admittedBinding copies one exact immutable public dependency from complete participant admission.
func admittedBinding(p *api.StacksNetworkParticipant, kind, name string) (*stacks.FaucetBinding, error) {
	if p.Status.Admission != nil {
		for _, b := range p.Status.Admission.Dependencies {
			if b.Kind == kind && b.Name == name && b.UID != "" {
				return &stacks.FaucetBinding{Kind: b.Kind, Name: b.Name, UID: b.UID, Fingerprint: b.Fingerprint}, nil
			}
		}
	}
	return nil, errors.New("admitted faucet dependency unavailable")
}
