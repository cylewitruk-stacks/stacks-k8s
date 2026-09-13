package participantworkload

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload/stacksconfig"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// stacksConfiguration resolves public topology before provisioning scoped private rendering.
func (r *Reconciler) stacksConfiguration(ctx context.Context, root *api.StacksNetwork, p *api.StacksNetworkParticipant, state *api.ParticipantRuntimeStatus) (bool, bool, error) {
	in, err := r.stacksInput(ctx, root, p, state)
	if err != nil {
		return false, false, err
	}
	// Artifact names use public parameters before adding their own assigned identities.
	revision := digest(in)
	configPin := state.ConfigRef
	if state.ConfigurationDigest != revision {
		configPin = nil
	}
	config, err := r.emptySecret(ctx, p, "config-"+strings.TrimPrefix(revision, "sha256:"), configPin)
	if err != nil {
		return false, false, err
	}
	report := &corev1.ConfigMap{ObjectMeta: objectMeta(p, "report-"+strings.TrimPrefix(revision, "sha256:"), "support")}
	if err := r.createOwned(ctx, p, report); err != nil {
		return false, false, err
	}
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(report), report); err != nil {
		return false, false, err
	}
	in.Config = *config
	in.Report = *binding("ConfigMap", report)
	state.ConfigRef = config
	state.ConfigurationDigest = revision
	state.PolicyDigest = in.PolicyDigest
	if raw := report.Data["report.json"]; raw != "" {
		var result StacksConfigReport
		if len(raw) > 4096 || json.Unmarshal([]byte(raw), &result) != nil || result.InputDigest != digest(in) || result.GenesisDigest != in.Genesis.Fingerprint || result.Identity != in.Identity || !strings.HasPrefix(result.ConfigDigest, "sha256:") {
			return false, false, fmt.Errorf("invalid public Stacks configuration report")
		}
		if !result.Verified && (in.Customization == nil || ptr.Deref(in.Customization.Compatibility, common.CompatibilityManaged) != common.CompatibilityUnverified) {
			return false, false, fmt.Errorf("managed configuration agreement unavailable")
		}
		state.ConfigRef.Fingerprint = result.ConfigDigest
		return true, result.Verified, nil
	}
	data, _ := json.Marshal(in)
	if len(data) > stacksconfig.MaximumBytes {
		return false, false, fmt.Errorf("public Stacks render request exceeds bounded input size")
	}
	if raw := report.Data["input.json"]; raw != "" && raw != string(data) {
		return false, false, fmt.Errorf("public render input snapshot changed")
	} else if raw == "" {
		base := report.DeepCopy()
		if report.Data == nil {
			report.Data = map[string]string{}
		}
		report.Data["input.json"] = string(data)
		if err := r.Client.Patch(ctx, report, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return false, false, err
		}
	}
	var placement *common.Placement
	if root.Spec.Defaults != nil {
		placement = root.Spec.Defaults.WorkerPlacement
	}
	mode := "resolve-stacks-config"
	if in.CandidateDigest != "" {
		mode = "validate-stacks-config"
	}
	return false, false, r.provisionConfigurationJob(ctx, p, revision, mode, data, StacksConfigRules(in), placement, report.Name)
}

// stacksInput reads public identities and Secret metadata, never private actor bytes.
func (r *Reconciler) stacksInput(ctx context.Context, root *api.StacksNetwork, p *api.StacksNetworkParticipant, state *api.ParticipantRuntimeStatus) (StacksConfigInput, error) {
	in := StacksConfigInput{Namespace: p.Namespace, ParticipantUID: p.UID, Kind: p.Spec.Kind, PolicyDigest: p.Status.Admission.PolicyDigest}
	var genesis api.StacksGenesis
	if r.candidateConfiguration != nil {
		genesis.Spec = r.candidateConfiguration.Genesis
		in.CandidateDigest = candidateConfigurationDigest(*r.candidateConfiguration)
		in.Genesis.Fingerprint = digest(genesis.Spec.Chain)
	} else {
		if root.Status.GenesisRef == nil {
			return in, fmt.Errorf("frozen genesis binding missing")
		}
		if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: root.Status.GenesisRef.Name}, &genesis); err != nil {
			return in, err
		}
		if genesis.UID != root.Status.GenesisRef.UID || genesis.DeletionTimestamp != nil || digest(genesis.Spec.Chain) != root.Status.GenesisDigest {
			return in, fmt.Errorf("frozen genesis identity changed")
		}
		in.Genesis = *binding("StacksGenesis", &genesis)
		in.Genesis.Fingerprint = root.Status.GenesisDigest
	}
	fields, err := actorFields(p)
	if err != nil {
		return in, err
	}
	in.Customization = fields.Config
	var accountRef *common.NameRef
	if p.Spec.Kind == api.ParticipantStacksNode {
		node := p.Status.Admission.Configuration.StacksNode
		accountRef = node.IdentityAccountRef
		btc, err := r.boundParticipant(ctx, root, p, node.BitcoinNodeRef, api.ParticipantBitcoinNode)
		if err != nil {
			return in, err
		}
		// Completed public rendering binds immutable credentials without requiring actor startup.
		btcRuntime := btc.Status.Runtime
		if btcRuntime == nil || btcRuntime.ActorRPCSecretRef == nil || btcRuntime.ConfigRef == nil || btcRuntime.ConfigRef.Fingerprint == "" || btcRuntime.ConfigurationDigest == "" || btc.Status.Admission == nil || btcRuntime.PolicyDigest != btc.Status.Admission.PolicyDigest {
			return in, fmt.Errorf("Bitcoin actor credential configuration is not resolved")
		}
		actorRPC := *btcRuntime.ActorRPCSecretRef
		if state.ActorRPCSecretRef != nil && (state.ActorRPCSecretRef.Name != actorRPC.Name || state.ActorRPCSecretRef.UID != actorRPC.UID) {
			return in, fmt.Errorf("Bitcoin actor credential identity changed")
		}
		if err := r.privateMetadata(ctx, p.Namespace, actorRPC, btc.UID); err != nil {
			return in, err
		}
		in.ActorRPC = &PrivateInput{Binding: actorRPC, OwnerUID: btc.UID}
		state.ActorRPCSecretRef = &actorRPC
		if state.EventAuthSecretRef, err = r.emptySecret(ctx, p, "event-auth", state.EventAuthSecretRef); err != nil {
			return in, err
		}
		in.EventAuth = PrivateInput{Binding: *state.EventAuthSecretRef, OwnerUID: p.UID, Key: "token"}
		p2p, err := r.allocatedService(ctx, p, "p2p")
		if err != nil {
			return in, err
		}
		rpc, err := r.allocatedService(ctx, p, "rpc")
		if err != nil {
			return in, err
		}
		in.ServiceBindings = append(in.ServiceBindings, *binding("Service", p2p), *binding("Service", rpc))
		in.Node = stacksconfig.NodeParameters{Name: p.Spec.ParticipantName, Chain: genesis.Spec.Chain, P2PAddress: p2p.Spec.ClusterIP, RPCHost: rpc.Spec.ClusterIP, BitcoinHost: serviceHost(btc, "p2p")}
		in.Node.BootstrapPeers, err = r.stacksSeedSnapshot(ctx, root, p)
		if err != nil {
			return in, err
		}
		in.Node.SignerHost, err = r.pairedSignerHost(ctx, root, p)
		if err != nil {
			return in, err
		}
		if node.Mining != nil {
			in.Node.Mining = ptr.Deref(node.Mining.Enabled, false)
			in.Node.FeeRateSatsPerVByte = ptr.Deref(node.Mining.FeeRateSatsPerVByte, 2)
			if in.Node.Mining {
				wallet, err := r.minerWallet(ctx, p, node.Mining.BitcoinWalletRef)
				if err != nil {
					return in, err
				}
				in.Node.WalletName = ptr.Deref(wallet.Spec.WalletName, wallet.Name)
				if wallet.Spec.KeySource == nil || wallet.Spec.KeySource.StacksMinerAccountRef == nil || accountRef == nil || wallet.Spec.KeySource.StacksMinerAccountRef.Name != accountRef.Name {
					return in, fmt.Errorf("miner wallet does not derive from node identity account")
				}
			}
		}
	} else {
		signer := p.Status.Admission.Configuration.StacksSigner
		accountRef = signer.AccountRef
		node, err := r.boundParticipant(ctx, root, p, signer.NodeRef, api.ParticipantStacksNode)
		if err != nil {
			return in, err
		}
		if node.Status.Runtime == nil || node.Status.Runtime.EventAuthSecretRef == nil || node.Status.Runtime.ConfigRef == nil || node.Status.Runtime.ConfigRef.Fingerprint == "" {
			return in, fmt.Errorf("paired node event authentication unavailable")
		}
		ref := *node.Status.Runtime.EventAuthSecretRef
		if state.EventAuthSecretRef != nil && (state.EventAuthSecretRef.Name != ref.Name || state.EventAuthSecretRef.UID != ref.UID) {
			return in, fmt.Errorf("paired event authentication identity changed")
		}
		if err := r.privateMetadata(ctx, p.Namespace, ref, node.UID); err != nil {
			return in, err
		}
		state.EventAuthSecretRef = &ref
		in.EventAuth = PrivateInput{Binding: ref, OwnerUID: node.UID, Key: "token"}
		in.NodeHost = serviceHost(node, "rpc")
	}
	account, err := r.boundAccount(ctx, p, accountRef)
	if err != nil {
		return in, err
	}
	in.Identity = *account.Status.Identity
	in.Key = PrivateInput{Binding: common.Binding{Kind: "Secret", Name: account.Status.CredentialsRef.Name, UID: account.Status.CredentialsUID}, Key: account.Status.CredentialsRef.Key}
	if err := r.privateMetadata(ctx, p.Namespace, in.Key.Binding, ""); err != nil {
		return in, err
	}
	if in.Customization != nil {
		if ref := in.Customization.SecretRef; ref != nil {
			pin, ok := admittedBinding(p, "Secret", ref.Name)
			if !ok {
				return in, fmt.Errorf("custom configuration Secret was not admitted")
			}
			if err := r.privateMetadata(ctx, p.Namespace, pin, ""); err != nil {
				return in, err
			}
			in.Custom = &PrivateInput{Binding: pin, Key: ref.Key}
		}
		in.Services, err = r.customServiceHosts(ctx, root, p, in.Customization.ServiceRefs)
		if err != nil {
			return in, err
		}
	}
	return in, nil
}

// serviceHost is the stable DNS name for a participant-owned endpoint.
func serviceHost(p *api.StacksNetworkParticipant, endpoint string) string {
	return Name(p, endpoint) + "." + p.Namespace + ".svc"
}

// allocatedService verifies the participant-owned address before advertising it to native peers.
func (r *Reconciler) allocatedService(ctx context.Context, p *api.StacksNetworkParticipant, endpoint string) (*corev1.Service, error) {
	var service corev1.Service
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: Name(p, endpoint)}, &service); err != nil {
		return nil, err
	}
	if !owned(&service, p) || service.DeletionTimestamp != nil || net.ParseIP(service.Spec.ClusterIP) == nil {
		return nil, fmt.Errorf("allocated Stacks %s Service unavailable", endpoint)
	}
	return &service, nil
}

// privateMetadata checks pinned identity without decoding Secret data.
func (r *Reconciler) privateMetadata(ctx context.Context, namespace string, ref common.Binding, ownerUID types.UID) error {
	if ref.Kind != "Secret" || ref.UID == "" {
		return fmt.Errorf("private input binding missing")
	}
	obj := &metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}}
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, obj); err != nil {
		return err
	}
	if obj.UID != ref.UID || obj.DeletionTimestamp != nil || (ownerUID != "" && !resolverOwned(obj, ownerUID)) {
		return fmt.Errorf("private input identity changed")
	}
	return nil
}

// admittedBinding selects one exact captured dependency.
func admittedBinding(p *api.StacksNetworkParticipant, kind, name string) (common.Binding, bool) {
	if p.Status.Admission != nil {
		for _, ref := range p.Status.Admission.Dependencies {
			if ref.Kind == kind && ref.Name == name {
				return ref, true
			}
		}
	}
	return common.Binding{}, false
}

// boundParticipant verifies the current selected identity behind a mandatory participant ref.
func (r *Reconciler) boundParticipant(ctx context.Context, root *api.StacksNetwork, p *api.StacksNetworkParticipant, ref *common.NameRef, kind api.ParticipantKind) (*api.StacksNetworkParticipant, error) {
	if ref == nil {
		return nil, fmt.Errorf("mandatory participant reference missing")
	}
	name := foundation.ParticipantName(string(root.UID), ref.Name)
	pin, ok := admittedBinding(p, "StacksNetworkParticipant", name)
	if !ok {
		return nil, fmt.Errorf("mandatory participant was not admitted")
	}
	var target api.StacksNetworkParticipant
	if err := r.readConfigurationParticipant(ctx, client.ObjectKey{Namespace: root.Namespace, Name: name}, &target); err != nil {
		return nil, err
	}
	if target.UID != pin.UID || target.Spec.Kind != kind || target.DeletionTimestamp != nil || !selected(root, &target) {
		return nil, fmt.Errorf("mandatory participant identity changed")
	}
	if r.candidateConfiguration == nil {
		if err := foundation.ValidateParticipantAdmission(ctx, r.Reader, root, &target); err != nil {
			return nil, err
		}
	}
	return &target, nil
}

// boundAccount reads an admitted reusable public identity and exact credential metadata.
func (r *Reconciler) boundAccount(ctx context.Context, p *api.StacksNetworkParticipant, ref *common.NameRef) (*stacks.StacksAccount, error) {
	if ref == nil {
		return nil, fmt.Errorf("actor account is required")
	}
	pin, ok := admittedBinding(p, "StacksAccount", ref.Name)
	if !ok {
		return nil, fmt.Errorf("actor account was not admitted")
	}
	var account stacks.StacksAccount
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: ref.Name}, &account); err != nil {
		return nil, err
	}
	if account.UID != pin.UID || account.DeletionTimestamp != nil || account.Status.Digest != pin.Fingerprint || account.Status.Identity == nil || account.Status.CredentialsRef == nil || account.Status.CredentialsUID == "" {
		return nil, fmt.Errorf("public actor key agreement unavailable")
	}
	return &account, nil
}

// minerWallet reads the exact wallet identity admitted for native miner spending.
func (r *Reconciler) minerWallet(ctx context.Context, p *api.StacksNetworkParticipant, ref *common.NameRef) (*bitcoin.BitcoinWallet, error) {
	if ref == nil {
		return nil, fmt.Errorf("miner wallet is required")
	}
	pin, ok := admittedBinding(p, "BitcoinWallet", ref.Name)
	if !ok {
		return nil, fmt.Errorf("miner wallet was not admitted")
	}
	var wallet bitcoin.BitcoinWallet
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: ref.Name}, &wallet); err != nil {
		return nil, err
	}
	if wallet.UID != pin.UID || wallet.DeletionTimestamp != nil || wallet.Status.Digest != pin.Fingerprint {
		return nil, fmt.Errorf("miner wallet identity changed")
	}
	return &wallet, nil
}

// customServiceHosts resolves only declared same-namespace fixed-port endpoint aliases.
func (r *Reconciler) customServiceHosts(ctx context.Context, root *api.StacksNetwork, p *api.StacksNetworkParticipant, refs []common.ServiceRef) (map[string]string, error) {
	hosts := map[string]string{}
	for _, ref := range refs {
		if _, ok := hosts[ref.Alias]; ok || ref.Alias == "" {
			return nil, fmt.Errorf("duplicate or empty Service alias")
		}
		kind := api.ParticipantKind(ref.Kind)
		if !((kind == api.ParticipantBitcoinNode || kind == api.ParticipantStacksNode) && (ref.Endpoint == "rpc" || ref.Endpoint == "p2p") || kind == api.ParticipantStacksSigner && ref.Endpoint == "events") {
			return nil, fmt.Errorf("unsupported Service endpoint")
		}
		target, err := r.boundParticipant(ctx, root, p, &common.NameRef{Name: ref.Name}, kind)
		if err != nil {
			return nil, err
		}
		hosts[ref.Alias] = serviceHost(target, ref.Endpoint)
	}
	return hosts, nil
}

// pairedSignerHost discovers the currently admitted single consensus attachment without readiness cycles.
func (r *Reconciler) pairedSignerHost(ctx context.Context, root *api.StacksNetwork, p *api.StacksNetworkParticipant) (string, error) {
	host := ""
	for _, entry := range root.Spec.Participants {
		if entry.Kind != api.ParticipantStacksSigner {
			continue
		}
		var signer api.StacksNetworkParticipant
		if err := r.readConfigurationParticipant(ctx, client.ObjectKey{Namespace: p.Namespace, Name: foundation.ParticipantName(string(root.UID), entry.Name)}, &signer); err != nil {
			return "", err
		}
		if signer.Status.Admission == nil || signer.Status.Admission.Configuration.StacksSigner == nil || !selected(root, &signer) {
			continue
		}
		ref := signer.Status.Admission.Configuration.StacksSigner.NodeRef
		if cfg := signer.Status.Admission.Configuration.StacksSigner.Config; cfg != nil && ptr.Deref(cfg.Compatibility, common.CompatibilityManaged) == common.CompatibilityUnverified {
			continue
		}
		if ref == nil || ref.Name != p.Spec.ParticipantName {
			continue
		}
		if r.candidateConfiguration == nil {
			if err := foundation.ValidateParticipantAdmission(ctx, r.Reader, root, &signer); err != nil {
				return "", err
			}
		}
		if host != "" {
			return "", fmt.Errorf("multiple consensus signers target one node")
		}
		host = serviceHost(&signer, "events")
	}
	return host, nil
}

// stacksSeedSnapshot preserves startup hints when peers later join or disappear.
func (r *Reconciler) stacksSeedSnapshot(ctx context.Context, root *api.StacksNetwork, p *api.StacksNetworkParticipant) ([]string, error) {
	revision := p.Status.Admission.PolicyDigest
	if r.candidateConfiguration != nil {
		revision = candidateConfigurationDigest(*r.candidateConfiguration)
	}
	snapshot := &corev1.ConfigMap{ObjectMeta: objectMeta(p, "seeds-"+strings.TrimPrefix(revision, "sha256:"), "support")}
	if err := r.createOwned(ctx, p, snapshot); err != nil {
		return nil, err
	}
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(snapshot), snapshot); err != nil {
		return nil, err
	}
	if raw := snapshot.Data["seeds.json"]; raw != "" {
		var seeds []string
		if len(raw) > 65536 || json.Unmarshal([]byte(raw), &seeds) != nil {
			return nil, fmt.Errorf("invalid seed snapshot")
		}
		return seeds, nil
	}
	wanted := map[string]bool{}
	node := p.Status.Admission.Configuration.StacksNode
	if node.Peers != nil && node.Peers.NodeRefs != nil {
		for _, ref := range *node.Peers.NodeRefs {
			wanted[ref.Name] = true
		}
	} else {
		for _, entry := range root.Spec.Participants {
			if entry.Kind == api.ParticipantStacksNode {
				wanted[entry.Name] = true
			}
		}
	}
	delete(wanted, p.Spec.ParticipantName)
	names := make([]string, 0, len(wanted))
	for name := range wanted {
		names = append(names, name)
	}
	sort.Strings(names)
	seeds := []string{}
	for _, name := range names {
		var peer api.StacksNetworkParticipant
		if err := r.readConfigurationParticipant(ctx, client.ObjectKey{Namespace: p.Namespace, Name: foundation.ParticipantName(string(root.UID), name)}, &peer); err != nil {
			return nil, err
		}
		if peer.Spec.Kind != api.ParticipantStacksNode || peer.DeletionTimestamp != nil || !selected(root, &peer) {
			continue
		}
		if peer.Status.Admission == nil || peer.Status.Admission.Configuration.StacksNode == nil {
			return nil, fmt.Errorf("peer public identity is not admitted")
		}
		account, err := r.boundAccount(ctx, &peer, peer.Status.Admission.Configuration.StacksNode.IdentityAccountRef)
		if err != nil {
			return nil, err
		}
		seeds = append(seeds, account.Status.Identity.PublicKey+"@"+serviceHost(&peer, "p2p")+":20444")
	}
	data, _ := json.Marshal(seeds)
	base := snapshot.DeepCopy()
	snapshot.Data = map[string]string{"seeds.json": string(data)}
	if err := r.Client.Patch(ctx, snapshot, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return nil, err
	}
	return seeds, nil
}
