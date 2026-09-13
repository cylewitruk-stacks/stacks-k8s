package foundation

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// inputDependencyIndex indexes public names even before those inputs exist or resolve.
const inputDependencyIndex = "foundation.inputDependency"

// inputReference identifies one same-namespace public dependency, never its authority.
type inputReference struct {
	// kind selects the public resource API.
	kind string
	// name is the declared or retained dependency name.
	name string
}

// key encodes a cache index key; neither kind nor Kubernetes names contain slashes.
func (r inputReference) key() string { return r.kind + "/" + r.name }

// publicInputs contains the reusable kinds admitted by the foundation.
func publicInputs() []client.Object {
	return []client.Object{&bitcoin.BitcoinNode{}, &bitcoin.BitcoinWallet{}, &bitcoin.BitcoinBlockProduction{}, &bitcoin.BitcoinBlockSchedule{}, &stacks.StacksAccount{}, &stacks.StacksNode{}, &stacks.StacksSigner{}, &stacks.StacksStacker{}, &stacks.StacksFaucet{}, &stacks.StacksContractSet{}, &stacks.StacksTransactionProduction{}, &api.StacksEpochSchedule{}}
}

// inputReferences extracts direct public dependencies without cache or Secret reads.
func inputReferences(obj client.Object) []string {
	keys := map[string]bool{}
	add := func(kind, name string) {
		if name != "" {
			keys[(inputReference{kind, name}).key()] = true
		}
	}
	ref := func(kind string, value *common.NameRef) {
		if value != nil {
			add(kind, value.Name)
		}
	}
	secret := func(value *common.SecretKeyRef) {
		if value != nil {
			add("Secret", value.Name)
		}
	}
	config := func(value *api.Configuration, owner client.Object) {
		if value == nil {
			return
		}
		copy := value.DeepCopy()
		if owner != nil {
			defaultAccount(copy, owner)
		}
		configurationInputs(copy, add)
	}
	switch v := obj.(type) {
	case *api.StacksNetwork:
		ref("StacksEpochSchedule", v.Spec.EpochScheduleRef)
		if v.Spec.Genesis != nil {
			for _, allocation := range v.Spec.Genesis.Allocations {
				ref("StacksAccount", &allocation.AccountRef)
			}
		}
		add("StacksGenesis", RuntimeName(string(v.UID), "", "StacksGenesis", v.Name, "genesis"))
		if records := v.Status.Bitcoin; records != nil {
			for _, record := range records.ExecutionRefs {
				add("BitcoinExecution", record.Name)
			}
			if records.InitializationRef != nil {
				add("BitcoinInitialization", records.InitializationRef.Name)
			}
		}
		for _, entry := range v.Spec.Participants {
			name := ParticipantName(string(v.UID), entry.Name)
			add("StacksNetworkParticipant", name)
			ref(string(entry.Kind), entry.Definition.Ref)
			config(entry.Definition.Inline, &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: name}})
			config(entry.Overrides, nil)
		}
		// Withdrawal cleanup still needs deletion/identity notifications after omission.
		for _, identity := range v.Status.Identities {
			add("StacksNetworkParticipant", ParticipantName(string(v.UID), identity.Name))
		}
	case *api.StacksNetworkParticipant:
		add(string(v.Spec.Kind), v.Spec.Source.Name)
		config(&v.Spec.Configuration, v)
		if v.Status.Admission != nil {
			add(string(v.Spec.Kind), v.Status.Admission.Source.Name)
			config(&v.Status.Admission.Configuration, nil)
			for _, binding := range v.Status.Admission.Dependencies {
				add(binding.Kind, binding.Name)
			}
		}
	case *stacks.StacksAccount:
		if v.Spec.Key != nil {
			secret(v.Spec.Key.SecretRef)
		}
		add("Secret", RuntimeName("", string(v.UID), "StacksAccount", v.Name, "key"))
	case *bitcoin.BitcoinWallet:
		if v.Spec.KeySource != nil {
			secret(v.Spec.KeySource.SecretRef)
			ref("StacksAccount", v.Spec.KeySource.StacksMinerAccountRef)
		}
		add("Secret", RuntimeName("", string(v.UID), "BitcoinWallet", v.Name, "key"))
	case *bitcoin.BitcoinNode:
		config(&api.Configuration{BitcoinNode: &v.Spec}, v)
	case *stacks.StacksNode:
		config(&api.Configuration{StacksNode: &v.Spec}, v)
	case *stacks.StacksSigner:
		config(&api.Configuration{StacksSigner: &v.Spec}, v)
	case *stacks.StacksStacker:
		config(&api.Configuration{StacksStacker: &v.Spec}, v)
	case *stacks.StacksFaucet:
		config(&api.Configuration{StacksFaucet: &v.Spec}, v)
	case *stacks.StacksContractSet:
		config(&api.Configuration{StacksContractSet: &v.Spec}, v)
	case *stacks.StacksTransactionProduction:
		config(&api.Configuration{StacksTransactionProduction: &v.Spec}, v)
	case *bitcoin.BitcoinBlockProduction:
		config(&api.Configuration{BitcoinBlockProduction: &v.Spec}, v)
	}
	if v, ok := obj.(resolvable); ok {
		status := v.GetResolutionStatus()
		secret(status.CredentialsRef)
		for _, binding := range status.Dependencies {
			add(binding.Kind, binding.Name)
		}
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}

// configurationInputs selects reusable inputs, excluding network-relative participant aliases.
func configurationInputs(c *api.Configuration, add func(string, string)) {
	ref := func(kind string, value *common.NameRef) {
		if value != nil {
			add(kind, value.Name)
		}
	}
	refs := func(kind string, values *[]common.NameRef) {
		for _, value := range ptr.Deref(values, nil) {
			ref(kind, &value)
		}
	}
	actor := func(fields common.ActorFields) {
		if fields.Config != nil && fields.Config.SecretRef != nil {
			add("Secret", fields.Config.SecretRef.Name)
		}
	}
	if v := c.BitcoinNode; v != nil {
		actor(v.ActorFields)
		refs("BitcoinWallet", v.WalletRefs)
	}
	if v := c.StacksNode; v != nil {
		actor(v.ActorFields)
		ref("StacksAccount", v.IdentityAccountRef)
		if v.Mining != nil {
			ref("BitcoinWallet", v.Mining.BitcoinWalletRef)
		}
	}
	if v := c.StacksSigner; v != nil {
		actor(v.ActorFields)
		ref("StacksAccount", v.AccountRef)
	}
	if v := c.StacksStacker; v != nil {
		ref("StacksAccount", v.HolderAccountRef)
		ref("StacksAccount", v.AdministratorAccountRef)
	}
	if v := c.StacksFaucet; v != nil {
		ref("StacksAccount", v.AccountRef)
	}
	if v := c.StacksTransactionProduction; v != nil {
		ref("StacksAccount", v.AccountRef)
		if v.Recipient != nil {
			ref("StacksAccount", v.Recipient.AccountRef)
		}
	}
	if v := c.StacksContractSet; v != nil {
		ref("StacksAccount", v.DeployerAccountRef)
		if v.Initialization != nil {
			for _, account := range v.Initialization.SignerAccountRefs {
				ref("StacksAccount", &account)
			}
			ref("StacksAccount", &v.Initialization.AggregateKeyAccountRef)
		}
	}
	if v := c.BitcoinBlockProduction; v != nil {
		ref("BitcoinBlockSchedule", v.ScheduleRef)
		ref("BitcoinWallet", v.PayoutWalletRef)
		if v.Initialization != nil {
			refs("BitcoinWallet", v.Initialization.MinerWalletRefs)
		}
	}
}

// admissionEvents separates graph inputs from runtime observation and readiness traffic.
func admissionEvents() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		a, b := e.ObjectOld, e.ObjectNew
		if a == nil || b == nil {
			return false
		}
		if a.GetUID() != b.GetUID() || a.GetGeneration() != b.GetGeneration() || !equal(a.GetDeletionTimestamp(), b.GetDeletionTimestamp()) || !equal(a.GetOwnerReferences(), b.GetOwnerReferences()) || !equal(a.GetFinalizers(), b.GetFinalizers()) {
			return true
		}
		switch old := a.(type) {
		case *api.StacksNetworkParticipant:
			current, ok := b.(*api.StacksNetworkParticipant)
			return !ok || !equal(old.Spec, current.Spec) || !equal(old.Status.Admission, current.Status.Admission) || !equal(admissionConditions(old.Status.Conditions), admissionConditions(current.Status.Conditions))
		case *api.StacksNetwork:
			current, ok := b.(*api.StacksNetwork)
			return !ok || !equal(old.Spec, current.Spec) || !equal(old.Status.Identities, current.Status.Identities) || !equal(old.Status.GenesisRef, current.Status.GenesisRef) || !equal(gateCompletion(old.Status.Initialization), gateCompletion(current.Status.Initialization)) || (old.Status.Phase != current.Status.Phase && (old.Status.Phase == api.NetworkPhaseFailed || current.Status.Phase == api.NetworkPhaseFailed)) || meta.IsStatusConditionTrue(old.Status.Conditions, "Failed") != meta.IsStatusConditionTrue(current.Status.Conditions, "Failed")
		case *api.StacksGenesis:
			current, ok := b.(*api.StacksGenesis)
			return !ok || !equal(old.Spec, current.Spec)
		}
		if old, ok := a.(resolvable); ok {
			current, ok := b.(resolvable)
			if !ok {
				return true
			}
			oldSpec, oldErr := sourceSpec(a)
			newSpec, newErr := sourceSpec(b)
			oldStatus, newStatus := old.GetResolutionStatus().DeepCopy(), current.GetResolutionStatus().DeepCopy()
			oldStatus.Conditions, newStatus.Conditions = admissionConditions(oldStatus.Conditions), admissionConditions(newStatus.Conditions)
			return oldErr != nil || newErr != nil || !equal(oldSpec, newSpec) || !equal(oldStatus, newStatus)
		}
		// Metadata-only Secret watches cannot inspect payload/immutable transitions.
		return a.GetResourceVersion() != b.GetResourceVersion()
	}}
}

// admissionConditions ignores heartbeat timestamps and domain runtime conditions.
func admissionConditions(conditions []metav1.Condition) []metav1.Condition {
	var result []metav1.Condition
	for _, condition := range conditions {
		if condition.Type == "AdmissionReady" || condition.Type == "Resolved" || condition.Type == "PolicyDeferred" || condition.Type == "ConfigVerified" {
			condition.LastTransitionTime = metav1.Time{}
			result = append(result, condition)
		}
	}
	slices.SortFunc(result, func(a, b metav1.Condition) int { return cmp.Compare(a.Type, b.Type) })
	return result
}

// indexedInputs lists only immediate consumers of an input through the cache index.
func indexedInputs(ctx context.Context, c client.Client, object client.Object, namespace, key string) ([]client.Object, error) {
	gvk, err := apiutil.GVKForObject(object, c.Scheme())
	if err != nil {
		return nil, err
	}
	gvk.Kind += "List"
	value, err := c.Scheme().New(gvk)
	if err != nil {
		return nil, err
	}
	list := value.(client.ObjectList)
	if err := c.List(ctx, list, client.InNamespace(namespace), client.MatchingFields{inputDependencyIndex: key}); err != nil {
		return nil, err
	}
	var result []client.Object
	err = meta.EachListItem(list, func(item runtime.Object) error {
		result = append(result, item.(client.Object))
		return nil
	})
	return result, err
}

// intermediateInputs enumerates the fixed reusable-reference paths; participant aliases stay root-local.
func intermediateInputs(kind string) []client.Object {
	switch kind {
	case "Secret":
		return []client.Object{&stacks.StacksAccount{}, &bitcoin.BitcoinWallet{}, &bitcoin.BitcoinNode{}, &stacks.StacksNode{}, &stacks.StacksSigner{}}
	case "StacksAccount":
		return []client.Object{&bitcoin.BitcoinWallet{}, &stacks.StacksNode{}, &stacks.StacksSigner{}, &stacks.StacksStacker{}, &stacks.StacksFaucet{}, &stacks.StacksContractSet{}, &stacks.StacksTransactionProduction{}}
	case "BitcoinWallet":
		return []client.Object{&bitcoin.BitcoinNode{}, &stacks.StacksNode{}, &bitcoin.BitcoinBlockProduction{}}
	case "BitcoinBlockSchedule":
		return []client.Object{&bitcoin.BitcoinBlockProduction{}}
	default:
		return nil
	}
}

// enqueueInputRoots follows indexed input references to roots, including unresolved and retained policies.
func enqueueInputRoots(c client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		pending := []client.Object{obj}
		seen := map[string]bool{}
		roots := map[client.ObjectKey]bool{}
		for len(pending) > 0 {
			input := pending[0]
			pending = pending[1:]
			gvk, err := apiutil.GVKForObject(input, c.Scheme())
			if err != nil {
				return inputWatchError(ctx, obj, err)
			}
			key := (inputReference{gvk.Kind, input.GetName()}).key()
			if seen[key] {
				continue
			}
			seen[key] = true
			consumers := append([]client.Object{&api.StacksNetwork{}, &api.StacksNetworkParticipant{}}, intermediateInputs(gvk.Kind)...)
			for _, kind := range consumers {
				objects, err := indexedInputs(ctx, c, kind, obj.GetNamespace(), key)
				if err != nil {
					return inputWatchError(ctx, obj, err)
				}
				for _, object := range objects {
					if root, ok := object.(*api.StacksNetwork); ok {
						roots[client.ObjectKeyFromObject(root)] = true
					} else {
						pending = append(pending, object)
					}
				}
			}
		}
		result := make([]reconcile.Request, 0, len(roots))
		for key := range roots {
			result = append(result, reconcile.Request{NamespacedName: key})
		}
		return result
	}
}

// inputWatchError preserves a repair notification when a cache index lookup fails.
func inputWatchError(ctx context.Context, obj client.Object, err error) []reconcile.Request {
	ctrl.LoggerFrom(ctx).Error(err, "Unable to route public input notification", "input", client.ObjectKeyFromObject(obj))
	return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: obj.GetNamespace(), Name: "network"}}}
}

// setupWatches registers public input indexes before starting any foundation controller.
func (r *Reconciler) setupWatches(m ctrl.Manager) error {
	inputs := publicInputs()
	for _, object := range append([]client.Object{&api.StacksNetwork{}, &api.StacksNetworkParticipant{}}, inputs...) {
		if err := m.GetFieldIndexer().IndexField(context.Background(), object, inputDependencyIndex, inputReferences); err != nil {
			return fmt.Errorf("index foundation inputs for %T: %w", object, err)
		}
	}
	includeRuntime := r.Runtime != nil
	enqueue := handler.TypedEnqueueRequestsFromMapFunc(inputRequests(enqueueInputRoots(r.Client), includeRuntime))
	rootRequests := inputRequests(func(_ context.Context, obj client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(obj)}}
	}, includeRuntime)
	filter := builder.WithPredicates(admissionEvents())
	b := builder.TypedControllerManagedBy[networkRequest](m).Named("foundation-network").
		WithOptions(controller.TypedOptions[networkRequest]{MaxConcurrentReconciles: 1}).
		Watches(&api.StacksNetwork{}, handler.TypedEnqueueRequestsFromMapFunc(rootRequests), filter).
		Watches(&api.StacksNetworkParticipant{}, enqueue, filter).Watches(&api.StacksGenesis{}, enqueue, filter)
	for _, input := range inputs {
		b = b.Watches(input, enqueue, filter)
	}
	b = b.WatchesMetadata(&corev1.Secret{}, enqueue, filter)
	if includeRuntime {
		observe := runtimeNotifications(r.Client)
		for _, object := range []client.Object{&api.StacksNetwork{}, &api.StacksNetworkParticipant{}, &bitcoin.BitcoinExecution{}, &bitcoin.BitcoinInitialization{}} {
			b = b.Watches(object, observe, builder.WithPredicates(runtimeEvents()))
		}
	}
	return b.Complete(reconcile.TypedFunc[networkRequest](r.reconcileRequest))
}

// enqueueIdentityInputs routes credential/account changes to their exact identity consumers.
func enqueueIdentityInputs(c client.Client, source client.Object) handler.MapFunc {
	return func(ctx context.Context, input client.Object) []reconcile.Request {
		gvk, err := apiutil.GVKForObject(input, c.Scheme())
		if err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "Identify resolver input")
			return nil
		}
		objects, err := indexedInputs(ctx, c, source, input.GetNamespace(), (inputReference{gvk.Kind, input.GetName()}).key())
		if err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "List identity input consumers")
			return nil
		}
		result := make([]reconcile.Request, 0, len(objects))
		for _, object := range objects {
			result = append(result, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(object)})
		}
		return result
	}
}

// setupIdentityWatches uses indexes registered by the root setup to target identity inputs.
func (r *IdentityReconciler) setupIdentityWatches(m ctrl.Manager) error {
	name := "foundation-account"
	if r.Wallet {
		name = "foundation-wallet"
	}
	enqueue := handler.EnqueueRequestsFromMapFunc(enqueueIdentityInputs(r.Client, r.object()))
	filter := builder.WithPredicates(admissionEvents())
	b := ctrl.NewControllerManagedBy(m).Named(name).For(r.object(), filter).Owns(&corev1.ConfigMap{}).Owns(&batchv1.Job{}).WatchesMetadata(&corev1.Secret{}, enqueue, filter)
	if r.Wallet {
		b = b.Watches(&stacks.StacksAccount{}, enqueue, filter)
	}
	return b.Complete(r)
}

// inputRequests queues both paths for input changes; coalescing one cannot discard the other.
func inputRequests(inputs handler.MapFunc, includeRuntime bool) handler.TypedMapFunc[client.Object, networkRequest] {
	return func(ctx context.Context, obj client.Object) []networkRequest {
		var result []networkRequest
		for _, request := range inputs(ctx, obj) {
			result = append(result, networkRequest{key: request.NamespacedName})
			if includeRuntime {
				result = append(result, networkRequest{key: request.NamespacedName, runtimeOnly: true})
			}
		}
		return result
	}
}

// runtimeRequests uses one indexed public lookup, with an owner notification for newly allocated records.
// Neither this mapping nor its internal queue scope grants authority to admitted workloads.
func runtimeRequests(c client.Client) handler.TypedMapFunc[client.Object, networkRequest] {
	return func(ctx context.Context, obj client.Object) []networkRequest {
		if root, ok := obj.(*api.StacksNetwork); ok {
			return []networkRequest{{key: client.ObjectKeyFromObject(root), runtimeOnly: true}}
		}
		gvk, err := apiutil.GVKForObject(obj, c.Scheme())
		if err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "Identify runtime notification")
			return nil
		}
		roots, err := indexedInputs(ctx, c, &api.StacksNetwork{}, obj.GetNamespace(), (inputReference{gvk.Kind, obj.GetName()}).key())
		if err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "Route runtime notification")
			return []networkRequest{{key: client.ObjectKey{Namespace: obj.GetNamespace(), Name: "network"}, runtimeOnly: true}}
		}
		var result []networkRequest
		for _, root := range roots {
			result = append(result, networkRequest{key: client.ObjectKeyFromObject(root), runtimeOnly: true})
		}
		if len(result) == 0 {
			owner := metav1.GetControllerOf(obj)
			if owner != nil && owner.APIVersion == api.GroupVersion.String() && owner.Kind == "StacksNetwork" && owner.Name == "network" {
				result = append(result, networkRequest{key: client.ObjectKey{Namespace: obj.GetNamespace(), Name: owner.Name}, runtimeOnly: true})
			}
		}
		return result
	}
}

// runtimeEvents drops heartbeat-only notifications; periodic projection evaluates freshness.
func runtimeEvents() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		a, b := e.ObjectOld, e.ObjectNew
		if a == nil || b == nil {
			return false
		}
		if a.GetUID() != b.GetUID() || a.GetGeneration() != b.GetGeneration() || !equal(a.GetDeletionTimestamp(), b.GetDeletionTimestamp()) || !equal(a.GetOwnerReferences(), b.GetOwnerReferences()) || !equal(a.GetFinalizers(), b.GetFinalizers()) {
			return true
		}
		switch old := a.(type) {
		case *api.StacksNetworkParticipant:
			current, ok := b.(*api.StacksNetworkParticipant)
			return !ok || !equal(old.Status.Runtime, current.Status.Runtime) || !equal(old.Status.BitcoinControl, current.Status.BitcoinControl) || !equal(old.Status.Execution, current.Status.Execution) || !equal(runtimeConditions(old.Status.Conditions), runtimeConditions(current.Status.Conditions))
		case *api.StacksNetwork:
			current, ok := b.(*api.StacksNetwork)
			return !ok || !equal(old.Status.Bitcoin, current.Status.Bitcoin)
		case *bitcoin.BitcoinExecution:
			current, ok := b.(*bitcoin.BitcoinExecution)
			return !ok || BitcoinExecutionChanged(old, current)
		case *bitcoin.BitcoinInitialization:
			current, ok := b.(*bitcoin.BitcoinInitialization)
			return !ok || !equal(old.Spec, current.Spec) || !equal(old.Status, current.Status)
		}
		return false
	}}
}

// runtimeConditions excludes admission projections and condition timestamp-only rewrites.
func runtimeConditions(conditions []metav1.Condition) []metav1.Condition {
	var result []metav1.Condition
	for _, condition := range conditions {
		if condition.Type != "Resolved" && condition.Type != "PolicyDeferred" && condition.Type != "ConfigVerified" {
			condition.LastTransitionTime = metav1.Time{}
			result = append(result, condition)
		}
	}
	slices.SortFunc(result, func(a, b metav1.Condition) int { return cmp.Compare(a.Type, b.Type) })
	return result
}

// gateCompletion routes durable gate transitions without routing timing observations.
func gateCompletion(state *api.InitializationStatus) *gateCompletionState {
	if state == nil {
		return nil
	}
	completed := make([]bool, len(state.Gates))
	for i, gate := range state.Gates {
		completed[i] = gate.CompletedAt != nil
	}
	return &gateCompletionState{string(state.GenesisUID), state.GenesisDigest, state.GateIndex, state.Completed, completed}
}

// gateCompletionState contains only admission-relevant initialization transitions.
type gateCompletionState struct {
	genesisUID    string
	genesisDigest string
	gateIndex     int32
	completed     bool
	gates         []bool
}

// BitcoinExecutionChanged retains control, identity, receipt and value changes while omitting read timestamps.
// Consumers must independently schedule freshness expiry and recovery evaluation.
func BitcoinExecutionChanged(old, current *bitcoin.BitcoinExecution) bool {
	if !equal(old.Spec, current.Spec) {
		return true
	}
	a, b := old.Status.DeepCopy(), current.Status.DeepCopy()
	if a.Observation != nil {
		a.Observation.ObservedAt = metav1.Time{}
	}
	if b.Observation != nil {
		b.Observation.ObservedAt = metav1.Time{}
	}
	return !equal(a, b)
}
