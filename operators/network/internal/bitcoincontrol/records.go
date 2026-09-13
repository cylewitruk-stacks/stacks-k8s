// Package bitcoincontrol implements scoped v1alpha2 Bitcoin mutation authority.
package bitcoincontrol

import (
	"context"
	"fmt"
	"sort"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// EnsureRecords creates network-owned infrastructure and returns actual bindings.
// Only the aggregate caller persists these bindings in root status before activation.
func EnsureRecords(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	scheme *runtime.Scheme,
	root *api.StacksNetwork,
) ([]common.Binding, *common.Binding, error) {
	refs := []common.Binding{}
	var initRef *common.Binding
	if root.Status.Bitcoin != nil {
		refs = append(refs, root.Status.Bitcoin.ExecutionRefs...)
		initRef = root.Status.Bitcoin.InitializationRef
	}
	if root.UID == "" || root.DeletionTimestamp != nil {
		return refs, initRef, nil
	}
	for _, ref := range refs {
		var record bitcoin.BitcoinExecution
		if e := c.Get(
			ctx,
			client.ObjectKey{Namespace: root.Namespace, Name: ref.Name},
			&record,
		); e != nil || record.UID != ref.UID ||
			!metav1.IsControlledBy(&record, root) {
			return refs, initRef, fmt.Errorf("pinned Bitcoin execution record unavailable")
		}
	}
	if initRef != nil {
		var record bitcoin.BitcoinInitialization
		if e := c.Get(
			ctx,
			client.ObjectKey{Namespace: root.Namespace, Name: initRef.Name},
			&record,
		); e != nil || record.UID != initRef.UID ||
			!metav1.IsControlledBy(&record, root) {
			return refs, initRef, fmt.Errorf("pinned Bitcoin initialization unavailable")
		}
	}
	participants := &api.StacksNetworkParticipantList{}
	inventory := client.Reader(c)
	if initRef == nil {
		inventory = reader
	}
	if e := inventory.List(ctx, participants, client.InNamespace(root.Namespace), client.Limit(1001)); e != nil {
		return refs, initRef, e
	}
	if len(participants.Items) > 1000 || initRef == nil && participants.Continue != "" {
		return refs, initRef, fmt.Errorf("participant inventory incomplete")
	}
	for i := range participants.Items {
		p := &participants.Items[i]
		if p.Spec.Kind != api.ParticipantBitcoinNode || p.Spec.NetworkUID != root.UID || p.DeletionTimestamp != nil {
			continue
		}
		name := naming.RuntimeName(
			string(root.UID),
			string(p.UID),
			string(api.ParticipantBitcoinNode),
			p.Spec.ParticipantName,
			"execution",
		)
		if hasBinding(refs, name, "") {
			continue
		}
		uid := p.UID
		if e := reader.Get(
			ctx,
			client.ObjectKeyFromObject(p),
			p,
		); e != nil || p.UID != uid ||
			p.DeletionTimestamp != nil {
			continue
		}
		if e := foundation.ValidateParticipantAdmission(ctx, reader, root, p); e != nil {
			continue
		}
		record := &bitcoin.BitcoinExecution{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  root.Namespace,
				Labels:     labels(p, api.RoleSupport),
				Finalizers: []string{foundation.ArtifactFinalizer},
			},
			Spec: bitcoin.BitcoinExecutionSpec{NetworkUID: root.UID, Participant: objectref.Participant(p)},
		}
		if e := controllerutil.SetControllerReference(root, record, scheme); e != nil {
			return refs, initRef, e
		}
		current := &bitcoin.BitcoinExecution{}
		e := reader.Get(ctx, client.ObjectKeyFromObject(record), current)
		if apierrors.IsNotFound(e) {
			e = c.Create(ctx, record)
			if e != nil {
				return refs, initRef, e
			}
		} else {
			if e != nil {
				return refs, initRef, e
			}
			if !metav1.IsControlledBy(current, root) || !equality.Semantic.DeepEqual(current.Spec, record.Spec) ||
				current.DeletionTimestamp != nil {
				return refs, initRef, fmt.Errorf("execution record ownership differs")
			}
			record = current
		}
		refs = append(refs, objectref.BitcoinExecution(record))
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	if initRef != nil || root.Status.GenesisRef == nil {
		return refs, initRef, nil
	}
	genesis := &api.StacksGenesis{}
	if e := reader.Get(
		ctx,
		client.ObjectKey{Namespace: root.Namespace, Name: root.Status.GenesisRef.Name},
		genesis,
	); e != nil {
		return refs, nil, e
	}
	if genesis.UID != root.Status.GenesisRef.UID || genesis.Spec.Source.NetworkUID != root.UID ||
		!metav1.IsControlledBy(genesis, root) {
		return refs, nil, fmt.Errorf("frozen genesis identity unavailable")
	}
	spec, e := freezeInitialization(ctx, reader, root, genesis, participants.Items)
	if e != nil {
		return refs, nil, e
	}
	name := naming.RuntimeName(string(root.UID), "", bitcoin.KindBitcoinInitialization, "network", "initialization")
	record := &bitcoin.BitcoinInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  root.Namespace,
			Finalizers: []string{foundation.ArtifactFinalizer},
		},
		Spec: spec,
	}
	if e = controllerutil.SetControllerReference(root, record, scheme); e != nil {
		return refs, nil, e
	}
	current := &bitcoin.BitcoinInitialization{}
	e = reader.Get(ctx, client.ObjectKeyFromObject(record), current)
	if apierrors.IsNotFound(e) {
		if e = c.Create(ctx, record); e != nil {
			return refs, nil, e
		}
	} else {
		if e != nil {
			return refs, nil, e
		}
		if !metav1.IsControlledBy(current, root) || !equality.Semantic.DeepEqual(current.Spec, spec) ||
			current.DeletionTimestamp != nil {
			return refs, nil, fmt.Errorf("initialization record ownership differs")
		}
		record = current
	}
	b := objectref.BitcoinInitialization(record)
	return refs, &b, nil
}

// freezeInitialization preserves the full captured Bitcoin cohort without weakening genesis.
func freezeInitialization(
	ctx context.Context,
	reader client.Reader,
	root *api.StacksNetwork,
	genesis *api.StacksGenesis,
	participants []api.StacksNetworkParticipant,
) (bitcoin.BitcoinInitializationSpec, error) {
	spec := bitcoin.BitcoinInitializationSpec{NetworkUID: root.UID, Genesis: objectref.Genesis(genesis)}
	spec.Genesis.Fingerprint = foundation.Digest(genesis.Spec)
	if len(genesis.Spec.Bootstrap.Gates) == 0 || genesis.Spec.Bootstrap.Gates[0].Name != api.GatePrepareBitcoin {
		return spec, fmt.Errorf("first frozen Bitcoin gate unavailable")
	}
	byUID := map[types.UID]*api.StacksNetworkParticipant{}
	byName := map[string]*api.StacksNetworkParticipant{}
	for i := range participants {
		p := &participants[i]
		byUID[p.UID] = p
		byName[p.Spec.ParticipantName] = p
	}
	var production *api.BootstrapRequirement
	for i := range genesis.Spec.Bootstrap.Requirements {
		req := &genesis.Spec.Bootstrap.Requirements[i]
		p := byUID[req.Participant.UID]
		if p == nil || p.Name != req.Participant.Name {
			return spec, fmt.Errorf("captured participant missing")
		}
		if p.Spec.Kind == api.ParticipantBitcoinNode {
			spec.Nodes = append(spec.Nodes, req.Participant)
		}
		if req.BitcoinInitialization != nil {
			if production != nil {
				return spec, fmt.Errorf("multiple bootstrap production requirements")
			}
			production = req
		}
	}
	if production == nil || production.BitcoinPayoutWallet == nil {
		return spec, fmt.Errorf("captured production requirements missing")
	}
	initial := production.BitcoinInitialization
	target := byName[initial.TargetNodeRef.Name]
	if target == nil || target.Spec.Kind != api.ParticipantBitcoinNode {
		return spec, fmt.Errorf("initialization target unavailable")
	}
	spec.Production = production.Participant
	spec.Target = objectref.Participant(target)
	spec.MinimumHeight = initial.MinimumHeight
	spec.MatureOutputsPerMiner = initial.MatureOutputsPerMiner
	if spec.MinimumHeight != genesis.Spec.Bootstrap.Gates[0].BitcoinCeiling || initial.MatureOutputsPerMiner < 1 {
		return spec, fmt.Errorf("initialization differs from frozen ceiling")
	}
	resolve := func(ref common.Binding) (bitcoin.FrozenBitcoinWallet, error) {
		wallet := &bitcoin.BitcoinWallet{}
		if e := reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: ref.Name}, wallet); e != nil {
			return bitcoin.FrozenBitcoinWallet{}, e
		}
		if wallet.UID != ref.UID || wallet.Status.Digest != ref.Fingerprint || wallet.DeletionTimestamp != nil {
			return bitcoin.FrozenBitcoinWallet{}, fmt.Errorf("frozen wallet identity unavailable")
		}
		return publicWallet(wallet)
	}
	var e error
	spec.PayoutWallet, e = resolve(*production.BitcoinPayoutWallet)
	if e != nil {
		return spec, e
	}
	for _, name := range ptr.Deref(initial.MinerWalletRefs, nil) {
		var ref *common.Binding
		for i := range production.Dependencies {
			b := &production.Dependencies[i]
			if b.Kind == bitcoin.KindBitcoinWallet && b.Name == name.Name {
				ref = b
				break
			}
		}
		if ref == nil {
			return spec, fmt.Errorf("captured miner wallet binding unavailable")
		}
		wallet, e := resolve(*ref)
		if e != nil {
			return spec, e
		}
		spec.MinerWallets = append(spec.MinerWallets, wallet)
	}
	if len(spec.MinerWallets) == 0 ||
		100+int64(len(spec.MinerWallets))*int64(spec.MatureOutputsPerMiner) > spec.MinimumHeight {
		return spec, fmt.Errorf("frozen miner maturity requirement cannot fit first gate")
	}
	sort.Slice(
		spec.MinerWallets,
		func(i, j int) bool { return spec.MinerWallets[i].Wallet.Name < spec.MinerWallets[j].Wallet.Name },
	)
	return spec, nil
}

// publicWallet validates the supported public descriptor profile.
func publicWallet(w *bitcoin.BitcoinWallet) (bitcoin.FrozenBitcoinWallet, error) {
	if err := foundation.ValidateWalletProfile(w.Spec); err != nil {
		return bitcoin.FrozenBitcoinWallet{}, err
	}
	_, address, e := identity.FromDescriptor(w.Status.Descriptor)
	if e != nil || address != w.Status.BitcoinAddress || w.Status.Digest == "" {
		return bitcoin.FrozenBitcoinWallet{}, fmt.Errorf("public wallet descriptor identity unavailable")
	}
	name := ptr.Deref(w.Spec.WalletName, w.Name)
	if name == "" || len(name) > 253 {
		return bitcoin.FrozenBitcoinWallet{}, fmt.Errorf("invalid local wallet name")
	}
	return bitcoin.FrozenBitcoinWallet{
		Wallet:     objectref.WithFingerprint(objectref.BitcoinWallet(w), w.Status.Digest),
		Name:       name,
		Address:    address,
		Descriptor: w.Status.Descriptor,
	}, nil
}

// hasBinding checks a retained name and, when supplied, its exact UID.
func hasBinding(refs []common.Binding, name string, uid types.UID) bool {
	for _, b := range refs {
		if b.Name == name && (uid == "" || b.UID == uid) {
			return true
		}
	}
	return false
}

// labels matches the public participant runtime identity contract.
func labels(p *api.StacksNetworkParticipant, role string) map[string]string {
	return map[string]string{
		api.LabelManagedBy:       api.ManagedByNetworkOperator,
		api.LabelNetwork:         api.NetworkLabelValue,
		api.LabelNetworkUID:      string(p.Spec.NetworkUID),
		api.LabelParticipant:     p.Spec.ParticipantName,
		api.LabelParticipantUID:  string(p.UID),
		api.LabelParticipantKind: string(p.Spec.Kind),
		api.LabelRole:            role,
	}
}
