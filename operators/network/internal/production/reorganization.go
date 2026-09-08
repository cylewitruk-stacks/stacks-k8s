package production

import (
	"context"
	"fmt"
	"reflect"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// reserveReorganization prepares immutable local-chain identity without side effects.
func (r *Reconciler) reserveReorganization(ctx context.Context, p *bitcoinv1.BitcoinProductionTarget, n *networkv1.StacksNetwork, target admittedTarget, a *actionv1.BitcoinReorganization) (bool, error) {
	if a.Spec.NetworkRef.Name != n.Name || a.Spec.BitcoinNodeRef.Name != target.identity.Name || a.Status.AdmittedAt != nil || actionv1.IsTerminalPhase(a.Status.Phase) || !a.DeletionTimestamp.IsZero() || !r.Now().Before(a.CreationTimestamp.Add(a.Spec.Timeout.Duration)) {
		return false, nil
	}
	rpc := r.RPC.(ReorganizationRPC)
	if rpc.Check(ctx, target.endpoint, a.Spec.Address) != nil || rpc.CheckTips(ctx, target.endpoint) != nil {
		return false, nil
	}
	tip, err := rpc.Tip(ctx, target.endpoint)
	if err != nil || tip.Height < int64(a.Spec.Depth) {
		return false, nil
	}
	ancestorHash, err := rpc.HashAt(ctx, target.endpoint, tip.Height-int64(a.Spec.Depth))
	if err != nil {
		return false, nil
	}
	ancestor, err := rpc.Header(ctx, target.endpoint, ancestorHash)
	if err != nil || ancestor.Height != tip.Height-int64(a.Spec.Depth) {
		return false, nil
	}
	pivot, err := rpc.HashAt(ctx, target.endpoint, ancestor.Height+1)
	if err != nil {
		return false, nil
	}
	// Walk the short admitted suffix rather than inferring ancestry from height.
	walk := tip
	for height := tip.Height; height > ancestor.Height; height-- {
		if walk.Height != height || walk.Chainwork <= ancestor.Chainwork {
			return false, nil
		}
		if height == ancestor.Height+1 && walk.Hash != pivot {
			return false, nil
		}
		walk, err = rpc.Header(ctx, target.endpoint, walk.PreviousBlockHash)
		if err != nil {
			return false, nil
		}
	}
	if walk != ancestor {
		return false, nil
	}
	latest, err := rpc.Tip(ctx, target.endpoint)
	if err != nil || latest != tip {
		return false, nil
	}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(n), n); err != nil {
		return false, err
	}
	current, err := r.admit(ctx, p, n)
	if err != nil || current != target || n.Spec.Suspended || !n.DeletionTimestamp.IsZero() {
		return false, nil
	}
	fresh := &actionv1.BitcoinReorganization{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(a), fresh); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	if fresh.UID != a.UID || !reflect.DeepEqual(fresh.Spec, a.Spec) || fresh.Status.AdmittedAt != nil || actionv1.IsTerminalPhase(fresh.Status.Phase) || !fresh.DeletionTimestamp.IsZero() || !r.Now().Before(fresh.CreationTimestamp.Add(fresh.Spec.Timeout.Duration)) {
		return false, nil
	}
	before := p.DeepCopy()
	offer, reason, err := r.opportunity(ctx, p, n)
	if err != nil {
		return false, err
	}
	consumeReserved(p, offer, reason)
	p.Status.Reorganization = &bitcoinv1.ReorganizationReservation{ActionReservation: bitcoinv1.ActionReservation{Name: a.Name, UID: string(a.UID), Generation: a.Generation, AdmittedAt: metav1.NewTime(r.Now().UTC()), ExpiresAt: metav1.NewTime(a.CreationTimestamp.Add(a.Spec.Timeout.Duration)), Network: actionv1.NetworkIdentity{Name: n.Name, UID: string(n.UID), ObservedGeneration: n.Generation}, Target: target.identity, Policy: actionv1.PolicyIdentity{UID: string(p.UID), Generation: p.Generation, Digest: r.ConfigDigest}, CorrelationID: a.Labels["actions.stacks.org/correlation-id"]}, Spec: a.Spec, OriginalChain: tip, ForkParent: ancestor, InvalidatedHash: pivot}
	p.Status.Phase, p.Status.Message = "Reserved", "Local suffix replacement reserved the shared executor"
	return true, r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

// reconcileReorganization advances known receipts and compensation under one retained reservation.
func (r *Reconciler) reconcileReorganization(ctx context.Context, p *bitcoinv1.BitcoinProductionTarget, n *networkv1.StacksNetwork) (ctrl.Result, error) {
	record := p.Status.Reorganization
	if !r.ReorganizationEnabled {
		return r.report(ctx, p, "Blocked", "Restore the reorganization-enabled executor to resolve retained work", 0)
	}
	a := &actionv1.BitcoinReorganization{}
	err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: record.Name}, a)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	same := err == nil && string(a.UID) == record.UID && reflect.DeepEqual(a.Spec, record.Spec)
	if p.Status.DispatchState == "Armed" {
		if phase := r.collectors.phase(p.Status.DispatchID, p.UID); phase != "" {
			return r.report(ctx, p, phase, "Collecting or accounting the reorganization receipt", time.Second)
		}
		if !record.EffectUncertain {
			before := p.DeepCopy()
			record.EffectUncertain = true
			return ctrl.Result{RequeueAfter: time.Second}, r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
		}
		return r.report(ctx, p, "Blocked", "Unresolved reorganization call forbids further mutation, including cleanup", time.Second)
	}
	clean := record.CleanupAcknowledged || record.StartedAt == nil
	acknowledged := same && actionv1.IsTerminalPhase(a.Status.Phase) && a.Status.LastDispatchID == record.LastDispatchID && a.Status.BlocksGenerated == record.BlocksGenerated && a.Status.CleanupAcknowledged == record.CleanupAcknowledged
	if clean && (!same || acknowledged) {
		before := p.DeepCopy()
		p.Status.Reorganization = nil
		p.Status.Phase, p.Status.Message = "Running", "Reorganization released; latest baseline will be evaluated"
		return ctrl.Result{RequeueAfter: time.Millisecond}, r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	if record.EffectUncertain || record.CleanupUnsafe {
		return r.report(ctx, p, "Blocked", "Retained reorganization cleanup is unresolved", time.Second)
	}
	stop := ""
	switch {
	case !same || !a.DeletionTimestamp.IsZero():
		stop = "ActionCancelled"
	case !p.DeletionTimestamp.IsZero():
		stop = "IdentityDiverged"
	case record.BlocksGenerated < record.Spec.Depth+1 && !r.Now().Before(record.ExpiresAt.Time):
		stop = "DeadlineExceeded"
	case actionv1.IsTerminalPhase(a.Status.Phase):
		stop = "EffectUncertain"
	}
	if record.StopReason == "" && stop != "" {
		return r.stopReorganization(ctx, p, stop, false)
	}
	if clean && record.StopReason != "" || record.FinalChain != nil {
		return r.report(ctx, p, "Reserved", "Waiting for terminal reorganization receipt acknowledgement", time.Second)
	}
	if same && (!controllerutil.ContainsFinalizer(a, actionv1.CleanupFinalizer) || a.Status.AdmittedAt == nil || a.Status.AdmittedTarget == nil || *a.Status.AdmittedTarget != record.Target || a.Status.AdmittedPolicy == nil || a.Status.AdmittedPolicy.UID != string(p.UID)) {
		return r.report(ctx, p, "Reserved", "Waiting for durable reorganization admission", time.Second)
	}
	cleanup := record.InvalidationAcknowledged && !record.CleanupAcknowledged && (record.StopReason != "" || record.BlocksGenerated == record.Spec.Depth+1)
	if cleanup && !r.Now().Before(record.ExpiresAt.Add(30*time.Second)) {
		return r.stopReorganization(ctx, p, "CleanupUncertain", true)
	}
	target, err := r.admitTarget(ctx, p, n, false)
	if err != nil {
		if !r.Now().Before(record.ExpiresAt.Add(30 * time.Second)) {
			if record.CleanupAcknowledged {
				return r.stopReorganization(ctx, p, "IdentityDiverged", false)
			}
			if record.StartedAt != nil {
				return r.stopReorganization(ctx, p, "CleanupUncertain", true)
			}
		}
		return r.report(ctx, p, "Reserved", "Waiting for exact admitted reorganization target", time.Second)
	}
	if target.identity != record.Target {
		return r.stopReorganization(ctx, p, "IdentityDiverged", record.InvalidationAcknowledged && !record.CleanupAcknowledged)
	}
	if n.Spec.Suspended && !cleanup && !record.CleanupAcknowledged {
		return r.report(ctx, p, "Reserved", "Parent suspension stops new replacement work", time.Second)
	}
	rpc := r.RPC.(ReorganizationRPC)
	preflight, cancel := context.WithTimeout(ctx, r.RPCTimeout)
	defer cancel()
	step := "Invalidate"
	if record.InvalidationAcknowledged {
		step = "Generate"
	}
	if cleanup {
		step = "Reconsider"
	}
	if record.CleanupAcknowledged {
		final, err := rpc.Tip(preflight, target.endpoint)
		if err != nil {
			return r.stopReorganization(ctx, p, "MechanismFailed", false)
		}
		hash, err := rpc.HashAt(preflight, target.endpoint, record.ForkParent.Height+int64(record.Spec.Depth)+1)
		if err != nil || record.VerifiedTip == nil || hash != record.VerifiedTip.Hash || record.VerifiedTip.Chainwork <= record.OriginalChain.Chainwork || final.Chainwork <= record.OriginalChain.Chainwork {
			return r.stopReorganization(ctx, p, "MechanismFailed", false)
		}
		before := p.DeepCopy()
		record.FinalChain = &final
		return ctrl.Result{RequeueAfter: time.Second}, r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	// Verify every generation receipt before another generation or normal cleanup.
	if record.StopReason == "" && record.VerifiedBlocks < record.BlocksGenerated {
		header, err := rpc.Header(preflight, target.endpoint, record.LastBlockHash)
		previous := record.ForkParent
		if record.VerifiedTip != nil {
			previous = *record.VerifiedTip
		}
		if err != nil || header.PreviousBlockHash != previous.Hash || header.Height != previous.Height+1 || header.Chainwork <= previous.Chainwork {
			return r.stopReorganization(ctx, p, "MechanismFailed", false)
		}
		before := p.DeepCopy()
		record.VerifiedTip = &header
		record.VerifiedBlocks++
		return ctrl.Result{RequeueAfter: time.Second}, r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	if step != "Reconsider" {
		if rpc.Check(preflight, target.endpoint, record.Spec.Address) != nil {
			return r.stopReorganization(ctx, p, "MechanismFailed", false)
		}
		current, err := rpc.Tip(preflight, target.endpoint)
		expected := record.OriginalChain
		if record.InvalidationAcknowledged {
			expected = record.ForkParent
			if record.VerifiedTip != nil {
				expected = *record.VerifiedTip
			}
		}
		if err != nil || current != expected {
			return r.stopReorganization(ctx, p, "MechanismFailed", false)
		}
		if step == "Generate" && record.LastCompletedAt != nil && r.Now().Before(record.LastCompletedAt.Add(time.Second)) {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
	}
	return r.armReorganization(ctx, p, n, a, target, step)
}

// stopReorganization records a stop or unsafe compensation fact without discarding earlier reasons.
func (r *Reconciler) stopReorganization(ctx context.Context, p *bitcoinv1.BitcoinProductionTarget, reason string, unsafe bool) (ctrl.Result, error) {
	before := p.DeepCopy()
	record := p.Status.Reorganization
	if record.StopReason == "" {
		record.StopReason = reason
	}
	record.CleanupUnsafe = record.CleanupUnsafe || unsafe
	if reflect.DeepEqual(before.Status, p.Status) {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	return ctrl.Result{RequeueAfter: time.Second}, r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

// armReorganization rechecks current identity and cancellation before one atomic authorization.
func (r *Reconciler) armReorganization(ctx context.Context, p *bitcoinv1.BitcoinProductionTarget, n *networkv1.StacksNetwork, a *actionv1.BitcoinReorganization, target admittedTarget, step string) (ctrl.Result, error) {
	record := p.Status.Reorganization
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(n), n); err != nil {
		return ctrl.Result{}, err
	}
	current, err := r.admitTarget(ctx, p, n, false)
	if err != nil {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	if current != target || !n.DeletionTimestamp.IsZero() {
		return r.stopReorganization(ctx, p, "IdentityDiverged", record.InvalidationAcknowledged && !record.CleanupAcknowledged)
	}
	if step != "Reconsider" {
		if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: record.Name}, a); err != nil {
			return ctrl.Result{RequeueAfter: time.Second}, client.IgnoreNotFound(err)
		}
		if string(a.UID) != record.UID || !reflect.DeepEqual(a.Spec, record.Spec) || actionv1.IsTerminalPhase(a.Status.Phase) || !a.DeletionTimestamp.IsZero() || n.Spec.Suspended || !r.Now().Before(record.ExpiresAt.Time) {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
	} else if !r.Now().Before(record.ExpiresAt.Add(30 * time.Second)) {
		return r.stopReorganization(ctx, p, "CleanupUncertain", true)
	}
	if err := ctx.Err(); err != nil {
		return ctrl.Result{}, err
	}
	before := p.DeepCopy()
	record.Step = step
	p.Status.DispatchState = "Armed"
	p.Status.DispatchID = fmt.Sprintf("%s/%s/%s", p.UID, r.ProcessNonce, p.ResourceVersion)
	if !r.collectors.reserve(p.Status.DispatchID, p.UID) {
		p.Status = before.Status
		return r.report(ctx, p, "Reserved", "Receipt collector unavailable or draining", time.Second)
	}
	if record.StartedAt == nil {
		now := metav1.NewTime(r.Now().UTC())
		record.StartedAt = &now
	}
	p.Status.TargetUID, p.Status.PodUID, p.Status.ContainerID = target.actorUID, target.podUID, target.containerID
	p.Status.Phase, p.Status.Message = "Collecting", "Collecting the reorganization mutation receipt"
	if err := r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		r.collectors.release(p.Status.DispatchID)
		return ctrl.Result{}, err
	}
	r.collectors.collect(p.DeepCopy(), target.endpoint)
	return ctrl.Result{RequeueAfter: time.Second}, nil
}
