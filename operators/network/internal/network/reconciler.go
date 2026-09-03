package network

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/network/api/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/canonical"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/leaf"
)

// Reconciler compiles StacksNetwork declarations and aggregates leaf status.
type Reconciler struct {
	client.Client
	APIReader client.Reader
	Scheme    *runtime.Scheme
	Now       func() time.Time
}

// Reconcile moves one aggregate network toward its compiled leaf topology.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	network := &networkv1alpha1.StacksNetwork{}
	if err := r.Get(ctx, request.NamespacedName, network); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	patchBase := network.DeepCopy()
	if !network.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	desired, err := Compile(network)
	if err != nil {
		log.FromContext(ctx).Error(err, "topology declaration is invalid")
		if statusErr := r.updateStatus(ctx, network, patchBase, degradedStatus(network, "ReconciliationFailed", err)); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, reconcile.TerminalError(err)
	}
	retiring, err := r.synchronize(ctx, network, desired)
	if err != nil {
		log.FromContext(ctx).Error(err, "topology synchronization failed")
		if leaf.IsTransientError(err) {
			return ctrl.Result{}, err
		}
		if statusErr := r.updateStatus(ctx, network, patchBase, degradedStatus(network, "ReconciliationFailed", err)); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}
	if retiring {
		status := transitionalStatus(network, int32(len(desired.Objects())), "ActorsRetiring", "Retired actor resources are still terminating")
		if err := r.updateStatus(ctx, network, patchBase, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	status, err := r.observe(ctx, network, desired)
	if err != nil {
		log.FromContext(ctx).Error(err, "topology observation failed")
		if statusErr := r.updateStatus(ctx, network, patchBase, degradedStatus(network, "ObservationFailed", err)); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.updateStatus(ctx, network, patchBase, status)
}

// SetupWithManager registers aggregate and owned-child watches.
func (r *Reconciler) SetupWithManager(manager ctrl.Manager, concurrency int) error {
	if r.Client == nil || r.APIReader == nil || r.Scheme == nil {
		return fmt.Errorf("StacksNetwork reconciler requires a client, uncached reader, and scheme")
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	return ctrl.NewControllerManagedBy(manager).
		For(&networkv1alpha1.StacksNetwork{}).
		Owns(&networkv1alpha1.BitcoinNode{}).
		Owns(&networkv1alpha1.StacksNode{}).
		Owns(&networkv1alpha1.StacksSigner{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: concurrency}).
		Complete(r)
}

type currentTopology struct {
	bitcoin       []networkv1alpha1.BitcoinNode
	bitcoinByName map[string]*networkv1alpha1.BitcoinNode
	stacks        []networkv1alpha1.StacksNode
	stacksByName  map[string]*networkv1alpha1.StacksNode
	signers       []networkv1alpha1.StacksSigner
	signersByName map[string]*networkv1alpha1.StacksSigner
}

func (r *Reconciler) synchronize(ctx context.Context, network *networkv1alpha1.StacksNetwork, desired DesiredTopology) (bool, error) {
	current, err := r.readCurrent(ctx, r.Client, network)
	if err != nil {
		return false, err
	}
	for index := range desired.BitcoinNodes {
		wanted := &desired.BitcoinNodes[index]
		if existing := current.bitcoinByName[wanted.Name]; existing != nil && childMatches(network, existing, wanted.Labels, &existing.Spec, &wanted.Spec) {
			continue
		}
		if err := r.applyBitcoinNode(ctx, network, wanted); err != nil {
			return false, err
		}
	}
	for index := range desired.StacksNodes {
		wanted := &desired.StacksNodes[index]
		if existing := current.stacksByName[wanted.Name]; existing != nil && childMatches(network, existing, wanted.Labels, &existing.Spec, &wanted.Spec) {
			continue
		}
		if err := r.applyStacksNode(ctx, network, wanted); err != nil {
			return false, err
		}
	}
	for index := range desired.Signers {
		wanted := &desired.Signers[index]
		if existing := current.signersByName[wanted.Name]; existing != nil && childMatches(network, existing, wanted.Labels, &existing.Spec, &wanted.Spec) {
			continue
		}
		if err := r.applySigner(ctx, network, wanted); err != nil {
			return false, err
		}
	}
	return r.prune(ctx, network, desired, current)
}

func childMatches(network *networkv1alpha1.StacksNetwork, current client.Object, desiredLabels map[string]string, currentSpec, desiredSpec any) bool {
	// Exact comparison is intentional: compiled leaf CRDs forbid API-server
	// defaults, and a repository contract test preserves that invariant.
	return ownedByNetwork(network, current) &&
		reflect.DeepEqual(current.GetLabels(), desiredLabels) && apiequality.Semantic.DeepEqual(currentSpec, desiredSpec)
}

func ownedByNetwork(network *networkv1alpha1.StacksNetwork, object client.Object) bool {
	owner := metav1.GetControllerOf(object)
	return owner != nil && owner.UID == network.UID && owner.APIVersion == networkv1alpha1.GroupVersion.String() && owner.Kind == "StacksNetwork"
}

func (r *Reconciler) applyBitcoinNode(ctx context.Context, network *networkv1alpha1.StacksNetwork, desired *networkv1alpha1.BitcoinNode) error {
	current := &networkv1alpha1.BitcoinNode{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		if err := assertAvailable(network, current); err != nil {
			return err
		}
		current.Labels = copyMap(desired.Labels)
		current.Spec = *desired.Spec.DeepCopy()
		return controllerutil.SetControllerReference(network, current, r.Scheme)
	})
	if err != nil {
		return fmt.Errorf("apply BitcoinNode %s: %w", desired.Name, err)
	}
	return nil
}

func (r *Reconciler) applyStacksNode(ctx context.Context, network *networkv1alpha1.StacksNetwork, desired *networkv1alpha1.StacksNode) error {
	current := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		if err := assertAvailable(network, current); err != nil {
			return err
		}
		current.Labels = copyMap(desired.Labels)
		current.Spec = *desired.Spec.DeepCopy()
		return controllerutil.SetControllerReference(network, current, r.Scheme)
	})
	if err != nil {
		return fmt.Errorf("apply StacksNode %s: %w", desired.Name, err)
	}
	return nil
}

func (r *Reconciler) applySigner(ctx context.Context, network *networkv1alpha1.StacksNetwork, desired *networkv1alpha1.StacksSigner) error {
	current := &networkv1alpha1.StacksSigner{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		if err := assertAvailable(network, current); err != nil {
			return err
		}
		current.Labels = copyMap(desired.Labels)
		current.Spec = *desired.Spec.DeepCopy()
		return controllerutil.SetControllerReference(network, current, r.Scheme)
	})
	if err != nil {
		return fmt.Errorf("apply StacksSigner %s: %w", desired.Name, err)
	}
	return nil
}

func assertAvailable(network *networkv1alpha1.StacksNetwork, object client.Object) error {
	if object.GetUID() == "" {
		return nil
	}
	owner := metav1.GetControllerOf(object)
	if owner != nil && owner.APIVersion == networkv1alpha1.GroupVersion.String() && owner.Kind == "StacksNetwork" && owner.UID != network.UID {
		return fmt.Errorf("derived resource name collision: %T %s is owned by StacksNetwork %q (UID %s), not %q (UID %s); choose network and actor names that produce unique resource names", object, object.GetName(), owner.Name, owner.UID, network.Name, network.UID)
	}
	if owner == nil || owner.UID != network.UID || owner.APIVersion != networkv1alpha1.GroupVersion.String() || owner.Kind != "StacksNetwork" {
		return fmt.Errorf("refuse to adopt %T %s without matching StacksNetwork owner UID", object, object.GetName())
	}
	return nil
}

func (r *Reconciler) prune(ctx context.Context, network *networkv1alpha1.StacksNetwork, desired DesiredTopology, current currentTopology) (bool, error) {
	retiring := false
	wanted := map[string]map[string]bool{"BitcoinNode": {}, "StacksNode": {}, "StacksSigner": {}}
	for _, object := range desired.Objects() {
		wanted[object.kind][object.name] = true
	}
	for index := range current.bitcoin {
		if !wanted["BitcoinNode"][current.bitcoin[index].Name] {
			retiring = true
			if err := r.deleteOwned(ctx, network, &current.bitcoin[index]); err != nil {
				return false, err
			}
		}
	}
	for index := range current.stacks {
		if !wanted["StacksNode"][current.stacks[index].Name] {
			retiring = true
			if err := r.deleteOwned(ctx, network, &current.stacks[index]); err != nil {
				return false, err
			}
		}
	}
	for index := range current.signers {
		if !wanted["StacksSigner"][current.signers[index].Name] {
			retiring = true
			if err := r.deleteOwned(ctx, network, &current.signers[index]); err != nil {
				return false, err
			}
		}
	}
	return retiring, nil
}

func (r *Reconciler) deleteOwned(ctx context.Context, network *networkv1alpha1.StacksNetwork, object client.Object) error {
	if err := assertAvailable(network, object); err != nil {
		return err
	}
	uid := object.GetUID()
	propagation := metav1.DeletePropagationForeground
	if err := r.Delete(ctx, object, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}, PropagationPolicy: &propagation}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete retired %T %s: %w", object, object.GetName(), err)
	}
	return nil
}

func (r *Reconciler) observe(ctx context.Context, network *networkv1alpha1.StacksNetwork, desired DesiredTopology) (networkv1alpha1.StacksNetworkStatus, error) {
	status := networkv1alpha1.StacksNetworkStatus{ObservedGeneration: network.Generation, DesiredActors: int32(len(desired.Objects())), Conditions: append([]metav1.Condition(nil), network.Status.Conditions...)}
	identities := []networkv1alpha1.ActorIdentity{}
	allReady := true
	appendChild := func(kind, name, actor, phase string, ready bool, identity *networkv1alpha1.ActorIdentity) {
		status.Children = append(status.Children, networkv1alpha1.ChildStatus{Kind: kind, Name: name, ActorName: actor, Ready: ready, Phase: phase})
		if ready {
			status.ReadyActors++
		} else {
			allReady = false
		}
		if identity != nil {
			identities = append(identities, *identity.DeepCopy())
		}
	}
	current, err := r.readCurrent(ctx, r.APIReader, network)
	if err != nil {
		return status, err
	}
	if !current.matches(desired) {
		return status, fmt.Errorf("directly observed owned leaf set does not match the compiled topology")
	}
	for _, desiredNode := range desired.BitcoinNodes {
		current := current.bitcoinByName[desiredNode.Name]
		if current == nil {
			return status, fmt.Errorf("directly observed BitcoinNode %s is absent", desiredNode.Name)
		}
		if !childMatches(network, current, desiredNode.Labels, &current.Spec, &desiredNode.Spec) {
			return status, fmt.Errorf("directly observed BitcoinNode %s declaration or ownership diverged", desiredNode.Name)
		}
		ready := current.Status.Ready && current.Status.ObservedGeneration == current.Generation && current.Status.Identity != nil
		appendChild("BitcoinNode", current.Name, current.Spec.ActorName, current.Status.Phase, ready, current.Status.Identity)
	}
	for _, desiredNode := range desired.StacksNodes {
		current := current.stacksByName[desiredNode.Name]
		if current == nil {
			return status, fmt.Errorf("directly observed StacksNode %s is absent", desiredNode.Name)
		}
		if !childMatches(network, current, desiredNode.Labels, &current.Spec, &desiredNode.Spec) {
			return status, fmt.Errorf("directly observed StacksNode %s declaration or ownership diverged", desiredNode.Name)
		}
		ready := current.Status.Ready && current.Status.ObservedGeneration == current.Generation && current.Status.Identity != nil
		appendChild("StacksNode", current.Name, current.Spec.ActorName, current.Status.Phase, ready, current.Status.Identity)
	}
	for _, desiredSigner := range desired.Signers {
		current := current.signersByName[desiredSigner.Name]
		if current == nil {
			return status, fmt.Errorf("directly observed StacksSigner %s is absent", desiredSigner.Name)
		}
		if !childMatches(network, current, desiredSigner.Labels, &current.Spec, &desiredSigner.Spec) {
			return status, fmt.Errorf("directly observed StacksSigner %s declaration or ownership diverged", desiredSigner.Name)
		}
		ready := current.Status.Ready && current.Status.ObservedGeneration == current.Generation && current.Status.Identity != nil
		appendChild("StacksSigner", current.Name, current.Spec.ActorName, current.Status.Phase, ready, current.Status.Identity)
	}
	sort.Slice(status.Children, func(i, j int) bool {
		if status.Children[i].Kind != status.Children[j].Kind {
			return status.Children[i].Kind < status.Children[j].Kind
		}
		return status.Children[i].Name < status.Children[j].Name
	})
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].Kind != identities[j].Kind {
			return identities[i].Kind < identities[j].Kind
		}
		return identities[i].Name < identities[j].Name
	})
	if network.Spec.Suspended {
		status.Phase = "Suspended"
		allReady = false
		meta.SetStatusCondition(&status.Conditions, condition(network.Generation, metav1.ConditionFalse, "Ready", "NetworkSuspended", "Every declared actor is suspended"))
		meta.SetStatusCondition(&status.Conditions, condition(network.Generation, metav1.ConditionTrue, "Suspended", "NetworkSuspended", "Every declared actor is suspended"))
	} else if allReady && len(identities) == int(status.DesiredActors) {
		meta.SetStatusCondition(&status.Conditions, condition(network.Generation, metav1.ConditionFalse, "Suspended", "NetworkActive", "The network is not suspended"))
		digest, err := inventoryDigest(network.Generation, identities)
		if err != nil {
			return status, err
		}
		status.Phase, status.InventoryReady, status.InventoryDigest, status.Actors = "Ready", true, digest, identities
		if network.Status.InventoryDigest == digest && network.Status.InventoryObservedAt != nil {
			status.InventoryObservedAt = network.Status.InventoryObservedAt.DeepCopy()
		} else {
			now := metav1.NewTime(r.Now().UTC())
			status.InventoryObservedAt = &now
		}
		meta.SetStatusCondition(&status.Conditions, condition(network.Generation, metav1.ConditionTrue, "Ready", "TopologyReady", "Every declared actor has an admitted runtime identity"))
	} else {
		status.Phase = "Progressing"
		meta.SetStatusCondition(&status.Conditions, condition(network.Generation, metav1.ConditionFalse, "Suspended", "NetworkActive", "The network is not suspended"))
		meta.SetStatusCondition(&status.Conditions, condition(network.Generation, metav1.ConditionFalse, "Ready", "ActorsNotReady", fmt.Sprintf("%d of %d actors are ready", status.ReadyActors, status.DesiredActors)))
	}
	return status, nil
}

func inventoryDigest(generation int64, actors []networkv1alpha1.ActorIdentity) (string, error) {
	payload := struct {
		SchemaVersion      string                          `json:"schemaVersion"`
		ObservedGeneration int64                           `json:"observedGeneration"`
		Actors             []networkv1alpha1.ActorIdentity `json:"actors"`
	}{SchemaVersion: "network.stacks.org/inventory/v1", ObservedGeneration: generation, Actors: actors}
	return canonical.Digest(payload)
}

func (current currentTopology) matches(desired DesiredTopology) bool {
	if len(current.bitcoin) != len(desired.BitcoinNodes) || len(current.stacks) != len(desired.StacksNodes) || len(current.signers) != len(desired.Signers) {
		return false
	}
	for index := range desired.BitcoinNodes {
		if current.bitcoinByName[desired.BitcoinNodes[index].Name] == nil {
			return false
		}
	}
	for index := range desired.StacksNodes {
		if current.stacksByName[desired.StacksNodes[index].Name] == nil {
			return false
		}
	}
	for index := range desired.Signers {
		if current.signersByName[desired.Signers[index].Name] == nil {
			return false
		}
	}
	return true
}

func (r *Reconciler) readCurrent(ctx context.Context, reader client.Reader, network *networkv1alpha1.StacksNetwork) (currentTopology, error) {
	bitcoin := &networkv1alpha1.BitcoinNodeList{}
	if err := reader.List(ctx, bitcoin, client.InNamespace(network.Namespace)); err != nil {
		return currentTopology{}, fmt.Errorf("list BitcoinNode children: %w", err)
	}
	stacks := &networkv1alpha1.StacksNodeList{}
	if err := reader.List(ctx, stacks, client.InNamespace(network.Namespace)); err != nil {
		return currentTopology{}, fmt.Errorf("list StacksNode children: %w", err)
	}
	signers := &networkv1alpha1.StacksSignerList{}
	if err := reader.List(ctx, signers, client.InNamespace(network.Namespace)); err != nil {
		return currentTopology{}, fmt.Errorf("list StacksSigner children: %w", err)
	}
	current := currentTopology{
		bitcoinByName: make(map[string]*networkv1alpha1.BitcoinNode, len(bitcoin.Items)),
		stacksByName:  make(map[string]*networkv1alpha1.StacksNode, len(stacks.Items)),
		signersByName: make(map[string]*networkv1alpha1.StacksSigner, len(signers.Items)),
	}
	for index := range bitcoin.Items {
		if ownedByNetwork(network, &bitcoin.Items[index]) {
			current.bitcoin = append(current.bitcoin, *bitcoin.Items[index].DeepCopy())
		}
	}
	for index := range stacks.Items {
		if ownedByNetwork(network, &stacks.Items[index]) {
			current.stacks = append(current.stacks, *stacks.Items[index].DeepCopy())
		}
	}
	for index := range signers.Items {
		if ownedByNetwork(network, &signers.Items[index]) {
			current.signers = append(current.signers, *signers.Items[index].DeepCopy())
		}
	}
	for index := range current.bitcoin {
		current.bitcoinByName[current.bitcoin[index].Name] = &current.bitcoin[index]
	}
	for index := range current.stacks {
		current.stacksByName[current.stacks[index].Name] = &current.stacks[index]
	}
	for index := range current.signers {
		current.signersByName[current.signers[index].Name] = &current.signers[index]
	}
	return current, nil
}

func degradedStatus(network *networkv1alpha1.StacksNetwork, reason string, err error) networkv1alpha1.StacksNetworkStatus {
	status := networkv1alpha1.StacksNetworkStatus{
		ObservedGeneration: network.Generation,
		Phase:              "Degraded",
		DesiredActors:      int32(len(network.Spec.BitcoinNodes) + len(network.Spec.StacksNodes) + len(network.Spec.Signers)),
		Conditions:         append([]metav1.Condition(nil), network.Status.Conditions...),
	}
	meta.SetStatusCondition(&status.Conditions, condition(network.Generation, metav1.ConditionFalse, "Suspended", "NetworkActive", "The network is not suspended"))
	meta.SetStatusCondition(&status.Conditions, condition(network.Generation, metav1.ConditionFalse, "Ready", reason, err.Error()))
	return status
}

func transitionalStatus(network *networkv1alpha1.StacksNetwork, desiredActors int32, reason, message string) networkv1alpha1.StacksNetworkStatus {
	status := networkv1alpha1.StacksNetworkStatus{
		ObservedGeneration: network.Generation, Phase: "Progressing", DesiredActors: desiredActors,
		Conditions: append([]metav1.Condition(nil), network.Status.Conditions...),
	}
	meta.SetStatusCondition(&status.Conditions, condition(network.Generation, metav1.ConditionFalse, "Suspended", "NetworkActive", "The network is not suspended"))
	meta.SetStatusCondition(&status.Conditions, condition(network.Generation, metav1.ConditionFalse, "Ready", reason, message))
	return status
}

func condition(generation int64, status metav1.ConditionStatus, kind, reason, message string) metav1.Condition {
	return metav1.Condition{Type: kind, Status: status, ObservedGeneration: generation, Reason: reason, Message: message}
}

func (r *Reconciler) updateStatus(ctx context.Context, network, patchBase *networkv1alpha1.StacksNetwork, desired networkv1alpha1.StacksNetworkStatus) error {
	if reflect.DeepEqual(patchBase.Status, desired) {
		return nil
	}
	network.Status = desired
	return r.Status().Patch(ctx, network, client.MergeFrom(patchBase))
}
