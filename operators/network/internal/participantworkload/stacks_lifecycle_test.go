package participantworkload

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// stacksParticipantFixture supplies an admitted native actor with explicit image selection.
func stacksParticipantFixture(kind api.ParticipantKind) *api.StacksNetworkParticipant {
	p := participantFixture()
	p.Name = "participant-" + strings.ToLower(string(kind))
	p.Spec.ParticipantName = "actor-" + strings.ToLower(string(kind))
	p.Spec.Kind = kind
	fields := common.ActorFields{Image: ptr.To("native-stacks:test"), Storage: &common.Storage{Size: ptr.To("5Gi"), RetainOnDelete: ptr.To(true)}, Placement: &common.Placement{NodeSelector: map[string]string{"pool": "actors"}}}
	configuration := api.Configuration{StacksNode: &stacks.StacksNodeSpec{ActorFields: fields}}
	if kind == "StacksSigner" {
		configuration = api.Configuration{StacksSigner: &stacks.StacksSignerSpec{ActorFields: fields}}
	}
	p.Spec.Configuration = configuration
	p.Status.Admission = &api.Admission{Configuration: configuration, PolicyDigest: digest(configuration)}
	return p
}

// nativeRuntimeFixture supplies public artifact bindings without credential data.
func nativeRuntimeFixture() *api.ParticipantRuntimeStatus {
	return &api.ParticipantRuntimeStatus{ConfigRef: &common.Binding{Kind: "Secret", Name: "config", UID: "config-uid"}, ConfigurationDigest: "sha256:configuration", EventAuthSecretRef: &common.Binding{Kind: "Secret", Name: "node-event-auth", UID: "event-uid"}}
}

func TestStacksActorsShareStorageAndTerminationBoundary(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []api.ParticipantKind{"StacksNode", "StacksSigner"} {
		t.Run(string(kind), func(t *testing.T) {
			p := stacksParticipantFixture(kind)
			state := nativeRuntimeFixture()
			workload, err := actorWorkload(p, state)
			if err != nil {
				t.Fatal(err)
			}
			workload.UID = "workload-uid"
			pod := workload.Spec.Template.Spec
			if ptr.Deref(pod.AutomountServiceAccountToken, true) || pod.NodeSelector["pool"] != "actors" || len(pod.InitContainers) != 1 || pod.InitContainers[0].Name != "config-check" || !strings.Contains(pod.InitContainers[0].Command[2], ">/dev/null 2>&1") {
				t.Fatal("native execution or private validation boundary missing")
			}
			if len(pod.Containers[0].Env) != 0 || pod.Containers[0].Name != actorContainer(kind) {
				t.Fatal("native actor received another role's credential")
			}
			mounts := map[string]string{"config": "/config", "data": "/data", "tmp": "/tmp", "event-auth": "/event-auth"}
			for _, mount := range pod.Containers[0].VolumeMounts {
				path, allowed := mounts[mount.Name]
				if !allowed || mount.MountPath != path || ((mount.Name == "config" || mount.Name == "event-auth") && !mount.ReadOnly) {
					t.Fatal("unexpected native actor mount or writable credential")
				}
				delete(mounts, mount.Name)
			}
			if len(mounts) != 0 {
				t.Fatal("native actor mount missing")
			}
			retention := workload.Spec.PersistentVolumeClaimRetentionPolicy
			if retention.WhenScaled != appsv1.RetainPersistentVolumeClaimRetentionPolicyType || retention.WhenDeleted != appsv1.RetainPersistentVolumeClaimRetentionPolicyType {
				t.Fatal("Stacks actor lost shared PVC policy")
			}
			services := Services(p)
			if kind == "StacksNode" && (services[0].Spec.ClusterIP == corev1.ClusterIPNone || services[0].Spec.Ports[0].Port != 20444) {
				t.Fatal("node needs stable allocated native P2P address")
			}
			if kind == "StacksSigner" && (len(services) != 1 || services[0].Spec.Ports[0].Port != 30000) {
				t.Fatal("consensus event service mismatch")
			}
			c := testClient(t, workload)
			r := Reconciler{Client: c, Reader: c, Kind: kind}
			if _, err := r.shutdown(ctx, p, state, false); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(workload), workload); err != nil {
				t.Fatal(err)
			}
			if *workload.Spec.Replicas != 0 || state.Terminated {
				t.Fatal("suspension must scale without inventing termination")
			}
			observed := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-uid", Labels: Labels(p, "actor")}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: actorContainer(kind), ContainerID: "containerd://native", ImageID: "sha256:native"}}}}
			observePod(state, observed)
			if state.ContainerID != "containerd://native" || state.ImageID != "sha256:native" {
				t.Fatal("native process identity lost")
			}
			observed.UID = "next-pod"
			observed.Status.ContainerStatuses = nil
			observePod(state, observed)
			if state.ContainerID != "" || state.ImageID != "" {
				t.Fatal("new Pod inherited old process evidence")
			}
			other := stacksParticipantFixture(kind)
			other.UID = "other-uid"
			before, err := actorWorkload(other, nativeRuntimeFixture())
			if err != nil {
				t.Fatal(err)
			}
			fields, _ := actorFields(p)
			fields.Image = ptr.To("native-stacks:changed")
			after, err := actorWorkload(p, state)
			if err != nil {
				t.Fatal(err)
			}
			unaffected, err := actorWorkload(other, nativeRuntimeFixture())
			if err != nil || !reflect.DeepEqual(before, unaffected) || after.Spec.Template.Spec.Containers[0].Image != "native-stacks:changed" {
				t.Fatal("image change affected another actor")
			}
		})
	}
}

func TestStacksStartupRequiresExactPreparedRecordWithoutSignerEnrollment(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	p := stacksParticipantFixture("StacksSigner")
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: p.Namespace, UID: p.Spec.NetworkUID}, Status: api.StacksNetworkStatus{Phase: "Running", GenesisRef: &common.Binding{Kind: "StacksGenesis", Name: "genesis", UID: "genesis-uid"}}}
	init := &bitcoin.BitcoinInitialization{ObjectMeta: metav1.ObjectMeta{Name: "actual-initialization-record", Namespace: p.Namespace, UID: "initialization-uid", OwnerReferences: p.OwnerReferences}, Spec: bitcoin.BitcoinInitializationSpec{NetworkUID: root.UID, Genesis: *root.Status.GenesisRef}}
	root.Status.Bitcoin = &api.BitcoinRuntimeStatus{InitializationRef: binding("BitcoinInitialization", init)}
	c := testClient(t, init)
	r := Reconciler{Client: c, Reader: c, Kind: "StacksSigner"}
	if err := r.stacksStartAuthorized(ctx, root, p, now); err == nil {
		t.Fatal("aggregate phase authorized startup without PrepareBitcoin")
	}
	init.Status.PreparedAt = &metav1.Time{Time: now}
	if err := c.Update(ctx, init); err != nil {
		t.Fatal(err)
	}
	if err := r.stacksStartAuthorized(ctx, root, p, now); err != nil {
		t.Fatalf("consensus startup incorrectly requires enrollment: %v", err)
	}
	root.Status.Bitcoin.InitializationRef.UID = "replacement"
	if err := r.stacksStartAuthorized(ctx, root, p, now); err == nil {
		t.Fatal("same-name preparation replacement authorized startup")
	}
}

func TestMinerActivationUsesFreshExactLocalWalletObservation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	p := stacksParticipantFixture("StacksNode")
	btc := participantFixture()
	btc.Name = foundation.ParticipantName(string(p.Spec.NetworkUID), btc.Spec.ParticipantName)
	btc.UID = "bitcoin-uid"
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: p.Namespace, UID: p.Spec.NetworkUID}, Spec: api.StacksNetworkSpec{Participants: []api.Participant{{Name: btc.Spec.ParticipantName, Kind: "BitcoinNode"}}}, Status: api.StacksNetworkStatus{Identities: []api.InstanceIdentity{{Name: btc.Spec.ParticipantName, UID: btc.UID}}, GenesisRef: &common.Binding{Kind: "StacksGenesis", Name: "genesis", UID: "genesis-uid"}}}
	wallet := &bitcoin.BitcoinWallet{ObjectMeta: metav1.ObjectMeta{Name: "miner-wallet", Namespace: p.Namespace, UID: "wallet-uid"}, Status: common.ResolutionStatus{Digest: "wallet-digest", BitcoinAddress: "miner-address"}}
	p.Status.Admission.Configuration.StacksNode.BitcoinNodeRef = &common.NameRef{Name: btc.Spec.ParticipantName}
	p.Status.Admission.Configuration.StacksNode.Mining = &stacks.Mining{Enabled: ptr.To(true), BitcoinWalletRef: &common.NameRef{Name: wallet.Name}}
	p.Status.Admission.Dependencies = []common.Binding{*binding("StacksNetworkParticipant", btc), {Kind: "BitcoinWallet", Name: wallet.Name, UID: wallet.UID, Fingerprint: wallet.Status.Digest}}
	init := &bitcoin.BitcoinInitialization{ObjectMeta: metav1.ObjectMeta{Name: "init", Namespace: p.Namespace, UID: "init-uid", OwnerReferences: p.OwnerReferences}, Spec: bitcoin.BitcoinInitializationSpec{NetworkUID: root.UID, Genesis: *root.Status.GenesisRef}, Status: bitcoin.BitcoinInitializationStatus{PreparedAt: &metav1.Time{Time: now}}}
	btc.Status.Runtime = &api.ParticipantRuntimeStatus{PolicyDigest: btc.Status.Admission.PolicyDigest, PodRef: &common.Binding{Kind: "Pod", Name: "bitcoin-pod", UID: "pod-uid"}, ContainerID: "containerd://bitcoin", ConfigRef: &common.Binding{Kind: "Secret", Name: "bitcoin-config", UID: "config-uid"}, RPCSecretRef: &common.Binding{Kind: "Secret", Name: "control", UID: "control-uid"}}
	execution := &bitcoin.BitcoinExecution{ObjectMeta: metav1.ObjectMeta{Name: "actual-execution", Namespace: p.Namespace, UID: "execution-uid", OwnerReferences: p.OwnerReferences}, Spec: bitcoin.BitcoinExecutionSpec{NetworkUID: root.UID, Participant: *binding("StacksNetworkParticipant", btc)}}
	execution.Status.Observation = &bitcoin.BitcoinObservation{ObservedAt: metav1.Time{Time: now}, Target: bitcoin.BitcoinTargetIdentity{Participant: *binding("StacksNetworkParticipant", btc), Pod: *btc.Status.Runtime.PodRef, ContainerID: btc.Status.Runtime.ContainerID, Configuration: *btc.Status.Runtime.ConfigRef, Credentials: *btc.Status.Runtime.RPCSecretRef, PolicyDigest: btc.Status.Runtime.PolicyDigest}, Wallets: []bitcoin.BitcoinWalletObservation{{Wallet: *binding("BitcoinWallet", wallet), Name: wallet.Name, Address: wallet.Status.BitcoinAddress, Ready: true, MatureOutputs: 1}}}
	root.Status.Bitcoin = &api.BitcoinRuntimeStatus{InitializationRef: binding("BitcoinInitialization", init), ExecutionRefs: []common.Binding{*binding("BitcoinExecution", execution)}}
	for _, failure := range []string{"", "stale", "process", "wallet", "immature"} {
		t.Run(failure, func(t *testing.T) {
			record := execution.DeepCopy()
			switch failure {
			case "stale":
				record.Status.Observation.ObservedAt.Time = now.Add(-11 * time.Second)
			case "process":
				record.Status.Observation.Target.ContainerID = "other-process"
			case "wallet":
				record.Status.Observation.Wallets[0].Wallet.UID = "other-wallet"
			case "immature":
				record.Status.Observation.Wallets[0].MatureOutputs = 0
			}
			c := testClient(t, btc, wallet, init, record)
			r := Reconciler{Client: c, Reader: c, Kind: "StacksNode"}
			if err := r.stacksStartAuthorized(ctx, root, p, now); (err != nil) != (failure != "") {
				t.Fatalf("funding gate: %v", err)
			}
		})
	}
}

// stacksInputFixture supplies exact public dependencies for native Stacks rendering.
func stacksInputFixture(t *testing.T) (client.Client, *api.StacksNetwork, *api.StacksNetworkParticipant, *api.StacksNetworkParticipant) {
	t.Helper()
	p := stacksParticipantFixture("StacksNode")
	btc := participantFixture()
	btc.Name = foundation.ParticipantName(string(p.Spec.NetworkUID), btc.Spec.ParticipantName)
	btc.UID = "bitcoin-uid"
	account := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "node-account", Namespace: p.Namespace, UID: "account-uid"}, Status: common.ResolutionStatus{Digest: "account-digest", Identity: &common.PublicIdentity{Address: "ST000000000000000000002AMW42H", PublicKey: "02public"}, CredentialsRef: &common.SecretKeyRef{Name: "private-account", Key: "privateKey"}, CredentialsUID: "key-uid"}}
	key := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: account.Status.CredentialsRef.Name, Namespace: p.Namespace, UID: account.Status.CredentialsUID}, Immutable: ptr.To(true), Data: map[string][]byte{"privateKey": []byte("must never enter controller")}}
	rpc := &corev1.Secret{ObjectMeta: objectMeta(btc, "rpc-actor", "support")}
	rpc.UID = "rpc-uid"
	btc.Status.Runtime = &api.ParticipantRuntimeStatus{ActorRPCSecretRef: binding("Secret", rpc), ConfigRef: &common.Binding{Kind: "Secret", Name: "bitcoin-config", UID: "bitcoin-config-uid", Fingerprint: "sha256:bitcoin-config"}, ConfigurationDigest: "sha256:bitcoin-input", PolicyDigest: btc.Status.Admission.PolicyDigest}
	event := &corev1.Secret{ObjectMeta: objectMeta(p, "event-auth", "support")}
	event.UID = "event-uid"
	p2p := Services(p)[0]
	p2p.UID = "service-uid"
	p2p.Spec.ClusterIP = "10.96.0.20"
	rpcService := Services(p)[1]
	rpcService.UID = "rpc-service-uid"
	rpcService.Spec.ClusterIP = "10.96.0.21"
	genesis := &api.StacksGenesis{ObjectMeta: metav1.ObjectMeta{Name: "genesis", Namespace: p.Namespace, UID: "genesis-uid"}}
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: p.Namespace, UID: p.Spec.NetworkUID}, Spec: api.StacksNetworkSpec{Participants: []api.Participant{{Name: p.Spec.ParticipantName, Kind: p.Spec.Kind}, {Name: btc.Spec.ParticipantName, Kind: btc.Spec.Kind}}}, Status: api.StacksNetworkStatus{Identities: []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: p.UID}, {Name: btc.Spec.ParticipantName, UID: btc.UID}}, GenesisRef: binding("StacksGenesis", genesis), GenesisDigest: digest(genesis.Spec.Chain)}}
	p.Status.Admission.Configuration.StacksNode.IdentityAccountRef = &common.NameRef{Name: account.Name}
	p.Status.Admission.Configuration.StacksNode.BitcoinNodeRef = &common.NameRef{Name: btc.Spec.ParticipantName}
	p.Status.Admission.Dependencies = []common.Binding{*binding("StacksNetworkParticipant", btc), {Kind: "StacksAccount", Name: account.Name, UID: account.UID, Fingerprint: account.Status.Digest}}
	c := testClient(t, p, btc, account, key, rpc, event, p2p, rpcService, genesis)
	return c, root, p, btc
}

func TestStacksPublicResolutionAvoidsSecretDataAndFreezesJoinSeeds(t *testing.T) {
	ctx := context.Background()
	c, root, p, btc := stacksInputFixture(t)
	r := Reconciler{Client: c, Reader: &denySecretData{Client: c}, Kind: "StacksNode"}
	state := api.ParticipantRuntimeStatus{}
	first, err := r.stacksInput(ctx, root, p, &state)
	if err != nil {
		t.Fatal(err)
	}
	if first.Key.Binding.UID != "key-uid" || first.ActorRPC.Binding.UID != "rpc-uid" || first.Node.BitcoinHost != serviceHost(btc, "p2p") || len(first.Node.BootstrapPeers) != 0 {
		t.Fatal("public binding or burnchain routing mismatch")
	}
	if first.Node.RPCHost != "10.96.0.21" || len(first.ServiceBindings) != 2 || first.ServiceBindings[1].UID != "rpc-service-uid" {
		t.Fatal("native RPC advertisement does not bind the allocated Service address")
	}
	root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: "late-peer", Kind: "StacksNode"})
	root.Status.Identities = append(root.Status.Identities, api.InstanceIdentity{Name: "late-peer", UID: "late-peer-uid"})
	next, err := r.stacksInput(ctx, root, p, &state)
	if err != nil || digest(next) != digest(first) {
		t.Fatalf("ordinary late join rolled existing actor config: %v", err)
	}
	state.ActorRPCSecretRef.UID = "replacement"
	if _, err := r.stacksInput(ctx, root, p, &state); err == nil {
		t.Fatal("controller silently rebound replaced RPC credentials")
	}
}

func TestStacksRPCAdvertisementRequiresAllocatedOwnedService(t *testing.T) {
	for _, mode := range []string{"missing", "foreign", "unallocated", "deleting"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			c, root, p, _ := stacksInputFixture(t)
			var service corev1.Service
			if err := c.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: Name(p, "rpc")}, &service); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "foreign":
				service.OwnerReferences[0].UID = "foreign"
			case "unallocated":
				service.Spec.ClusterIP = ""
			case "deleting":
				service.Finalizers = []string{"test.example/retained"}
			}
			if err := c.Update(ctx, &service); err != nil {
				t.Fatal(err)
			}
			if mode == "missing" || mode == "deleting" {
				if err := c.Delete(ctx, &service); err != nil {
					t.Fatal(err)
				}
			}
			r := Reconciler{Client: c, Reader: &denySecretData{Client: c}, Kind: "StacksNode"}
			if _, err := r.stacksInput(ctx, root, p, &api.ParticipantRuntimeStatus{}); err == nil {
				t.Fatal("unavailable RPC endpoint entered native configuration")
			}
		})
	}
}
