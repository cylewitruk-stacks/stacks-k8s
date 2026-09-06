package production

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/workload"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type fakeRPC struct {
	sends    int
	check    func(context.Context) error
	generate func(context.Context) error
}

func (f *fakeRPC) Check(ctx context.Context, _, _ string) error {
	if f.check != nil {
		return f.check(ctx)
	}
	return nil
}

func (f *fakeRPC) Generate(ctx context.Context, _, _, _ string) (string, error) {
	f.sends++
	if f.generate != nil {
		if err := f.generate(ctx); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%064x", f.sends), nil
}

type productionFixture struct {
	r      *Reconciler
	rpc    *fakeRPC
	now    time.Time
	parent *networkv1alpha1.StacksNetwork
	policy *bitcoinv1alpha1.BitcoinBlockProduction
	actor  *networkv1alpha1.BitcoinNode
	pod    *corev1.Pod
	sts    *appsv1.StatefulSet
	config *corev1.ConfigMap
}

func fixture(t *testing.T) *productionFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, networkv1alpha1.AddToScheme, bitcoinv1alpha1.AddToScheme, actionv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("approved configuration")))
	f := &productionFixture{now: time.Unix(1000, 0), rpc: &fakeRPC{}}
	f.parent = &networkv1alpha1.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "network-uid", Generation: 1}}
	policy := bitcoinv1alpha1.ProductionPolicy{Target: "bitcoin", IntervalSeconds: 5, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn"}
	f.parent.Spec.BitcoinBlockProduction = policy.DeepCopy()
	f.parent.Status.BitcoinProductionUID = "production-uid"
	f.policy = &bitcoinv1alpha1.BitcoinBlockProduction{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "production-uid", Generation: 1, Finalizers: []string{ledgerFinalizer}}, Spec: bitcoinv1alpha1.BitcoinBlockProductionSpec{NetworkName: "network", NetworkUID: "network-uid", Policy: policy}}
	f.actor = &networkv1alpha1.BitcoinNode{ObjectMeta: metav1.ObjectMeta{Name: "network-bitcoin", Namespace: "test", UID: "bitcoin-uid", Generation: 1}, Spec: networkv1alpha1.BitcoinNodeSpec{NetworkRef: networkv1alpha1.LocalObjectReference{Name: "network"}, ActorName: "bitcoin", Role: "miner", Image: "bitcoin:test", Config: networkv1alpha1.ConfigSource{ConfigMapRef: &networkv1alpha1.ConfigObjectRef{Name: "config", Key: "bitcoin.conf", ExpectedDigest: digest}}}}
	specDigest, err := workload.SpecDigest(f.actor.Spec)
	if err != nil {
		t.Fatal(err)
	}
	f.parent.Status.TargetDeclarations = &networkv1alpha1.TargetDeclarations{SchemaVersion: networkv1alpha1.TargetDeclarationsVersion, NetworkUID: "network-uid", ObservedGeneration: 1, Actors: []networkv1alpha1.TargetDeclaration{{Kind: "BitcoinNode", Name: f.actor.Name, ActorName: "bitcoin", SpecDigest: specDigest}}}
	one, immutable := int32(1), true
	f.sts = &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: f.actor.Name, Namespace: "test", UID: "sts-uid", Generation: 1}, Spec: appsv1.StatefulSetSpec{Replicas: &one, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"network.stacks.org/config-digest": digest}}}}, Status: appsv1.StatefulSetStatus{ObservedGeneration: 1, CurrentRevision: "revision", UpdateRevision: "revision", ReadyReplicas: 1}}
	imageID := "sha256:" + strings.Repeat("a", 64)
	f.pod = &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: f.actor.Name + "-0", Namespace: "test", UID: "pod-uid", Labels: map[string]string{appsv1.StatefulSetRevisionLabel: "revision"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "actor", Image: "bitcoin:test"}}}, Status: corev1.PodStatus{PodIP: "10.0.0.1", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}, ContainerStatuses: []corev1.ContainerStatus{{Name: "actor", Ready: true, ContainerID: "containerd://bitcoin", ImageID: imageID, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	f.config = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "test"}, Immutable: &immutable, Data: map[string]string{"bitcoin.conf": "approved configuration"}}
	f.actor.Status = networkv1alpha1.ActorStatus{Ready: true, ObservedGeneration: 1, Identity: &networkv1alpha1.ActorIdentity{ResourceName: f.actor.Name, SpecDigest: specDigest, ConfigDigest: digest, StatefulSetName: f.sts.Name, StatefulSetUID: string(f.sts.UID), ControllerRevision: "revision", PodName: f.pod.Name, PodUID: string(f.pod.UID), RuntimeImageID: imageID}}
	for _, pair := range [][2]client.Object{{f.parent, f.policy}, {f.parent, f.actor}, {f.actor, f.sts}, {f.sts, f.pod}} {
		if err := controllerutil.SetControllerReference(pair[0], pair[1], scheme); err != nil {
			t.Fatal(err)
		}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(f.parent, f.policy, f.actor, f.sts, f.pod, &actionv1.BitcoinBlockGeneration{}, &actionv1.BitcoinReorganization{}).WithObjects(f.parent, f.policy, f.actor, f.sts, f.pod, f.config).Build()
	f.r = &Reconciler{Client: c, APIReader: c, RPC: f.rpc, ConfigDigest: digest, RPCTimeout: time.Second, Now: func() time.Time { return f.now }, ProcessNonce: "process-one"}
	f.r.collectors = newCollectorPool(f.r, collectorLimit, collectorDrain)
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		f.r.collectors.drainTimeout = time.Second
		_ = f.r.collectors.Start(ctx)
	})
	return f
}

func (f *productionFixture) reconcile(t *testing.T, ctx context.Context) ctrl.Result {
	t.Helper()
	result, err := f.r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
	if err != nil {
		t.Fatal(err)
	}
	f.waitCollectors(t)
	if ctx.Err() == nil && f.ledger(t).Status.DispatchState == "Armed" {
		if _, err := f.r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)}); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

// waitCollectors joins quick fixture dispatches without changing production scheduling.
func (f *productionFixture) waitCollectors(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() { f.r.collectors.workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture collector did not finish")
	}
}

func (f *productionFixture) ledger(t *testing.T) *bitcoinv1alpha1.BitcoinBlockProduction {
	t.Helper()
	result := &bitcoinv1alpha1.BitcoinBlockProduction{}
	if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(f.policy), result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCadenceDoesNotRequireAggregateInventory(t *testing.T) {
	f := fixture(t)
	f.rpc.generate = func(context.Context) error {
		if f.ledger(t).Status.DispatchState != "Armed" {
			t.Fatal("send began without durable authorization")
		}
		return nil
	}
	f.reconcile(t, context.Background())
	if got := f.ledger(t).Status; got.BlocksProduced != 1 || got.DispatchState != "Idle" {
		t.Fatalf("receipt not accounted: %#v", got)
	}
	if f.parent.Status.InventoryReady {
		t.Fatal("fixture unexpectedly has global readiness")
	}
	f.reconcile(t, context.Background())
	if f.rpc.sends != 1 {
		t.Fatal("early reconciliation created an extra block")
	}
	f.now = f.now.Add(100 * time.Second)
	f.reconcile(t, context.Background())
	if f.rpc.sends != 2 {
		t.Fatal("missed ticks must collapse to one dispatch")
	}
	parent := f.parent.DeepCopy()
	if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(parent), parent); err != nil {
		t.Fatal(err)
	}
	parent.Spec.BitcoinBlockProduction.Paused = true
	if err := f.r.Update(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	f.reconcile(t, context.Background())
	if f.rpc.sends != 2 || f.ledger(t).Status.Phase != "Paused" {
		t.Fatal("pause did not stop new dispatches")
	}
}

func TestAmbiguousDispatchRemainsClosedAcrossRestart(t *testing.T) {
	f := fixture(t)
	f.rpc.generate = func(context.Context) error { return fmt.Errorf("response lost after server accepted request") }
	f.reconcile(t, context.Background())
	f.r.ProcessNonce = "new-process"
	f.now = f.now.Add(time.Hour)
	f.reconcile(t, context.Background())
	got := f.ledger(t).Status
	if f.rpc.sends != 1 || got.Phase != "Blocked" || got.DispatchState != "Armed" || got.BlocksProduced != 0 {
		t.Fatalf("ambiguous dispatch reopened: sends=%d status=%#v", f.rpc.sends, got)
	}
}

func TestGracefulCancellationPreservesAuthorizedReceipt(t *testing.T) {
	f := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.rpc.generate = func(collector context.Context) error { cancel(); return collector.Err() }
	f.reconcile(t, ctx)
	if f.ledger(t).Status.DispatchState != "Idle" {
		t.Fatal("shutdown discarded a surviving receipt")
	}
}

func TestAdmissionRejectsStaleOrUnapprovedTargets(t *testing.T) {
	cases := map[string]func(*productionFixture){
		"catalog generation":  func(f *productionFixture) { f.parent.Status.TargetDeclarations.ObservedGeneration++ },
		"catalog UID":         func(f *productionFixture) { f.parent.Status.TargetDeclarations.NetworkUID = "old" },
		"ledger UID":          func(f *productionFixture) { f.parent.Status.BitcoinProductionUID = "old" },
		"leaf generation":     func(f *productionFixture) { f.actor.Generation++ },
		"unready leaf":        func(f *productionFixture) { f.actor.Status.Ready = false },
		"replaced Pod":        func(f *productionFixture) { f.pod.UID = types.UID("replacement") },
		"rolling StatefulSet": func(f *productionFixture) { f.sts.Status.UpdateRevision = "new" },
		"mutable config":      func(f *productionFixture) { v := false; f.config.Immutable = &v },
		"config bytes":        func(f *productionFixture) { f.config.Data["bitcoin.conf"] = "unapproved" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := fixture(t)
			mutate(f)
			c := fake.NewClientBuilder().WithScheme(f.r.Scheme()).WithStatusSubresource(f.parent, f.policy, f.actor, f.sts, f.pod, &actionv1.BitcoinBlockGeneration{}, &actionv1.BitcoinReorganization{}).WithObjects(f.parent, f.policy, f.actor, f.sts, f.pod, f.config).Build()
			f.r.Client, f.r.APIReader = c, c
			f.reconcile(t, context.Background())
			if f.rpc.sends != 0 {
				t.Fatal("unadmitted target received mutation")
			}
		})
	}
}

func TestAccountingIsIdempotentAndRejectsOldDispatch(t *testing.T) {
	f := fixture(t)
	var armed *bitcoinv1alpha1.BitcoinBlockProduction
	f.rpc.generate = func(context.Context) error { armed = f.ledger(t); return nil }
	f.reconcile(t, context.Background())
	first := armed.DeepCopy()
	if err := f.r.account(context.Background(), first, receivedReceipt{hash: fmt.Sprintf("%064x", 1), completed: metav1.NewTime(f.now)}); err != nil {
		t.Fatal(err)
	}
	if f.ledger(t).Status.BlocksProduced != 1 {
		t.Fatal("duplicate receipt counted twice")
	}
	f.now = f.now.Add(time.Minute)
	f.reconcile(t, context.Background())
	if err := f.r.account(context.Background(), first, receivedReceipt{hash: fmt.Sprintf("%064x", 1), completed: metav1.NewTime(f.now)}); err == nil {
		t.Fatal("old dispatch receipt accepted after new authorization")
	}
}

// uncertainStatusClient models a status write whose acknowledgement is lost.
type uncertainStatusClient struct {
	client.Client
	state  string
	failed bool
}

func (c *uncertainStatusClient) Status() client.SubResourceWriter {
	return &uncertainStatusWriter{SubResourceWriter: c.Client.Status(), owner: c}
}

type uncertainStatusWriter struct {
	client.SubResourceWriter
	owner *uncertainStatusClient
}

func (w *uncertainStatusWriter) Patch(ctx context.Context, object client.Object, patch client.Patch, options ...client.SubResourcePatchOption) error {
	err := w.SubResourceWriter.Patch(ctx, object, patch, options...)
	if policy, ok := object.(*bitcoinv1alpha1.BitcoinBlockProduction); ok && err == nil && !w.owner.failed && policy.Status.DispatchState == w.owner.state {
		w.owner.failed = true
		return fmt.Errorf("API acknowledgement lost")
	}
	return err
}

func TestLostArmAcknowledgementNeverSends(t *testing.T) {
	f := fixture(t)
	f.r.Client = &uncertainStatusClient{Client: f.r.Client, state: "Armed"}
	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
	if err == nil {
		t.Fatal("expected uncertain arm write")
	}
	f.reconcile(t, context.Background())
	if f.rpc.sends != 0 || f.ledger(t).Status.Phase != "Blocked" {
		t.Fatal("uncertain authorization was dispatched")
	}
}

func TestLostReceiptAcknowledgementDoesNotRepeatRPCOrAccounting(t *testing.T) {
	f := fixture(t)
	f.r.Client = &uncertainStatusClient{Client: f.r.Client, state: "Idle"}
	f.reconcile(t, context.Background())
	if f.rpc.sends != 1 || f.ledger(t).Status.BlocksProduced != 1 {
		t.Fatal("lost status acknowledgement repeated an effect")
	}
}

func TestEnvironmentDeletionAbandonsUnresolvedLedger(t *testing.T) {
	f := fixture(t)
	f.rpc.generate = func(context.Context) error { return fmt.Errorf("no receipt") }
	f.reconcile(t, context.Background())
	if err := f.r.Delete(context.Background(), f.parent); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, context.Background())
	got := f.ledger(t)
	if got.Status.Phase != "Abandoned" || controllerutil.ContainsFinalizer(got, ledgerFinalizer) || got.Status.DispatchState != "Armed" {
		t.Fatalf("administrative abandonment lost outcome or retained finalizer: %#v", got)
	}
}

// TestConcurrentProducersCompeteForOneDurableAuthorization exercises the actual CAS path.
func TestConcurrentProducersCompeteForOneDurableAuthorization(t *testing.T) {
	f := fixture(t)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	f.rpc.check = func(context.Context) error { arrived <- struct{}{}; <-release; return nil }
	second := *f.r
	second.ProcessNonce = "process-two"
	second.collectors = newCollectorPool(&second, collectorLimit, time.Second)
	errors := make(chan error, 2)
	for _, r := range []*Reconciler{f.r, &second} {
		go func() {
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
			errors <- err
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(time.Second):
			t.Fatal("producers did not reach preflight")
		}
	}
	close(release)
	failed := 0
	for i := 0; i < 2; i++ {
		if <-errors != nil {
			failed++
		}
	}
	f.waitCollectors(t)
	second.collectors.workers.Wait()
	if failed != 1 || f.rpc.sends != 1 || f.ledger(t).Status.BlocksProduced != 1 {
		t.Fatalf("competing producers shared authorization: failures=%d sends=%d", failed, f.rpc.sends)
	}
}
