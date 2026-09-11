package foundation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Reconciler owns participant allocation, resolution and one genesis capture.
// It never creates actor workloads or authorizes protocol mutation.
type Reconciler struct {
	// Client performs optimistic writes of owned outputs and status.
	Client client.Client
	// Reader bypasses caches for current identity and freeze checks.
	Reader client.Reader
	// Scheme supplies ownership metadata.
	Scheme *runtime.Scheme
}

const foundationFinalizer = "network.stacks.org/foundation"

func condition(conditions *[]metav1.Condition, generation int64, kind string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(conditions, metav1.Condition{Type: kind, Status: status, Reason: reason, Message: message, ObservedGeneration: generation})
}
func (r *Reconciler) report(ctx context.Context, root *api.StacksNetwork, base *api.StacksNetwork, phase, reason, message string) (ctrl.Result, error) {
	root.Status.Phase = phase
	condition(&root.Status.Conditions, root.Generation, "Initialized", metav1.ConditionFalse, "RuntimeNotImplemented", "This delivery resolves and captures genesis; protocol initialization is not implemented")
	condition(&root.Status.Conditions, root.Generation, "Running", metav1.ConditionFalse, "RuntimeNotImplemented", "No actor or protocol workers are activated by the foundation")
	condition(&root.Status.Conditions, root.Generation, "Operational", metav1.ConditionFalse, "RuntimeNotImplemented", "Protocol health is not asserted")
	resolvedStatus := metav1.ConditionUnknown
	if phase == "ResolutionError" || phase == "Failed" {
		resolvedStatus = metav1.ConditionFalse
	} else if reason == "PausedBeforeInitialization" || reason == "GenesisCaptured" {
		resolvedStatus = metav1.ConditionTrue
	}
	condition(&root.Status.Conditions, root.Generation, "Resolved", resolvedStatus, reason, message)
	if !equal(base.Status, root.Status) {
		if err := r.Client.Status().Patch(ctx, root, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	// Retry unresolved inputs; settled states advance through watches only.
	if phase == "ResolutionError" {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	if reason == "Allocating" || reason == "Withdrawing" || reason == "GenesisRecovered" {
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// Reconcile advances only after owned identities and complete public inputs are durable.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var root api.StacksNetwork
	if err := r.Reader.Get(ctx, req.NamespacedName, &root); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if root.DeletionTimestamp != nil {
		return r.destroy(ctx, &root)
	}
	if !controllerutil.ContainsFinalizer(&root, foundationFinalizer) {
		base := root.DeepCopy()
		controllerutil.AddFinalizer(&root, foundationFinalizer)
		return ctrl.Result{Requeue: true}, r.Client.Patch(ctx, &root, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
	}
	base := root.DeepCopy()
	if root.Status.Phase == "Failed" {
		return ctrl.Result{}, nil
	}
	if root.Status.GenesisRef == nil {
		existing := &api.StacksGenesis{}
		key := types.NamespacedName{Namespace: root.Namespace, Name: RuntimeName(string(root.UID), "", "StacksGenesis", root.Name, "genesis")}
		err := r.Reader.Get(ctx, key, existing)
		if err == nil {
			if !ownedUID(existing, root.UID) || existing.Spec.Source.NetworkUID != root.UID || existing.DeletionTimestamp != nil {
				return r.report(ctx, &root, base, "Failed", "GenesisUnavailable", "Existing genesis is foreign or being removed")
			}
			for _, required := range existing.Spec.Bootstrap.Requirements {
				var p api.StacksNetworkParticipant
				if err := r.Reader.Get(ctx, types.NamespacedName{Namespace: root.Namespace, Name: required.Participant.Name}, &p); err != nil {
					if !apierrors.IsNotFound(err) {
						return ctrl.Result{}, err
					}
					return r.report(ctx, &root, base, "Failed", "BootstrapPolicyUnavailable", "Captured participant unavailable; recreate the network")
				}
				if p.UID != required.Participant.UID || p.Status.Admission == nil || p.Status.Admission.PolicyDigest != required.PolicyDigest {
					return r.report(ctx, &root, base, "Failed", "BootstrapPolicyUnavailable", "Captured initial policy unavailable; recreate the network")
				}
			}
			root.Status.GenesisRef = ptrBinding(binding("StacksGenesis", existing, Digest(existing.Spec)))
			root.Status.GenesisDigest = Digest(existing.Spec.Chain)
			root.Status.InputDigest = existing.Spec.Source.InputDigest
			return r.report(ctx, &root, base, "Initializing", "GenesisRecovered", "Recovered the immutable capture; current intent still requires resolution")
		}
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	var frozen *api.StacksGenesis
	if root.Status.GenesisRef != nil {
		frozen = &api.StacksGenesis{}
		err := r.Reader.Get(ctx, types.NamespacedName{Namespace: root.Namespace, Name: root.Status.GenesisRef.Name}, frozen)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			return r.report(ctx, &root, base, "Failed", "GenesisUnavailable", "Published genesis disappeared; recreate the network")
		}
		if frozen.UID != root.Status.GenesisRef.UID || !ownedUID(frozen, root.UID) || frozen.DeletionTimestamp != nil || Digest(frozen.Spec) != root.Status.GenesisRef.Fingerprint {
			return r.report(ctx, &root, base, "Failed", "GenesisUnavailable", "Published genesis identity or content changed; recreate the network")
		}
	}
	selected := map[string]api.Participant{}
	for _, entry := range root.Spec.Participants {
		selected[entry.Name] = entry
	}
	var resolutionErr error
	resolutionReason := "InputsUnavailable"
	issue := func(reason, message string) {
		if resolutionErr == nil {
			resolutionReason, resolutionErr = reason, fmt.Errorf("%s", message)
		}
	}
	// Record withdrawal before destruction; re-add cannot erase the removal decision.
	for i := range root.Status.Identities {
		id := &root.Status.Identities[i]
		if _, ok := selected[id.Name]; !ok && !id.Removing {
			id.Removing = true
			return r.report(ctx, &root, base, "Resolving", "Withdrawing", "Participant removal recorded before destructive cleanup")
		}
		if id.Removing {
			if err := r.deleteParticipant(ctx, &root, *id); err != nil {
				return ctrl.Result{}, err
			}
			if _, ok := selected[id.Name]; ok {
				issue("NameAlreadyUsed", "Removed participant names are single-use; choose a new name")
			}
		}
	}
	if root.Spec.Operation == "Stopped" {
		return r.report(ctx, &root, base, "Stopped", "Stopped", "Terminal stop; participant removals remain destructive")
	}
	all := map[string]*candidate{}
	instances := map[string]*api.StacksNetworkParticipant{}
	invalid := map[string]bool{}
	entries := append([]api.Participant(nil), root.Spec.Participants...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, entry := range entries {
		id := findIdentity(root.Status.Identities, entry.Name)
		if id != nil && id.Removing {
			continue
		}
		name := ParticipantName(string(root.UID), entry.Name)
		instance := &api.StacksNetworkParticipant{}
		err := r.Reader.Get(ctx, types.NamespacedName{Namespace: root.Namespace, Name: name}, instance)
		if apierrors.IsNotFound(err) {
			if id != nil {
				return r.report(ctx, &root, base, "Failed", "InstanceLost", "Recorded participant disappeared; replacement requires a fresh network")
			}
			if len(root.Status.Identities) >= 1000 {
				issue("InstanceLimit", "Network lifetime participant allocation limit reached")
				continue
			}
			instance = &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: root.Namespace}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: root.UID, ParticipantName: entry.Name, Kind: entry.Kind}}
			if err := createOwned(ctx, r.Client, r.Scheme, &root, instance); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		if err != nil {
			return ctrl.Result{}, err
		}
		if !ownedUID(instance, root.UID) || instance.Spec.NetworkUID != root.UID || instance.Spec.ParticipantName != entry.Name || instance.DeletionTimestamp != nil || id != nil && id.UID != instance.UID {
			return r.report(ctx, &root, base, "Failed", "InstanceIdentityChanged", "Participant identity changed or is foreign; recreate the network")
		}
		if instance.Spec.Kind != entry.Kind {
			const message = "Participant kind is immutable while its entry exists"
			issue("KindChanged", message)
			if err := r.participantStatus(ctx, instance, nil, "KindChanged", message); err != nil {
				return ctrl.Result{}, err
			}
			continue
		}
		if id == nil {
			root.Status.Identities = append(root.Status.Identities, api.InstanceIdentity{Name: entry.Name, UID: instance.UID})
			return r.report(ctx, &root, base, "Resolving", "Allocating", "Participant identity recorded before resolving instance-owned inputs")
		}
		instances[entry.Name] = instance
		// Controls are independent of candidate policy validity, including composition errors.
		if !equal(instance.Spec.Control, entry.Control) {
			previous := instance.DeepCopy()
			instance.Spec.Control = entry.Control.DeepCopy()
			if err := r.Client.Patch(ctx, instance, client.MergeFromWithOptions(previous, client.MergeFromWithOptimisticLock{})); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		c, err := r.compose(ctx, r.Client, &root, entry, instance)
		if err != nil {
			issue("InputsUnavailable", fmt.Sprintf("%s: %v", entry.Name, err))
			if err := r.participantStatus(ctx, instance, nil, "ResolutionError", err.Error()); err != nil {
				return ctrl.Result{}, err
			}
			continue
		}
		all[entry.Name] = c
	}
	// Validate the complete declaration graph, not cyclic readiness dependencies.
	for _, entry := range entries {
		c := all[entry.Name]
		if c == nil {
			continue
		}
		if err := c.validate(ctx, r.Client, all); err != nil {
			invalid[entry.Name] = true
			issue("InputsUnavailable", fmt.Sprintf("%s: %v", entry.Name, err))
			if err := r.participantStatus(ctx, c.instance, nil, "ResolutionError", err.Error()); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	for name, message := range topologyConflicts(all, instances, invalid) {
		invalid[name] = true
		issue("TopologyConflict", name+": "+message)
		if err := r.participantStatus(ctx, all[name].instance, nil, "TopologyConflict", message); err != nil {
			return ctrl.Result{}, err
		}
	}
	byUID := map[types.UID]string{}
	for name, c := range all {
		byUID[c.instance.UID] = name
	}
	// Mandatory dependencies inherit resolution failure, without blocking unrelated entries.
	for changed := true; changed; {
		changed = false
		for name, c := range all {
			if invalid[name] {
				continue
			}
			for _, dependency := range c.dependencies {
				if dependency.Kind != "StacksNetworkParticipant" {
					continue
				}
				if invalid[byUID[dependency.UID]] {
					invalid[name], changed = true, true
					if err := r.participantStatus(ctx, c.instance, nil, "ResolutionError", "A required participant is unresolved"); err != nil {
						return ctrl.Result{}, err
					}
					break
				}
			}
		}
	}
	dependencies := newDependencyCheck(r.Reader, &root)
	for _, entry := range entries {
		c := all[entry.Name]
		if c == nil || invalid[entry.Name] {
			continue
		}
		admissionReason, admissionMessage := "Admitted", "Complete public policy admitted; runtime not activated"
		admission := &api.Admission{PolicyDigest: Digest(c.configuration), Configuration: c.configuration, Dependencies: c.dependencies}
		if frozen != nil && !bitcoinBootstrapCompatible(c, frozen.Spec.Bootstrap) {
			message := "Bitcoin payout and initialization must match the frozen network, including for a new participant"
			issue("RequiresReplacement", entry.Name+": "+message)
			if err := r.participantStatus(ctx, c.instance, nil, "RequiresReplacement", message); err != nil {
				return ctrl.Result{}, err
			}
			continue
		}
		if prior := c.instance.Status.Admission; prior != nil && (!sameIdentities(prior.Dependencies, admission.Dependencies) || !sameProtectedConfiguration(c.instance.Spec.Kind, prior.Configuration, admission.Configuration)) {
			message := "A protected binding changed; use a new participant name"
			if c.instance.Spec.Kind == "BitcoinBlockProduction" {
				message = "Bitcoin payout or initialization binding changes require a fresh network"
			}
			issue("RequiresReplacement", entry.Name+": "+message)
			if err := r.participantStatus(ctx, c.instance, nil, "RequiresReplacement", message); err != nil {
				return ctrl.Result{}, err
			}
			continue
		}
		if frozen != nil {
			for _, required := range frozen.Spec.Bootstrap.Requirements {
				if required.Participant.UID == c.instance.UID && required.PolicyDigest != admission.PolicyDigest {
					if c.instance.Status.Admission == nil || c.instance.Status.Admission.PolicyDigest != required.PolicyDigest {
						return r.report(ctx, &root, base, "Failed", "BootstrapPolicyUnavailable", "Captured initial policy unavailable; recreate the network")
					}
					admission = c.instance.Status.Admission
					admissionReason, admissionMessage = "BootstrapPending", "Candidate policy deferred until initialization gates complete"
				}
			}
		}
		if err := dependencies.validate(ctx, admission.Dependencies); err != nil {
			issue("DependencyUnavailable", entry.Name+": "+err.Error())
			if err := r.participantStatus(ctx, c.instance, nil, "DependencyUnavailable", err.Error()); err != nil {
				return ctrl.Result{}, err
			}
			continue
		}
		old := c.instance.DeepCopy()
		c.instance.Spec.Source = c.source
		c.instance.Spec.Configuration = c.configuration
		if !equal(old.Spec, c.instance.Spec) {
			if err := r.Client.Patch(ctx, c.instance, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		previousStatus := c.instance.DeepCopy().Status
		if err := r.participantStatus(ctx, c.instance, admission, admissionReason, admissionMessage); err != nil {
			return ctrl.Result{}, err
		}
		if !equal(previousStatus, c.instance.Status) {
			return ctrl.Result{Requeue: true}, nil
		}
	}
	if resolutionErr != nil {
		return r.report(ctx, &root, base, "ResolutionError", resolutionReason, resolutionErr.Error())
	}
	if frozen != nil {
		return r.report(ctx, &root, base, "Initializing", "GenesisCaptured", "Immutable genesis captured; runtime activation is a later delivery slice")
	}
	spec, err := compileGenesis(ctx, r.Client, &root, all)
	if err != nil {
		return r.report(ctx, &root, base, "ResolutionError", "GenesisInputsUnavailable", err.Error())
	}
	root.Status.InputDigest = spec.Source.InputDigest
	if root.Spec.ExpectedInputDigest != nil && *root.Spec.ExpectedInputDigest != spec.Source.InputDigest {
		return r.report(ctx, &root, base, "ResolutionError", "InputDigestMismatch", "Resolved inputs differ from the expected digest")
	}
	if root.Spec.Operation == "Paused" {
		return r.report(ctx, &root, base, "Resolving", "PausedBeforeInitialization", "Inputs resolved; pause prevents genesis capture")
	}
	// Re-resolve public inputs with uncached reads immediately before freezing.
	fresh := map[string]*candidate{}
	for _, entry := range entries {
		c := all[entry.Name]
		var p api.StacksNetworkParticipant
		if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(c.instance), &p); err != nil {
			return ctrl.Result{}, err
		}
		if p.UID != c.instance.UID || p.DeletionTimestamp != nil || p.Status.Admission == nil || p.Status.Admission.PolicyDigest != Digest(c.configuration) {
			return ctrl.Result{Requeue: true}, nil
		}
		current, err := r.compose(ctx, r.Reader, &root, entry, &p)
		if err != nil {
			return ctrl.Result{}, err
		}
		fresh[entry.Name] = current
	}
	for name, c := range fresh {
		if err := c.validate(ctx, r.Reader, fresh); err != nil {
			return ctrl.Result{}, err
		}
		prior := all[name]
		if !equal(c.source, prior.source) || !equal(c.configuration, prior.configuration) || !equal(c.dependencies, prior.dependencies) {
			return ctrl.Result{Requeue: true}, nil
		}
	}
	currentSpec, err := compileGenesis(ctx, r.Reader, &root, fresh)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !equal(currentSpec, spec) {
		return ctrl.Result{Requeue: true}, nil
	}
	// A fresh boundary check must not reuse the earlier admission observations.
	captureDependencies := newDependencyCheck(r.Reader, &root)
	if err := captureDependencies.validate(ctx, currentSpec.Source.Dependencies); err != nil {
		return r.report(ctx, &root, base, "ResolutionError", "DependencyUnavailable", err.Error())
	}
	for _, required := range currentSpec.Bootstrap.Requirements {
		if err := captureDependencies.validate(ctx, required.Dependencies); err != nil {
			return r.report(ctx, &root, base, "ResolutionError", "DependencyUnavailable", err.Error())
		}
	}
	// Recheck the current root revision immediately before the one-way create.
	var current api.StacksNetwork
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(&root), &current); err != nil {
		return ctrl.Result{}, err
	}
	if current.UID != root.UID || current.ResourceVersion != root.ResourceVersion || current.DeletionTimestamp != nil {
		return ctrl.Result{Requeue: true}, nil
	}
	artifact := &api.StacksGenesis{ObjectMeta: metav1.ObjectMeta{Name: RuntimeName(string(root.UID), "", "StacksGenesis", root.Name, "genesis"), Namespace: root.Namespace}, Spec: spec}
	if err := controllerutil.SetControllerReference(&root, artifact, r.Scheme, controllerutil.WithBlockOwnerDeletion(false)); err != nil {
		return ctrl.Result{}, err
	}
	err = r.Client.Create(ctx, artifact)
	if err != nil {
		// A lost create acknowledgement is recovered by identity/content, never a second name.
		observed := &api.StacksGenesis{}
		if getErr := r.Reader.Get(ctx, client.ObjectKeyFromObject(artifact), observed); getErr != nil {
			return ctrl.Result{}, err
		}
		if !ownedUID(observed, root.UID) || observed.DeletionTimestamp != nil || !equal(observed.Spec, spec) {
			return r.report(ctx, &root, base, "Failed", "BootstrapPolicyUnavailable", "Existing freeze artifact differs from available initial policy; recreate the network")
		}
		artifact = observed
	}
	root.Status.GenesisRef = ptrBinding(binding("StacksGenesis", artifact, Digest(artifact.Spec)))
	root.Status.GenesisDigest = Digest(artifact.Spec.Chain)
	return r.report(ctx, &root, base, "Initializing", "GenesisCaptured", "Immutable genesis captured; runtime activation is a later delivery slice")
}

// bitcoinBootstrapCompatible preserves network-wide initialization identity across instance replacement.
func bitcoinBootstrapCompatible(c *candidate, bootstrap api.Bootstrap) bool {
	policy := c.configuration.BitcoinBlockProduction
	if policy == nil {
		return true
	}
	wallet := c.wallets[policy.PayoutWalletRef.Name]
	for _, required := range bootstrap.Requirements {
		if required.BitcoinInitialization == nil {
			continue
		}
		if wallet == nil || required.BitcoinPayoutWallet == nil || !equal(*required.BitcoinPayoutWallet, binding("BitcoinWallet", wallet, wallet.Status.Digest)) || !equal(required.BitcoinInitialization, policy.Initialization) {
			return false
		}
		// Other initialization wallet identities must also remain pinned.
		return sameIdentities(required.Dependencies, c.dependencies)
	}
	return false
}

func ptrBinding(b common.Binding) *common.Binding { return &b }
func findIdentity(ids []api.InstanceIdentity, name string) *api.InstanceIdentity {
	for i := range ids {
		if ids[i].Name == name {
			return &ids[i]
		}
	}
	return nil
}
func sameIdentities(a, b []common.Binding) bool {
	existing := map[string]common.Binding{}
	for _, binding := range a {
		existing[binding.Kind+"/"+binding.Name] = binding
	}
	for _, binding := range b {
		if prior, ok := existing[binding.Kind+"/"+binding.Name]; ok && (prior.UID != binding.UID || prior.Fingerprint != binding.Fingerprint) {
			return false
		}
	}
	return true
}

// sameProtectedConfiguration excludes mutable peer hints, schedules, traffic recipients and target selection.
func sameProtectedConfiguration(kind api.ParticipantKind, old, current api.Configuration) bool {
	paths := map[api.ParticipantKind][]string{
		"BitcoinNode":                 {"storage"},
		"BitcoinBlockProduction":      {"payoutWalletRef", "initialization"},
		"StacksNode":                  {"identityAccountRef", "bitcoinNodeRef", "mining.bitcoinWalletRef", "storage"},
		"StacksSigner":                {"accountRef", "nodeRef", "storage"},
		"StacksStacker":               {"holderAccountRef", "administratorAccountRef", "signerRef", "targetNodeRef"},
		"StacksFaucet":                {"accountRef", "targetNodeRef"},
		"StacksContractSet":           {"deployerAccountRef", "targetNodeRef"},
		"StacksTransactionProduction": {"accountRef", "targetNodeRef"},
	}
	oldMap, _ := objectMap(old)
	currentMap, _ := objectMap(current)
	value := func(object map[string]any, path string) any {
		var current any = object[branch(kind)]
		for _, key := range strings.Split(path, ".") {
			m, ok := current.(map[string]any)
			if !ok {
				return nil
			}
			current = m[key]
		}
		return current
	}
	for _, path := range paths[kind] {
		if !equal(value(oldMap, path), value(currentMap, path)) {
			return false
		}
	}
	return true
}
func (r *Reconciler) compose(ctx context.Context, reader client.Reader, root *api.StacksNetwork, entry api.Participant, instance *api.StacksNetworkParticipant) (*candidate, error) {
	var input any
	var err error
	var source api.Source
	var owner client.Object = instance
	if entry.Definition.Ref != nil {
		obj := DefinitionObject(entry.Kind)
		if obj == nil {
			return nil, fmt.Errorf("unsupported kind")
		}
		if err := reader.Get(ctx, types.NamespacedName{Namespace: root.Namespace, Name: entry.Definition.Ref.Name}, obj); err != nil || obj.GetDeletionTimestamp() != nil {
			return nil, fmt.Errorf("definition unavailable")
		}
		if instance.Spec.Source.UID != "" && instance.Spec.Source.UID != obj.GetUID() {
			return nil, fmt.Errorf("definition UID changed; participant replacement required")
		}
		input, err = sourceSpec(obj)
		source = api.Source{Name: obj.GetName(), UID: obj.GetUID(), Generation: obj.GetGeneration()}
		owner = obj
	} else {
		input, err = inlineSpec(entry)
		source.Generation = root.Generation
	}
	if err != nil {
		return nil, err
	}
	source.Digest = Digest(input)
	config, err := Compose(root, entry, input)
	if err != nil {
		return nil, err
	}
	if ref := defaultAccount(&config, owner); ref != nil {
		if err := EnsureDefaultAccount(ctx, r.Client, r.Scheme, owner, ref.Name); err != nil {
			return nil, err
		}
	}
	return &candidate{instance: instance, configuration: config, source: source}, nil
}
func (r *Reconciler) participantStatus(ctx context.Context, p *api.StacksNetworkParticipant, admission *api.Admission, reason, message string) error {
	base := p.DeepCopy()
	if admission != nil {
		p.Status.Admission = admission
	}
	state := metav1.ConditionTrue
	if reason == "ResolutionError" || reason == "RequiresReplacement" || reason == "KindChanged" || reason == "DependencyUnavailable" || reason == "TopologyConflict" {
		state = metav1.ConditionFalse
	}
	condition(&p.Status.Conditions, p.Generation, "Resolved", state, reason, message)
	deferred := metav1.ConditionFalse
	if reason == "BootstrapPending" {
		deferred = metav1.ConditionTrue
	}
	condition(&p.Status.Conditions, p.Generation, "PolicyDeferred", deferred, reason, message)
	if equal(base.Status, p.Status) {
		return nil
	}
	return r.Client.Status().Patch(ctx, p, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}
func (r *Reconciler) deleteParticipant(ctx context.Context, root *api.StacksNetwork, id api.InstanceIdentity) error {
	var p api.StacksNetworkParticipant
	if err := r.Reader.Get(ctx, types.NamespacedName{Namespace: root.Namespace, Name: ParticipantName(string(root.UID), id.Name)}, &p); err != nil {
		return client.IgnoreNotFound(err)
	}
	if p.UID != id.UID || !ownedUID(&p, root.UID) {
		return fmt.Errorf("refusing to delete foreign participant")
	}
	return client.IgnoreNotFound(r.Client.Delete(ctx, &p, client.Preconditions{UID: &id.UID}))
}
func (r *Reconciler) destroy(ctx context.Context, root *api.StacksNetwork) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(root, foundationFinalizer) {
		return ctrl.Result{}, nil
	}
	var participants api.StacksNetworkParticipantList
	if err := r.Reader.List(ctx, &participants, client.InNamespace(root.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	pending := false
	for i := range participants.Items {
		p := &participants.Items[i]
		if ownedUID(p, root.UID) {
			pending = true
			if err := r.Client.Delete(ctx, p, client.Preconditions{UID: &p.UID}); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
	}
	if pending {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	// GC removes owned immutable artifacts and resolver descendants; reusable definitions have no root owner.
	base := root.DeepCopy()
	controllerutil.RemoveFinalizer(root, foundationFinalizer)
	return ctrl.Result{}, r.Client.Patch(ctx, root, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// SetupWithManager watches public declarations; Secret notifications use metadata only.
func (r *Reconciler) SetupWithManager(m ctrl.Manager) error {
	enqueue := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: obj.GetNamespace(), Name: "network"}}}
	})
	builder := ctrl.NewControllerManagedBy(m).Named("foundation-network").For(&api.StacksNetwork{}).Owns(&api.StacksNetworkParticipant{}).Owns(&api.StacksGenesis{})
	for _, obj := range []client.Object{&bitcoin.BitcoinNode{}, &bitcoin.BitcoinWallet{}, &bitcoin.BitcoinBlockProduction{}, &bitcoin.BitcoinBlockSchedule{}, &stacks.StacksAccount{}, &stacks.StacksNode{}, &stacks.StacksSigner{}, &stacks.StacksStacker{}, &stacks.StacksFaucet{}, &stacks.StacksContractSet{}, &stacks.StacksTransactionProduction{}, &api.StacksEpochSchedule{}} {
		builder = builder.Watches(obj, enqueue)
	}
	return builder.WatchesMetadata(&metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}}, enqueue).Complete(r)
}
