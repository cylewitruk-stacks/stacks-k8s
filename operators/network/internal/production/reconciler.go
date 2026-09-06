package production

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

const ledgerFinalizer = "bitcoin.stacks.org/retain-production-ledger"

// Reconciler authorizes at most one unresolved mutating RPC per target ledger.
type Reconciler struct {
	// ReorganizationEnabled enables compensated local suffix replacement.
	ReorganizationEnabled bool
	// ActionsEnabled enables finite generation selection on the shared executor.
	ActionsEnabled bool
	// Client writes production resources; it has no workload mutation permissions.
	client.Client
	// APIReader supplies uncached admitted identities and ledger state.
	APIReader client.Reader
	// RPC implements the fixed method surface.
	RPC RPC
	// ConfigDigest identifies the administrator-approved static credential profile.
	ConfigDigest string
	// RPCTimeout bounds read-only preflight, not mutation receipt collection.
	RPCTimeout time.Duration
	// Now supplies wall-clock cadence anchors.
	Now func() time.Time
	// ProcessNonce distinguishes authorizations from different producer processes.
	ProcessNonce string
	// collectors retains bounded process-local dispatch and receipt state.
	collectors *collectorPool
}

// SetupWithManager registers production and owning-network watches.
func (r *Reconciler) SetupWithManager(manager ctrl.Manager, concurrency int) error {
	if r.Client == nil || r.APIReader == nil || r.RPC == nil || len(r.ConfigDigest) != 71 {
		return fmt.Errorf("producer requires clients, typed RPC and approved configuration digest")
	}
	if r.ReorganizationEnabled {
		if _, ok := r.RPC.(ReorganizationRPC); !ok {
			return fmt.Errorf("reorganization requires its typed RPC surface")
		}
	}
	if err := r.checkActionAPIs(manager.GetRESTMapper()); err != nil {
		return err
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.RPCTimeout == 0 {
		r.RPCTimeout = 10 * time.Second
	}
	if r.RPCTimeout < time.Second || r.RPCTimeout > 20*time.Second {
		return fmt.Errorf("producer preflight timeout must be 1..20 seconds")
	}
	if r.ProcessNonce == "" {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		r.ProcessNonce = hex.EncodeToString(nonce[:])
	}
	r.collectors = newCollectorPool(r, collectorLimit, collectorDrain)
	if err := manager.Add(r.collectors); err != nil {
		return err
	}
	mapNetwork := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []ctrl.Request {
		root := &bitcoinv1alpha1.BitcoinBlockProduction{}
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(object), root); err != nil {
			return nil
		}
		result := make([]ctrl.Request, 0, len(root.Status.Targets))
		for _, t := range root.Status.Targets {
			result = append(result, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: root.Namespace, Name: t.ResourceName}})
		}
		return result
	})
	builder := ctrl.NewControllerManagedBy(manager).For(&bitcoinv1alpha1.BitcoinProductionTarget{}).
		Watches(&networkv1alpha1.StacksNetwork{}, mapNetwork).
		Watches(&bitcoinv1alpha1.BitcoinBlockProduction{}, mapNetwork).
		WithOptions(controller.Options{MaxConcurrentReconciles: concurrency})
	if r.ActionsEnabled {
		builder = builder.Watches(&actionv1.BitcoinBlockGeneration{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []ctrl.Request {
			a := o.(*actionv1.BitcoinBlockGeneration)
			return []ctrl.Request{{NamespacedName: client.ObjectKey{Namespace: a.Namespace, Name: a.Spec.BitcoinNodeRef.Name}}}
		}))
	}
	if r.ReorganizationEnabled {
		builder = builder.Watches(&actionv1.BitcoinReorganization{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []ctrl.Request {
			a := o.(*actionv1.BitcoinReorganization)
			return []ctrl.Request{{NamespacedName: client.ObjectKey{Namespace: a.Namespace, Name: a.Spec.BitcoinNodeRef.Name}}}
		}))
	}
	return builder.Complete(r)
}

// Reconcile checks current intent, arms durably, then collects and accounts one receipt.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	policy := &bitcoinv1alpha1.BitcoinProductionTarget{}
	if err := r.APIReader.Get(ctx, request.NamespacedName, policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	parent := &networkv1alpha1.StacksNetwork{}
	err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: policy.Namespace, Name: policy.Spec.NetworkName}, parent)
	if apierrors.IsNotFound(err) || (err == nil && (string(parent.UID) != policy.Spec.NetworkUID || !parent.DeletionTimestamp.IsZero())) {
		return r.abandon(ctx, policy)
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if _, err := r.rootPolicy(ctx, policy, parent); err != nil {
		return r.report(ctx, policy, "Waiting", "Production ledger is not bound to the owning network", time.Second)
	}
	if !controllerutil.ContainsFinalizer(policy, ledgerFinalizer) {
		base := policy.DeepCopy()
		controllerutil.AddFinalizer(policy, ledgerFinalizer)
		return ctrl.Result{RequeueAfter: time.Millisecond}, r.Patch(ctx, policy, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
	}
	offer, skipReason, offerErr := r.opportunity(ctx, policy, parent)
	if offerErr != nil {
		return r.report(ctx, policy, "Waiting", offerErr.Error(), time.Second)
	}
	if offer != nil {
		if skipReason == "" && policy.Status.DispatchState == "Armed" {
			skipReason = "Outstanding"
		} else if skipReason == "" && (policy.Status.Action != nil || policy.Status.Reorganization != nil) {
			skipReason = "Reserved"
		}
		if skipReason != "" {
			return r.skip(ctx, policy, offer, skipReason)
		}
	}
	if policy.Status.Reorganization != nil {
		return r.reconcileReorganization(ctx, policy, parent)
	}
	if policy.Status.Action != nil {
		return r.reconcileAction(ctx, policy, parent)
	}
	if policy.Status.DispatchState == "Armed" {
		if phase := r.collectors.phase(policy.Status.DispatchID, policy.UID); phase != "" {
			message := "Waiting for the authorized RPC receipt"
			if phase == "Accounting" {
				message = "Received block retained locally pending durable accounting"
			}
			return r.report(ctx, policy, phase, message, time.Second)
		}
		return r.report(ctx, policy, "Blocked", "Dispatch has no durably accounted receipt; recreate the environment in a new namespace", 0)
	}
	if !policy.DeletionTimestamp.IsZero() {
		return r.report(ctx, policy, "Blocked", "Ledger retained until owning network deletion", 0)
	}
	if r.ActionsEnabled || r.ReorganizationEnabled {
		selected, err := r.selectAction(ctx, policy, parent)
		if err != nil || selected {
			return ctrl.Result{RequeueAfter: time.Second}, err
		}
	}
	if parent.Spec.BitcoinBlockProduction != nil && parent.Spec.BitcoinBlockProduction.Target(policy.Spec.Policy.Target) == nil {
		return r.report(ctx, policy, "Paused", "Target is not selected by the current policy; ledger retained", 0)
	}
	if parent.Spec.Suspended || parent.Spec.BitcoinBlockProduction == nil || parent.Spec.BitcoinBlockProduction.Paused {
		return r.report(ctx, policy, "Paused", "New production dispatches are paused", 0)
	}
	if offer == nil {
		return r.report(ctx, policy, "Running", "Waiting for a weighted policy opportunity", time.Second)
	}
	target, err := r.admit(ctx, policy, parent)
	if err != nil {
		return r.skip(ctx, policy, offer, "Unavailable")
	}
	preflight, cancel := context.WithTimeout(ctx, r.RPCTimeout)
	err = r.RPC.Check(preflight, target.endpoint, policy.Spec.Policy.Address)
	cancel()
	if err != nil {
		return r.skip(ctx, policy, offer, "Unavailable")
	}
	// Preflight may have taken time. Re-read desired operation and runtime before arming.
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
		return ctrl.Result{}, err
	}
	if parent.Spec.Suspended || parent.Spec.BitcoinBlockProduction == nil || parent.Spec.BitcoinBlockProduction.Paused || !parent.DeletionTimestamp.IsZero() {
		return ctrl.Result{RequeueAfter: time.Millisecond}, nil
	}
	currentTarget, err := r.admit(ctx, policy, parent)
	if err != nil || currentTarget != target {
		return r.report(ctx, policy, "Waiting", "Target changed during preflight", time.Second)
	}
	if err := ctx.Err(); err != nil {
		return ctrl.Result{}, err
	}
	currentOffer, reason, err := r.opportunity(ctx, policy, parent)
	if err != nil {
		return ctrl.Result{}, err
	}
	if currentOffer == nil || currentOffer.Number != offer.Number {
		return ctrl.Result{RequeueAfter: time.Millisecond}, nil
	}
	if reason != "" {
		return r.skip(ctx, policy, currentOffer, reason)
	}
	base := policy.DeepCopy()
	consume(policy, offer, "")
	policy.Status.DispatchState = "Armed"
	policy.Status.DispatchID = fmt.Sprintf("%s/%s/%s", policy.UID, r.ProcessNonce, policy.ResourceVersion)
	if !r.collectors.reserve(policy.Status.DispatchID, policy.UID) {
		policy.Status = base.Status
		return r.skip(ctx, policy, offer, "Capacity")
	}
	policy.Status.TargetUID, policy.Status.PodUID, policy.Status.ContainerID = target.actorUID, target.podUID, target.containerID
	policy.Status.Phase, policy.Status.Message, policy.Status.ObservedGeneration = "Collecting", "Collecting the authorized block receipt", policy.Generation
	if err := r.Status().Patch(ctx, policy, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		r.collectors.release(policy.Status.DispatchID)
		return ctrl.Result{}, err
	}
	r.collectors.collect(policy.DeepCopy(), target.endpoint)
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

// errLedgerRetired ends local receipt retention without granting another dispatch.
var errLedgerRetired = errors.New("dispatch ledger retired or its identity changed")

// account makes one idempotent accounting attempt using the original receipt timestamp.
func (r *Reconciler) account(ctx context.Context, armed *bitcoinv1alpha1.BitcoinProductionTarget, receipt receivedReceipt) error {
	current := &bitcoinv1alpha1.BitcoinProductionTarget{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(armed), current); err != nil {
		if apierrors.IsNotFound(err) {
			return errLedgerRetired
		}
		return err
	}
	if current.UID != armed.UID || current.Status.DispatchID != armed.Status.DispatchID {
		return errLedgerRetired
	}
	if current.Status.Reorganization != nil && current.Status.DispatchState == "Idle" && current.Status.Reorganization.LastDispatchID == armed.Status.DispatchID {
		return nil
	}
	if current.Status.DispatchState == "Idle" && current.Status.LastBlockHash == receipt.hash {
		return nil
	}
	if current.Status.DispatchState != "Armed" || current.Status.Phase == "Abandoned" {
		return errLedgerRetired
	}
	base := current.DeepCopy()
	current.Status.DispatchState = "Idle"
	if current.Status.Reorganization != nil {
		record := current.Status.Reorganization
		if armed.Status.Reorganization == nil || record.UID != armed.Status.Reorganization.UID || record.Step != receipt.method {
			return errLedgerRetired
		}
		switch receipt.method {
		case "Invalidate":
			record.InvalidationAcknowledged = true
		case "Generate":
			record.BlocksGenerated++
			record.ReplacementBlockHashes = append(record.ReplacementBlockHashes, receipt.hash)
			record.LastBlockHash = receipt.hash
		case "Reconsider":
			record.CleanupAcknowledged = true
		default:
			return errLedgerRetired
		}
		record.LastCompletedAt = receipt.completed.DeepCopy()
		record.LastDispatchID = current.Status.DispatchID
	} else if current.Status.Action != nil {
		if armed.Status.Action == nil || current.Status.Action.UID != armed.Status.Action.UID {
			return errLedgerRetired
		}
		current.Status.Action.BlocksGenerated++
		current.Status.Action.LastBlockHash, current.Status.Action.LastCompletedAt = receipt.hash, receipt.completed.DeepCopy()
		current.Status.Action.LastDispatchID = current.Status.DispatchID
	} else {
		current.Status.BlocksProduced++
	}
	if receipt.hash != "" {
		current.Status.LastBlockHash = receipt.hash
	}
	current.Status.LastCompletedAt = receipt.completed.DeepCopy()
	current.Status.Phase, current.Status.Message = "Running", "Block receipt accounted"
	return r.Status().Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// report updates human-readable state without changing dispatch authorization.
func (r *Reconciler) report(ctx context.Context, policy *bitcoinv1alpha1.BitcoinProductionTarget, phase, message string, after time.Duration) (ctrl.Result, error) {
	base := policy.DeepCopy()
	policy.Status.Phase, policy.Status.Message, policy.Status.ObservedGeneration = phase, message, policy.Generation
	if reflect.DeepEqual(base.Status, policy.Status) {
		return ctrl.Result{RequeueAfter: after}, nil
	}
	return ctrl.Result{RequeueAfter: after}, r.Status().Patch(ctx, policy, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// abandon releases only this controller's finalizer; deletion does not claim RPC quiescence.
func (r *Reconciler) abandon(ctx context.Context, policy *bitcoinv1alpha1.BitcoinProductionTarget) (ctrl.Result, error) {
	r.collectors.abandon(policy.UID)
	_, _ = r.report(ctx, policy, "Abandoned", "Owning environment removed; execution outcome is not a recovery claim", 0)
	current := &bitcoinv1alpha1.BitcoinProductionTarget{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(policy), current); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if current.UID != policy.UID {
		return ctrl.Result{}, nil
	}
	base := current.DeepCopy()
	controllerutil.RemoveFinalizer(current, ledgerFinalizer)
	return ctrl.Result{}, r.Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}
