package transactions

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	stacksv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

const ledgerFinalizer = "stacks.stacks.org/retain-transaction-ledger"

// Reconciler owns one exclusive account ledger; actors never receive its signing key.
type Reconciler struct {
	// Client writes only this capability's resources.
	client.Client
	// APIReader provides current ownership and admission reads.
	APIReader client.Reader
	// Profile binds the administrator-mounted account to one network/configuration.
	Profile AccountProfile
	// Signer creates signed bytes without network effects.
	Signer Signer
	// RPC observes the selected ingress and submits an authorized transaction once.
	RPC RPC
	// Now provides status and offered-cadence timestamps.
	Now func() time.Time
}

// SetupWithManager registers the child and parent watches; one worker serializes this account.
func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	if r.Profile.NetworkName == "" || r.Profile.Sender == "" || len(r.Profile.ConfigDigest) != 71 || r.Signer == nil || r.RPC == nil {
		return fmt.Errorf("transaction producer requires a bound account and ingress profile")
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	return ctrl.NewControllerManagedBy(manager).For(&stacksv1alpha1.StacksTransactionProduction{}).
		Watches(&networkv1alpha1.StacksNetwork{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []ctrl.Request {
			return []ctrl.Request{{NamespacedName: client.ObjectKeyFromObject(o)}}
		})).Complete(r)
}

// Reconcile never rebuilds or resends an outstanding transaction, including after restart.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	policy := &stacksv1alpha1.StacksTransactionProduction{}
	if err := r.APIReader.Get(ctx, request.NamespacedName, policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if policy.Spec.NetworkName != r.Profile.NetworkName || policy.Spec.Policy.Sender != r.Profile.Sender {
		return r.report(ctx, policy, "Waiting", "No designated signing profile for this network/account", 0)
	}
	parent := &networkv1alpha1.StacksNetwork{}
	err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: policy.Namespace, Name: policy.Spec.NetworkName}, parent)
	if apierrors.IsNotFound(err) || (err == nil && (string(parent.UID) != policy.Spec.NetworkUID || !parent.DeletionTimestamp.IsZero())) {
		return r.abandon(ctx, policy)
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if !metav1.IsControlledBy(policy, parent) || parent.Status.TransactionProductionUID != string(policy.UID) {
		return r.report(ctx, policy, "Waiting", "Ledger is not bound to the owning network", time.Second)
	}
	if !controllerutil.ContainsFinalizer(policy, ledgerFinalizer) {
		base := policy.DeepCopy()
		controllerutil.AddFinalizer(policy, ledgerFinalizer)
		return ctrl.Result{RequeueAfter: time.Millisecond}, r.Patch(ctx, policy, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
	}
	if policy.Status.Outstanding {
		// Evidence is read even while paused. Current declaration/identity remains required.
		target, err := r.admit(ctx, policy, parent)
		if err != nil {
			return r.reportPending(ctx, policy, "Ambiguous", "Outstanding transaction retained; current ingress identity is unavailable")
		}
		read, cancel := context.WithTimeout(ctx, 5*time.Second)
		inclusion, err := r.RPC.Inclusion(read, target.endpoint, policy.Status.TxID)
		cancel()
		if err != nil {
			return r.reportPending(ctx, policy, "Ambiguous", "Outstanding transaction retained; inclusion evidence is unavailable")
		}
		if !inclusion.Found {
			phase, message := "Pending", "Node acknowledged transaction; waiting for exact canonical inclusion"
			if !policy.Status.Accepted {
				phase, message = "Ambiguous", "Submission outcome is unknown; no resubmission or new nonce is authorized"
			}
			return r.reportPending(ctx, policy, phase, message)
		}
		if !inclusion.Success {
			return r.report(ctx, policy, "Blocked", "Exact transaction was included without a successful transfer result", 0)
		}
		base := policy.DeepCopy()
		policy.Status.Outstanding = false
		policy.Status.Confirmed++
		next := policy.Status.Nonce + 1
		policy.Status.NextNonce = &next
		policy.Status.LastBlockID = inclusion.BlockID
		now := metav1.NewTime(r.Now().UTC())
		policy.Status.LastConfirmedAt = &now
		policy.Status.Phase, policy.Status.Message = "Running", "Exact successful canonical inclusion accounted; finality is not implied"
		return ctrl.Result{RequeueAfter: time.Millisecond}, r.Status().Patch(ctx, policy, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
	}
	if !policy.DeletionTimestamp.IsZero() {
		return r.report(ctx, policy, "Blocked", "Ledger retained until owning environment deletion", 0)
	}
	if parent.Spec.Suspended || parent.Spec.StacksTransactionProduction == nil || parent.Spec.StacksTransactionProduction.Paused {
		return r.report(ctx, policy, "Paused", "New transfers are paused", 0)
	}
	target, err := r.admit(ctx, policy, parent)
	if err != nil {
		return r.report(ctx, policy, "Waiting", err.Error(), 2*time.Second)
	}
	if policy.Status.SubmittedAt != nil {
		remaining := policy.Status.SubmittedAt.Add(time.Duration(policy.Spec.Policy.IntervalSeconds) * time.Second).Sub(r.Now())
		if remaining > 0 {
			return r.report(ctx, policy, "Running", "Waiting for the next offered transfer interval", remaining)
		}
	}
	read, cancel := context.WithTimeout(ctx, 5*time.Second)
	account, err := r.RPC.Account(read, target.endpoint, policy.Spec.Policy.Sender)
	cancel()
	if err != nil {
		return r.report(ctx, policy, "Waiting", err.Error(), 2*time.Second)
	}
	if policy.Status.NextNonce != nil && account.Nonce != *policy.Status.NextNonce {
		return r.report(ctx, policy, "Blocked", "Ingress nonce differs from the ledger; waiting for an exact match; inspect lag, external use or reorganization", 2*time.Second)
	}
	if account.Nonce >= 9007199254740991 || account.Balance < uint64(policy.Spec.Policy.AmountMicroSTX+policy.Spec.Policy.FeeMicroSTX) {
		return r.report(ctx, policy, "Waiting", "Account nonce or available balance is outside the supported profile", 5*time.Second)
	}
	signing, cancel := context.WithTimeout(ctx, 5*time.Second)
	signed, err := r.Signer.Sign(signing, policy.Spec.Policy, account.Nonce)
	cancel()
	if err != nil {
		return r.report(ctx, policy, "Waiting", "Offline signing failed for the designated account", 5*time.Second)
	}
	if !validTransaction(signed) {
		return r.report(ctx, policy, "Blocked", "Signer returned an invalid transaction identity", 0)
	}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
		return ctrl.Result{}, err
	}
	if parent.Spec.Suspended || parent.Spec.StacksTransactionProduction == nil || parent.Spec.StacksTransactionProduction.Paused || !parent.DeletionTimestamp.IsZero() {
		return ctrl.Result{RequeueAfter: time.Millisecond}, nil
	}
	currentTarget, err := r.admit(ctx, policy, parent)
	if err != nil || currentTarget != target {
		return r.report(ctx, policy, "Waiting", "Ingress or policy changed during signing", time.Second)
	}
	if err := ctx.Err(); err != nil {
		return ctrl.Result{}, err
	}
	base := policy.DeepCopy()
	now := metav1.NewTime(r.Now().UTC())
	policy.Status.Outstanding, policy.Status.Accepted = true, false
	policy.Status.RejectionReason = ""
	policy.Status.TxID, policy.Status.Nonce = signed.TxID, account.Nonce
	policy.Status.NextNonce = &account.Nonce
	policy.Status.SubmittedAt = &now
	policy.Status.TargetUID, policy.Status.PodUID = target.actorUID, target.podUID
	policy.Status.Phase, policy.Status.Message, policy.Status.ObservedGeneration = "Ambiguous", "Signed transaction reserved before its only submission attempt", policy.Generation
	if err := r.Status().Patch(ctx, policy, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, err
	}
	// Loss of this response is recoverable only by observing this exact TxID; never by another POST.
	sending, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = r.RPC.Submit(sending, target.endpoint, signed)
	cancel()
	if err != nil {
		var rejection *submissionRejection
		if errors.As(err, &rejection) {
			base = policy.DeepCopy()
			policy.Status.RejectionReason = rejection.reason
			policy.Status.Phase, policy.Status.Message = "Blocked", rejectionMessage(rejection.reason)
			return ctrl.Result{RequeueAfter: 2 * time.Second}, r.Status().Patch(ctx, policy, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	base = policy.DeepCopy()
	policy.Status.Accepted = true
	policy.Status.Phase = "Pending"
	policy.Status.Message = "Exact transaction acknowledged; inclusion remains pending"
	return ctrl.Result{RequeueAfter: 2 * time.Second}, r.Status().Patch(ctx, policy, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// rejectionMessage reports the known response while preserving outstanding observation.
func rejectionMessage(reason string) string {
	return "Ingress rejected this submission (" + reason + "); nonce retained; exact inclusion remains under observation"
}

// reportPending preserves known rejection evidence when inclusion or ingress reads are unavailable.
func (r *Reconciler) reportPending(ctx context.Context, p *stacksv1alpha1.StacksTransactionProduction, phase, message string) (ctrl.Result, error) {
	if p.Status.RejectionReason != "" {
		phase, message = "Blocked", rejectionMessage(p.Status.RejectionReason)
	}
	return r.report(ctx, p, phase, message, 2*time.Second)
}

// report changes operational status without granting submission authority.
func (r *Reconciler) report(ctx context.Context, p *stacksv1alpha1.StacksTransactionProduction, phase, message string, after time.Duration) (ctrl.Result, error) {
	base := p.DeepCopy()
	p.Status.Phase, p.Status.Message, p.Status.ObservedGeneration = phase, message, p.Generation
	if reflect.DeepEqual(base.Status, p.Status) {
		return ctrl.Result{RequeueAfter: after}, nil
	}
	return ctrl.Result{RequeueAfter: after}, r.Status().Patch(ctx, p, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// abandon releases this finalizer during explicit environment disposal without claiming cancellation.
func (r *Reconciler) abandon(ctx context.Context, p *stacksv1alpha1.StacksTransactionProduction) (ctrl.Result, error) {
	_, _ = r.report(ctx, p, "Abandoned", "Owning environment removed; signed transactions may still execute", 0)
	current := &stacksv1alpha1.StacksTransactionProduction{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(p), current); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if current.UID != p.UID {
		return ctrl.Result{}, nil
	}
	base := current.DeepCopy()
	controllerutil.RemoveFinalizer(current, ledgerFinalizer)
	return ctrl.Result{}, r.Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}
