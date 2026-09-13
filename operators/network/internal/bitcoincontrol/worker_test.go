package bitcoincontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// fakeRPC models named wallet state and records every attempted mutation.
type fakeRPC struct {
	mu                       sync.Mutex
	height                   int64
	exists, loaded, imported bool
	calls                    []string
	extraLoaded              []string
	fail                     bool
	wait                     <-chan struct{}
	after                    func()
}

func (f *fakeRPC) Check(context.Context, string, string) error { return nil }
func (f *fakeRPC) Generate(ctx context.Context, endpoint, address, id string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "Generate")
	fail, wait, after := f.fail, f.wait, f.after
	f.mu.Unlock()
	if wait != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-wait:
		}
	}
	if fail {
		return "", fmt.Errorf("lost response")
	}
	f.mu.Lock()
	f.height++
	height := f.height
	f.mu.Unlock()
	if after != nil {
		after()
	}
	return fmt.Sprintf("%064x", height), nil
}
func (f *fakeRPC) Call(ctx context.Context, endpoint, id, method string, args []any, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var value any
	switch method {
	case "getblockchaininfo":
		value = map[string]any{"chain": "regtest", "blocks": f.height, "bestblockhash": fmt.Sprintf("%064x", f.height)}
	case "listwallets":
		loaded := append([]string{}, f.extraLoaded...)
		if f.loaded {
			loaded = append(loaded, "miner")
		}
		value = loaded
	case "listwalletdir":
		value = map[string]any{"wallets": []any{}}
		if f.exists {
			value = map[string]any{"wallets": []any{map[string]string{"name": "miner"}}}
		}
	case "getwalletinfo":
		value = map[string]any{"walletname": "miner", "private_keys_enabled": false, "descriptors": true}
	case "getdescriptorinfo":
		value = map[string]any{"descriptor": args[0].(string) + "#checksum", "hasprivatekeys": false}
	case "listdescriptors":
		value = map[string]any{"descriptors": []any{}}
		if f.imported {
			value = map[string]any{"descriptors": []any{map[string]string{"desc": testDescriptor + "#checksum"}}}
		}
	case "listunspent":
		value = []any{}
		if f.height >= 101 {
			value = []any{map[string]any{"address": testAddress, "confirmations": f.height, "txid": fmt.Sprintf("%064x", 1), "vout": 0}}
		}
	case "gettxout":
		value = map[string]any{"coinbase": true, "confirmations": f.height}
	case "createwallet":
		f.calls = append(f.calls, method)
		f.exists, f.loaded = true, true
		value = map[string]string{"name": "miner"}
	case "loadwallet":
		f.calls = append(f.calls, method)
		f.loaded = true
		value = map[string]string{"name": "miner"}
	case "importdescriptors":
		f.calls = append(f.calls, method)
		f.imported = true
		value = []any{map[string]bool{"success": true}}
	case "unloadwallet":
		f.calls = append(f.calls, method)
		if args[0] == "miner" {
			f.loaded = false
		} else {
			names := f.extraLoaded[:0]
			for _, name := range f.extraLoaded {
				if name != args[0] {
					names = append(names, name)
				}
			}
			f.extraLoaded = names
		}
		value = map[string]any{}
	default:
		return fmt.Errorf("unexpected RPC %s", method)
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return e
	}
	return json.Unmarshal(raw, out)
}
func (f *fakeRPC) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }

const testDescriptor = "pkh(0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798)"
const testAddress = "mrCDrCybB6J1vRfbwM5hemdJz73FwDBC8r"

// testFixture contains one fully identity-bound Bitcoin target and a frozen first gate.
type testFixture struct {
	c                client.Client
	worker           *Worker
	rpc              *fakeRPC
	root             *api.StacksNetwork
	node, production *api.StacksNetworkParticipant
	record           *bitcoin.BitcoinExecution
	initial          *bitcoin.BitcoinInitialization
	now              time.Time
}

func newFixture(t *testing.T) *testFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, bitcoin.AddToScheme, corev1.AddToScheme, appsv1.AddToScheme, rbacv1.AddToScheme} {
		if e := add(scheme); e != nil {
			t.Fatal(e)
		}
	}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "network-uid", Generation: 1}, Spec: api.StacksNetworkSpec{Operation: "Running", Participants: []api.Participant{{Name: "bitcoin", Kind: "BitcoinNode"}, {Name: "production", Kind: "BitcoinBlockProduction"}}}, Status: api.StacksNetworkStatus{Identities: []api.InstanceIdentity{{Name: "bitcoin", UID: "node-uid"}, {Name: "production", UID: "production-uid"}}, Bitcoin: &api.BitcoinRuntimeStatus{ExecutionRefs: []common.Binding{{Kind: "BitcoinExecution", Name: "execution", UID: "execution-uid"}}, InitializationRef: &common.Binding{Kind: "BitcoinInitialization", Name: "initialization", UID: "init-uid"}}}}
	owner := metav1.OwnerReference{APIVersion: api.GroupVersion.String(), Kind: "StacksNetwork", Name: root.Name, UID: root.UID, Controller: ptr.To(true)}
	walletBinding := common.Binding{Kind: "BitcoinWallet", Name: "miner", UID: "wallet-uid", Fingerprint: "wallet-digest"}
	node := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "node", Namespace: "test", UID: "node-uid", Generation: 1, OwnerReferences: []metav1.OwnerReference{owner}}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: root.UID, ParticipantName: "bitcoin", Kind: "BitcoinNode"}, Status: api.ParticipantStatus{Admission: &api.Admission{Configuration: api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{WalletRefs: ptr.To([]common.NameRef{{Name: "miner"}})}}, Dependencies: []common.Binding{walletBinding}}}}
	node.Status.Admission.PolicyDigest = foundation.Digest(node.Status.Admission.Configuration)
	node.Spec.Configuration = node.Status.Admission.Configuration
	node.Status.Runtime = &api.ParticipantRuntimeStatus{PolicyDigest: node.Status.Admission.PolicyDigest, WorkloadRefs: []common.Binding{{Kind: "StatefulSet", Name: "actor", UID: "actor-uid"}}, ConfigRef: &common.Binding{Kind: "Secret", Name: "config", UID: "config-uid"}, RPCSecretRef: &common.Binding{Kind: "Secret", Name: "rpc", UID: "rpc-uid"}, PodRef: &common.Binding{Kind: "Pod", Name: "actor-0", UID: "pod-uid"}, ContainerID: "containerd://bitcoin", Endpoints: []api.RuntimeEndpoint{{Name: "rpc", Host: "rpc.test.svc", Port: 18443}}}
	duration := common.Duration("1s")
	production := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "production", Namespace: "test", UID: "production-uid", Generation: 1, OwnerReferences: []metav1.OwnerReference{owner}}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: root.UID, ParticipantName: "production", Kind: "BitcoinBlockProduction"}, Status: api.ParticipantStatus{Admission: &api.Admission{Configuration: api.Configuration{BitcoinBlockProduction: &bitcoin.BitcoinBlockProductionSpec{Schedule: &bitcoin.BitcoinBlockScheduleSpec{Cadence: bitcoin.Cadence{Mode: "Fixed", Interval: &duration}}}}}}}
	production.Status.Admission.PolicyDigest = foundation.Digest(production.Status.Admission.Configuration)
	for _, participant := range []*api.StacksNetworkParticipant{node, production} {
		participant.Status.Conditions = []metav1.Condition{{Type: "AdmissionReady", Status: metav1.ConditionTrue, Reason: "RetainedPolicyEligible", Message: "Retained policy eligible", ObservedGeneration: participant.Generation, LastTransitionTime: metav1.NewTime(now)}}
	}
	record := &bitcoin.BitcoinExecution{ObjectMeta: metav1.ObjectMeta{Name: "execution", Namespace: "test", UID: "execution-uid", OwnerReferences: []metav1.OwnerReference{owner}}, Spec: bitcoin.BitcoinExecutionSpec{NetworkUID: root.UID, Participant: binding("StacksNetworkParticipant", node)}}
	wallet := bitcoin.FrozenBitcoinWallet{Wallet: walletBinding, Name: "miner", Address: testAddress, Descriptor: testDescriptor}
	initial := &bitcoin.BitcoinInitialization{ObjectMeta: metav1.ObjectMeta{Name: "initialization", Namespace: "test", UID: "init-uid", OwnerReferences: []metav1.OwnerReference{owner}}, Spec: bitcoin.BitcoinInitializationSpec{NetworkUID: root.UID, Production: binding("StacksNetworkParticipant", production), Target: binding("StacksNetworkParticipant", node), Nodes: []common.Binding{binding("StacksNetworkParticipant", node)}, MinimumHeight: 203, MatureOutputsPerMiner: 1, MinerWallets: []bitcoin.FrozenBitcoinWallet{wallet}, PayoutWallet: wallet}}
	powner := metav1.OwnerReference{APIVersion: api.GroupVersion.String(), Kind: "StacksNetworkParticipant", Name: node.Name, UID: node.UID, Controller: ptr.To(true)}
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "actor", Namespace: "test", UID: "actor-uid", OwnerReferences: []metav1.OwnerReference{powner}}, Spec: appsv1.StatefulSetSpec{Replicas: ptr.To[int32](1)}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "actor-0", Namespace: "test", UID: "pod-uid", Labels: labels(node, "actor"), OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "StatefulSet", Name: sts.Name, UID: sts.UID, Controller: ptr.To(true)}}}, Status: corev1.PodStatus{PodIP: "127.0.0.1", ContainerStatuses: []corev1.ContainerStatus{{Name: "bitcoin", ContainerID: node.Status.Runtime.ContainerID, Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "rpc", Namespace: "test", UID: "service-uid", OwnerReferences: []metav1.OwnerReference{powner}}, Spec: corev1.ServiceSpec{Selector: labels(node, "actor"), Ports: []corev1.ServicePort{{Name: "rpc", Port: 18443}}}}
	genesis := &api.StacksGenesis{ObjectMeta: metav1.ObjectMeta{Name: "genesis", Namespace: "test", UID: "genesis-uid", OwnerReferences: []metav1.OwnerReference{owner}}, Spec: api.StacksGenesisSpec{Source: api.GenesisSource{NetworkUID: root.UID}, Bootstrap: api.Bootstrap{Gates: []api.Gate{{Name: "PrepareBitcoin", BitcoinCeiling: 203}, {Name: "EnrollPoX4", BitcoinCeiling: 234, TargetCycle: ptr.To[int64](12)}}}, Chain: api.Chain{Epochs: []api.Epoch{{Name: "2.5", StartHeight: 209}, {Name: "3.0", StartHeight: 252}}}}}
	genesisRef := binding("StacksGenesis", genesis)
	genesisRef.Fingerprint = foundation.Digest(genesis.Spec)
	initial.Spec.Genesis = genesisRef
	root.Status.GenesisRef = &genesisRef
	root.Status.GenesisDigest = foundation.Digest(genesis.Spec.Chain)
	objects := []client.Object{root, node, production, record, initial, genesis, sts, pod, service, &bitcoin.BitcoinWallet{ObjectMeta: metav1.ObjectMeta{Name: "miner", Namespace: "test", UID: "wallet-uid"}, Spec: bitcoin.BitcoinWalletSpec{WatchOnly: ptr.To(true)}, Status: common.ResolutionStatus{Digest: "wallet-digest", Descriptor: testDescriptor, BitcoinAddress: testAddress}}}
	for _, name := range []string{"rpc", "config"} {
		objects = append(objects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", UID: types.UID(name + "-uid"), OwnerReferences: []metav1.OwnerReference{powner}}, Immutable: ptr.To(true)})
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjectTracker(clientgotesting.NewObjectTracker(scheme, serializer.NewCodecFactory(scheme).UniversalDecoder())).WithStatusSubresource(root, node, production, record, initial, pod).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if obj.GetUID() == "" {
				obj.SetUID(types.UID(obj.GetName() + "-uid"))
			}
			return c.Create(ctx, obj, opts...)
		},
		SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			p, ok := obj.(*api.StacksNetworkParticipant)
			if !ok || sub != "status" || patch.Type() != types.ApplyPatchType || p.Status.BitcoinControl == nil && p.Status.Scheduling == nil {
				return c.SubResource(sub).Patch(ctx, obj, patch, opts...)
			}
			current := &api.StacksNetworkParticipant{}
			if e := c.Get(ctx, client.ObjectKeyFromObject(p), current); e != nil {
				return e
			}
			if p.Status.BitcoinControl != nil {
				current.Status.BitcoinControl = p.Status.BitcoinControl
			}
			if p.Status.Scheduling != nil {
				current.Status.Scheduling = p.Status.Scheduling
			}
			if e := c.Status().Update(ctx, current); e != nil {
				return e
			}
			*p = *current
			return nil
		},
	}).Build()
	rpc := &fakeRPC{exists: true, loaded: true, imported: true}
	worker := &Worker{Client: c, Reader: c, RPC: rpc, Input: WorkerInput{Namespace: "test", RecordName: record.Name, RecordUID: record.UID, CredentialsName: "rpc", CredentialsUID: "rpc-uid"}, Now: func() time.Time { return now }, ProcessNonce: "process-one", DrainTimeout: time.Second}
	if e := worker.defaults(); e != nil {
		t.Fatal(e)
	}
	f := &testFixture{c: c, worker: worker, rpc: rpc, root: root, node: node, production: production, record: record, initial: initial, now: now}
	t.Cleanup(worker.drain)
	return f
}
func (f *testFixture) readRecord(t *testing.T) *bitcoin.BitcoinExecution {
	t.Helper()
	record := &bitcoin.BitcoinExecution{}
	if e := f.c.Get(context.Background(), client.ObjectKeyFromObject(f.record), record); e != nil {
		t.Fatal(e)
	}
	return record
}
func (f *testFixture) offer(t *testing.T, height int64) {
	t.Helper()
	offer := &bitcoin.BitcoinBlockOffer{Initialization: binding("BitcoinInitialization", f.initial), Production: binding("StacksNetworkParticipant", f.production), PolicyDigest: f.production.Status.Admission.PolicyDigest, Number: 1, Address: testAddress, Wallet: f.initial.Spec.PayoutWallet.Wallet, ExpectedHeight: height, ExpectedTip: fmt.Sprintf("%064x", height), Ceiling: 203, ExpiresAt: metav1.NewTime(f.now.Add(time.Minute))}
	initial := &bitcoin.BitcoinInitialization{}
	if e := f.c.Get(context.Background(), client.ObjectKeyFromObject(f.initial), initial); e != nil {
		t.Fatal(e)
	}
	initial.Status.Offer = offer
	if e := f.c.Status().Update(context.Background(), initial); e != nil {
		t.Fatal(e)
	}
	record := f.readRecord(t)
	record.Spec.Offer = offer
	if e := f.c.Update(context.Background(), record); e != nil {
		t.Fatal(e)
	}
	f.rpc.height = height
}
func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not converge")
}

func TestWalletMutationsUseSameAuthority(t *testing.T) {
	f := newFixture(t)
	f.rpc.exists, f.rpc.loaded, f.rpc.imported = false, false, false
	for _, method := range []string{"CreateWallet", "ImportDescriptor"} {
		if e := f.worker.Step(context.Background()); e != nil {
			t.Fatal(e)
		}
		eventually(t, func() bool {
			r := f.readRecord(t)
			return r.Status.LastReceipt != nil && string(r.Status.LastReceipt.Request.Method) == method && r.Status.Armed == nil
		})
	}
	if e := f.worker.Step(context.Background()); e != nil {
		t.Fatal(e)
	}
	record := f.readRecord(t)
	if !record.Status.Observation.Wallets[0].Ready || f.rpc.count() != 2 {
		t.Fatal("wallet convergence repeated mutation")
	}
	if record.Status.Reservation == nil || record.Status.Reservation.UID != f.initial.UID {
		t.Fatal("receipt released shared reservation")
	}
}
func TestGenerateOnceAndHoldAfterAmbiguousReceipt(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			f := newFixture(t)
			f.offer(t, 202)
			f.rpc.fail = fail
			if e := f.worker.Step(context.Background()); e != nil {
				t.Fatal(e)
			}
			eventually(t, func() bool { return f.rpc.count() == 1 })
			if !fail {
				eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == 1 })
			}
			replacement := &Worker{Client: f.c, Reader: f.c, RPC: f.rpc, Input: f.worker.Input, Now: f.worker.Now, ProcessNonce: "replacement"}
			if e := replacement.defaults(); e != nil {
				t.Fatal(e)
			}
			for range 3 {
				_ = replacement.Step(context.Background())
			}
			if f.rpc.count() != 1 {
				t.Fatal("existing authority was replayed")
			}
			record := f.readRecord(t)
			if fail && record.Status.Armed == nil {
				t.Fatal("lost response cleared exclusion")
			}
			if !fail && (record.Status.LastReceipt == nil || record.Status.BlocksGenerated != 1) {
				t.Fatal("receipt not accounted exactly once")
			}
		})
	}
}

// failingStatusClient injects lost CAS acknowledgements and transient accounting outages.
type failingStatusClient struct {
	client.Client
	fail        atomic.Bool
	afterCommit bool
}

func (c *failingStatusClient) Status() client.SubResourceWriter {
	return failingStatusWriter{SubResourceWriter: c.Client.Status(), owner: c}
}

type failingStatusWriter struct {
	client.SubResourceWriter
	owner *failingStatusClient
}

func (w failingStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	record, ok := obj.(*bitcoin.BitcoinExecution)
	if ok && w.owner.fail.Load() && (record.Status.Armed != nil || record.Status.LastReceipt != nil) {
		if w.owner.afterCommit {
			if e := w.SubResourceWriter.Update(ctx, obj, opts...); e != nil {
				return e
			}
		}
		return fmt.Errorf("API write acknowledgement unavailable")
	}
	return w.SubResourceWriter.Update(ctx, obj, opts...)
}
func TestLostArmAcknowledgementNeverSends(t *testing.T) {
	f := newFixture(t)
	f.offer(t, 1)
	writer := &failingStatusClient{Client: f.c, afterCommit: true}
	writer.fail.Store(true)
	f.worker.Client = writer
	if e := f.worker.Step(context.Background()); e == nil {
		t.Fatal("expected lost CAS acknowledgement")
	}
	if f.rpc.count() != 0 || f.readRecord(t).Status.Armed == nil {
		t.Fatal("lost arm acknowledgement authorized a send")
	}
	writer.fail.Store(false)
	if e := f.worker.Step(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.rpc.count() != 0 {
		t.Fatal("re-read Armed authorized resend")
	}
}
func TestReceiptSurvivesAPIOutage(t *testing.T) {
	f := newFixture(t)
	f.offer(t, 1)
	writer := &failingStatusClient{Client: f.c}
	f.worker.Client = writer
	f.rpc.after = func() { writer.fail.Store(true) }
	if e := f.worker.Step(context.Background()); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return writer.fail.Load() })
	if f.readRecord(t).Status.Armed == nil {
		t.Fatal("lost accounting cleared authority")
	}
	writer.fail.Store(false)
	eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == 1 })
	receipt := f.readRecord(t).Status.LastReceipt
	if !receipt.ReceivedAt.Equal(&metav1.Time{Time: f.now}) || f.rpc.count() != 1 {
		t.Fatal("receipt retry changed evidence or replayed RPC")
	}
}
func TestCurrentIdentityAndCeilingFailClosed(t *testing.T) {
	for _, change := range []string{"ceiling", "credentials", "process", "failed"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t)
			f.offer(t, 203)
			switch change {
			case "credentials":
				f.worker.Input.CredentialsUID = "replacement"
			case "process":
				pod := &corev1.Pod{}
				_ = f.c.Get(context.Background(), client.ObjectKey{Namespace: "test", Name: "actor-0"}, pod)
				pod.Status.ContainerStatuses[0].ContainerID = "replacement"
				_ = f.c.Status().Update(context.Background(), pod)
			case "failed":
				root := &api.StacksNetwork{}
				_ = f.c.Get(context.Background(), client.ObjectKeyFromObject(f.root), root)
				root.Status.Phase = "Failed"
				_ = f.c.Status().Update(context.Background(), root)
			}
			_ = f.worker.Step(context.Background())
			if f.rpc.count() != 0 {
				t.Fatal("invalid authority mutated Core")
			}
		})
	}
}
func TestPauseAcknowledgementWaitsForOutstandingSend(t *testing.T) {
	f := newFixture(t)
	f.offer(t, 1)
	done := make(chan struct{})
	f.rpc.wait = done
	if e := f.worker.Step(context.Background()); e != nil {
		t.Fatal(e)
	}
	root := &api.StacksNetwork{}
	_ = f.c.Get(context.Background(), client.ObjectKeyFromObject(f.root), root)
	root.Spec.Operation = "Paused"
	root.Generation++
	if e := f.c.Update(context.Background(), root); e != nil {
		t.Fatal(e)
	}
	if e := f.worker.Step(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.readRecord(t).Status.Control != nil {
		t.Fatal("pause acknowledged before send settled")
	}
	close(done)
	eventually(t, func() bool { return f.readRecord(t).Status.Armed == nil })
	if e := f.worker.Step(context.Background()); e != nil {
		t.Fatal(e)
	}
	ack := f.readRecord(t).Status.Control
	if ack == nil || ack.NetworkGeneration != root.Generation || ack.ProcessNonce != "process-one" {
		t.Fatal("missing bound pause acknowledgement")
	}
}
func TestWorkerResourcesAreScoped(t *testing.T) {
	f := newFixture(t)
	objects, e := WorkerResources(f.node, f.record, f.initial, "worker:test", 1)
	if e != nil {
		t.Fatal(e)
	}
	for _, obj := range objects {
		switch obj := obj.(type) {
		case *rbacv1.Role:
			for _, rule := range obj.Rules {
				if len(rule.ResourceNames) == 0 {
					t.Fatal("unbounded worker API rule")
				}
				for _, verb := range rule.Verbs {
					if verb != "get" && rule.Resources[0] != "bitcoinexecutions/status" {
						t.Fatal("worker can mutate unrelated resources")
					}
				}
			}
		case *appsv1.Deployment:
			if len(obj.Spec.Template.Spec.Volumes) != 1 || obj.Spec.Template.Spec.Volumes[0].Secret.SecretName != "rpc" || obj.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType || *obj.Spec.Template.Spec.TerminationGracePeriodSeconds != 45 {
				t.Fatal("worker transport/termination scope differs")
			}
		}
	}
}

func TestPublicWalletRejectsPrivateProfile(t *testing.T) {
	_, _, e := identity.FromDescriptor(testDescriptor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = publicWallet(&bitcoin.BitcoinWallet{Spec: bitcoin.BitcoinWalletSpec{WatchOnly: ptr.To(false)}, Status: common.ResolutionStatus{Digest: "digest", Descriptor: testDescriptor, BitcoinAddress: testAddress}}); e == nil {
		t.Fatal("silently accepted private wallet profile")
	}
}

// schedulerFor uses a deterministic clock and complete production policy.
func (f *testFixture) schedulerFor() *Scheduler {
	return &Scheduler{Client: f.c, Reader: f.c, Now: func() time.Time { return f.now }, Draw: func(int64) int64 { return 0 }, Freshness: 10 * time.Second}
}
func (f *testFixture) reconcile(t *testing.T, s *Scheduler) {
	t.Helper()
	if _, e := s.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.initial)}); e != nil {
		t.Fatal(e)
	}
}
