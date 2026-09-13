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
	if admission == nil || admission.Decision == "Pending" {
		var err error
		admission, err = r.admit(ctx, request)
		if err != nil {
			return ctrl.Result{}, err
		}
		// A lengthy identity/capacity read cannot extend the original dispatch window.
		if admission.Decision == "Admitted" {
			deadline, _ := Deadline(request)
			if !r.now().Before(deadline) {
				admission = baseAdmission(request, deadline, "Expired", "DeadlineBeforeAdmission")
			}
		}
	}
	projected := request.DeepCopy()
	projected.Status.Admission = admission
	phase, reason := ProjectPhase(projected, r.now())
	conditions := append([]metav1.Condition(nil), request.Status.Conditions...)
	status := metav1.ConditionFalse
	if phase == "Completed" {
		status = metav1.ConditionTrue
	}
	if phase == "Pending" || phase == "Submitted" || phase == "Inconclusive" {
		status = metav1.ConditionUnknown
	}
	meta.SetStatusCondition(&conditions, metav1.Condition{Type: "Completed", Status: status, ObservedGeneration: request.Generation, Reason: reason, Message: reason, LastTransitionTime: metav1.NewTime(r.now())})
	owned := stacks.FaucetRequestStatus{Admission: admission, Phase: phase, Conditions: conditions}
	if !equality.Semantic.DeepEqual(request.Status.Admission, owned.Admission) || request.Status.Phase != phase || !equality.Semantic.DeepEqual(request.Status.Conditions, conditions) {
		if admission != nil && admission.Decision == "Admitted" && admission.Worker != nil {
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
	if phase == "Completed" || phase == "Rejected" || phase == "Expired" {
		return ctrl.Result{}, nil
	}
	delay := 5 * time.Second
	if deadline, err := Deadline(request); err == nil && deadline.After(r.now()) && deadline.Sub(r.now()) < delay {
		delay = deadline.Sub(r.now())
	}
	return ctrl.Result{RequeueAfter: delay}, nil
}

// baseAdmission retains the creation-derived deadline in every controller decision.
func baseAdmission(request *stacks.StacksFaucetRequest, deadline time.Time, decision, reason string) *stacks.FaucetAdmission {
	return &stacks.FaucetAdmission{Decision: decision, Reason: reason, ExpiresAt: deadline.UTC().Format(time.RFC3339Nano), NetworkUID: request.Spec.NetworkUID}
}

// admit grants only a current participant/Pod/account/ingress identity with available capacity.
func (r *Reconciler) admit(ctx context.Context, request *stacks.StacksFaucetRequest) (*stacks.FaucetAdmission, error) {
	deadline, err := Deadline(request)
	if err != nil {
		return nil, err
	}
	decision := func(state, reason string) (*stacks.FaucetAdmission, error) {
		return baseAdmission(request, deadline, state, reason), nil
	}
	if !r.now().Before(deadline) {
		return decision("Expired", "DeadlineBeforeAdmission")
	}
	if request.DeletionTimestamp != nil {
		return decision("Expired", "DeletedBeforeAdmission")
	}
	amount, err := strconv.ParseUint(string(request.Spec.AmountMicroSTX), 10, 64)
	if err != nil || amount == 0 {
		return decision("Rejected", "InvalidAmount")
	}
	root := &api.StacksNetwork{}
	if err = r.Reader.Get(ctx, client.ObjectKey{Namespace: request.Namespace, Name: "network"}, root); err != nil {
		if apierrors.IsNotFound(err) {
			return decision("Pending", "NetworkUnavailable")
		}
		return nil, err
	}
	if root.UID != request.Spec.NetworkUID {
		return decision("Rejected", "NetworkIdentityChanged")
	}
	if root.DeletionTimestamp != nil || root.Spec.Operation == "Stopped" || root.Status.Phase == "Failed" || meta.IsStatusConditionTrue(root.Status.Conditions, "Failed") {
		return decision("Rejected", "NetworkStopped")
	}
	selected := false
	for _, entry := range root.Spec.Participants {
		selected = selected || entry.Name == request.Spec.FaucetRef.Name && entry.Kind == "StacksFaucet"
	}
	if !selected {
		for _, id := range root.Status.Identities {
			if id.Name == request.Spec.FaucetRef.Name {
				return decision("Rejected", "FaucetRemoved")
			}
		}
		return decision("Pending", "FaucetUnavailable")
	}
	p := &api.StacksNetworkParticipant{}
	name := foundation.ParticipantName(string(root.UID), request.Spec.FaucetRef.Name)
	if err = r.Reader.Get(ctx, client.ObjectKey{Namespace: request.Namespace, Name: name}, p); err != nil {
		if apierrors.IsNotFound(err) {
			return decision("Pending", "FaucetUnavailable")
		}
		return nil, err
	}
	if p.Spec.Kind != "StacksFaucet" || p.Spec.ParticipantName != request.Spec.FaucetRef.Name || p.DeletionTimestamp != nil {
		return decision("Rejected", "FaucetIdentityChanged")
	}
	session, err := stacksworker.Session(root, p)
	if err != nil || session.Removing {
		return decision("Rejected", "FaucetIdentityChanged")
	}
	if p.Status.Admission == nil || p.Status.Admission.Configuration.StacksFaucet == nil {
		return decision("Pending", "FaucetAdmissionUnavailable")
	}
	if err = foundation.ValidateParticipantAdmission(ctx, r.Reader, root, p); err != nil {
		return decision("Pending", "FaucetDependenciesUnavailable")
	}
	policy := p.Status.Admission.Configuration.StacksFaucet
	if policy.AccountRef == nil || policy.TargetNodeRef == nil || policy.MaxRequestMicroSTX == nil {
		return decision("Pending", "FaucetPolicyUnavailable")
	}
	limit, err := strconv.ParseUint(string(*policy.MaxRequestMicroSTX), 10, 64)
	if err != nil {
		return decision("Rejected", "InvalidFaucetLimit")
	}
	if amount > limit {
		return decision("Rejected", "RequestLimitExceeded")
	}
	if session.Worker == nil || session.Worker.Shutdown != nil || session.Worker.Disposal != nil {
		return decision("Pending", "WorkerUnavailable")
	}
	execution := p.Status.Execution
	if execution == nil || execution.PodUID != session.Worker.Pod.UID || execution.ProfileDigest != session.Worker.ProfileDigest || execution.ProcessNonce == "" || execution.Phase == "Inactive" || execution.Phase == "Failed" || execution.Phase == "Settled" || execution.Phase == "Unsettled" {
		return decision("Pending", "WorkerUnavailable")
	}
	pod := &corev1.Pod{}
	if err = r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: session.Worker.Pod.Name}, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return decision("Pending", "WorkerUnavailable")
		}
		return nil, err
	}
	if pod.UID != session.Worker.Pod.UID || !metav1.IsControlledBy(pod, p) || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return decision("Pending", "WorkerUnavailable")
	}
	source, err := admittedBinding(p, "StacksAccount", policy.AccountRef.Name)
	if err != nil {
		return decision("Pending", "SourceAccountUnavailable")
	}
	target, err := admittedBinding(p, "StacksNetworkParticipant", foundation.ParticipantName(string(root.UID), policy.TargetNodeRef.Name))
	if err != nil {
		return decision("Pending", "TargetUnavailable")
	}
	destination := ""
	var destinationBinding *stacks.FaucetBinding
	if request.Spec.Destination.Address != nil {
		destination = *request.Spec.Destination.Address
	}
	if request.Spec.Destination.AccountRef != nil {
		if destination != "" {
			return decision("Rejected", "InvalidDestination")
		}
		account := &stacks.StacksAccount{}
		if err = r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: request.Spec.Destination.AccountRef.Name}, account); err != nil {
			if apierrors.IsNotFound(err) {
				return decision("Pending", "DestinationUnavailable")
			}
			return nil, err
		}
		if account.DeletionTimestamp != nil || account.Status.Identity == nil || account.Status.ObservedGeneration != account.Generation || !meta.IsStatusConditionTrue(account.Status.Conditions, "Resolved") {
			return decision("Pending", "DestinationUnavailable")
		}
		destination = account.Status.Identity.Address
		destinationBinding = &stacks.FaucetBinding{Kind: "StacksAccount", Name: account.Name, UID: account.UID, Fingerprint: account.Status.Digest}
	}
	if !ValidAddress(destination) {
		return decision("Rejected", "InvalidDestination")
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
		return decision("Rejected", "CapacityExceeded")
	}
	admitted := baseAdmission(request, deadline, "Admitted", "WorkerBound")
	admitted.Faucet = &stacks.FaucetBinding{Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID}
	admitted.Worker = &stacks.FaucetBinding{Kind: "Pod", Name: pod.Name, UID: pod.UID}
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
