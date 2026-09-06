package transactions

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	stacksv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/workload"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// fakeRPC supplies controlled submission and inclusion outcomes.
type fakeRPC struct {
	sends       int
	nonce       int64
	submitError error
	inclusion   Inclusion
}

func (f *fakeRPC) Account(context.Context, string, string) (AccountState, error) {
	return AccountState{Nonce: f.nonce, Balance: 1000000000000}, nil
}
func (f *fakeRPC) Submit(context.Context, string, SignedTransfer) error {
	f.sends++
	return f.submitError
}
func (f *fakeRPC) Inclusion(context.Context, string, string) (Inclusion, error) {
	return f.inclusion, nil
}

// fakeSigner makes a bounded hash-consistent fixture without invoking an external process.
type fakeSigner struct{}

func (fakeSigner) Sign(_ context.Context, _ stacksv1alpha1.TransferPolicy, nonce int64) (SignedTransfer, error) {
	raw := make([]byte, 180)
	raw[0] = byte(nonce)
	sum := sha512.Sum512_256(raw)
	return SignedTransfer{TxID: hex.EncodeToString(sum[:]), Bytes: hex.EncodeToString(raw)}, nil
}

type productionFixture struct {
	r      *Reconciler
	rpc    *fakeRPC
	now    time.Time
	parent *networkv1alpha1.StacksNetwork
	policy *stacksv1alpha1.StacksTransactionProduction
	actor  *networkv1alpha1.StacksNode
	pod    *corev1.Pod
	sts    *appsv1.StatefulSet
	config *corev1.ConfigMap
}

func fixture(t *testing.T) *productionFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, networkv1alpha1.AddToScheme, stacksv1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("approved configuration")))
	f := &productionFixture{now: time.Unix(1000, 0), rpc: &fakeRPC{}}
	f.parent = &networkv1alpha1.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "network-uid", Generation: 1}}
	policy := stacksv1alpha1.TransferPolicy{Target: "ingress", IntervalSeconds: 5, Sender: "ST-sender", Recipient: "ST-recipient", AmountMicroSTX: 1, FeeMicroSTX: 1000}
	f.parent.Spec.StacksTransactionProduction = policy.DeepCopy()
	f.parent.Status.TransactionProductionUID = "production-uid"
	f.policy = &stacksv1alpha1.StacksTransactionProduction{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "production-uid", Generation: 1, Finalizers: []string{ledgerFinalizer}}, Spec: stacksv1alpha1.StacksTransactionProductionSpec{NetworkName: "network", NetworkUID: "network-uid", Policy: policy}}
	f.actor = &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "network-ingress", Namespace: "test", UID: "ingress-uid", Generation: 1}, Spec: networkv1alpha1.StacksNodeSpec{NetworkRef: networkv1alpha1.LocalObjectReference{Name: "network"}, ActorName: "ingress", Role: "miner", Image: "stacks:test", Config: networkv1alpha1.ConfigSource{SecretRef: &networkv1alpha1.ConfigObjectRef{Name: "config", Key: "config.toml", ExpectedDigest: digest}}}}
	specDigest, err := workload.SpecDigest(f.actor.Spec)
	if err != nil {
		t.Fatal(err)
	}
	f.parent.Status.TargetDeclarations = &networkv1alpha1.TargetDeclarations{SchemaVersion: networkv1alpha1.TargetDeclarationsVersion, NetworkUID: "network-uid", ObservedGeneration: 1, Actors: []networkv1alpha1.TargetDeclaration{{Kind: "StacksNode", Name: f.actor.Name, ActorName: "ingress", SpecDigest: specDigest}}}
	one, immutable := int32(1), true
	f.sts = &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: f.actor.Name, Namespace: "test", UID: "sts-uid", Generation: 1}, Spec: appsv1.StatefulSetSpec{Replicas: &one, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"network.stacks.org/config-digest": digest}}}}, Status: appsv1.StatefulSetStatus{ObservedGeneration: 1, CurrentRevision: "revision", UpdateRevision: "revision", ReadyReplicas: 1}}
	imageID := "sha256:" + strings.Repeat("a", 64)
	f.pod = &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: f.actor.Name + "-0", Namespace: "test", UID: "pod-uid", Labels: map[string]string{appsv1.StatefulSetRevisionLabel: "revision"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "actor", Image: "stacks:test"}}}, Status: corev1.PodStatus{PodIP: "10.0.0.1", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}, ContainerStatuses: []corev1.ContainerStatus{{Name: "actor", Ready: true, ContainerID: "containerd://stacks", ImageID: imageID, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	f.config = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "test"}, Immutable: &immutable, Data: map[string]string{"config.toml": "approved configuration"}}
	f.actor.Status = networkv1alpha1.ActorStatus{Ready: true, ObservedGeneration: 1, Identity: &networkv1alpha1.ActorIdentity{ResourceName: f.actor.Name, SpecDigest: specDigest, ConfigDigest: digest, StatefulSetName: f.sts.Name, StatefulSetUID: string(f.sts.UID), ControllerRevision: "revision", PodName: f.pod.Name, PodUID: string(f.pod.UID), RuntimeImageID: imageID}}
	for _, pair := range [][2]client.Object{{f.parent, f.policy}, {f.parent, f.actor}, {f.actor, f.sts}, {f.sts, f.pod}} {
		if err := controllerutil.SetControllerReference(pair[0], pair[1], scheme); err != nil {
			t.Fatal(err)
		}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(f.parent, f.policy, f.actor, f.sts, f.pod).WithObjects(f.parent, f.policy, f.actor, f.sts, f.pod, f.config).Build()
	f.r = &Reconciler{Client: c, APIReader: c, RPC: f.rpc, Profile: AccountProfile{NetworkName: "network", Sender: "ST-sender", ConfigDigest: digest}, Signer: fakeSigner{}, Now: func() time.Time { return f.now }}
	return f
}

// reconcile runs one level-based step against the fake API server.
func (f *productionFixture) reconcile(t *testing.T) {
	t.Helper()
	if _, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)}); err != nil {
		t.Fatal(err)
	}
}

// ledger reads the current durable transaction state.
func (f *productionFixture) ledger(t *testing.T) *stacksv1alpha1.StacksTransactionProduction {
	t.Helper()
	p := &stacksv1alpha1.StacksTransactionProduction{}
	if err := f.r.APIReader.Get(context.Background(), client.ObjectKeyFromObject(f.policy), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPendingTransferBackpressuresAndAccountsExactInclusionOnce(t *testing.T) {
	f := fixture(t)
	f.reconcile(t)
	first := f.ledger(t)
	if !first.Status.Outstanding || !first.Status.Accepted || f.rpc.sends != 1 {
		t.Fatal("submission was not reserved and acknowledged")
	}
	f.now = f.now.Add(time.Hour)
	f.reconcile(t)
	f.reconcile(t)
	if f.rpc.sends != 1 {
		t.Fatal("pending work caused catch-up submissions")
	}
	f.rpc.inclusion = Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	f.reconcile(t)
	got := f.ledger(t)
	if got.Status.Outstanding || got.Status.Confirmed != 1 || *got.Status.NextNonce != 1 {
		t.Fatalf("inclusion not accounted: %#v", got.Status)
	}
	f.rpc.nonce = 1
	f.reconcile(t)
	if f.rpc.sends != 2 || f.ledger(t).Status.Confirmed != 1 {
		t.Fatal("accounting duplicated or failed to resume latest cadence")
	}
}

func TestLostSubmissionResponseSurvivesRestartWithoutAnotherPOST(t *testing.T) {
	f := fixture(t)
	f.rpc.submitError = fmt.Errorf("connection lost")
	f.reconcile(t)
	original := f.ledger(t).Status.TxID
	replacement := *f.r
	f.r = &replacement
	f.reconcile(t)
	if f.ledger(t).Status.Phase != "Ambiguous" || f.rpc.sends != 1 {
		t.Fatal("ambiguous submission was retried")
	}
	f.rpc.inclusion = Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	f.reconcile(t)
	if f.ledger(t).Status.Confirmed != 1 || f.ledger(t).Status.TxID != original || f.rpc.sends != 1 {
		t.Fatal("restart failed to account the exact transaction")
	}
}

func TestPauseAndRemovalStillObserveOutstandingTransaction(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprint(remove), func(t *testing.T) {
			f := fixture(t)
			f.reconcile(t)
			if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(f.parent), f.parent); err != nil {
				t.Fatal(err)
			}
			if remove {
				f.parent.Spec.StacksTransactionProduction = nil
			} else {
				f.parent.Spec.StacksTransactionProduction.Paused = true
			}
			if err := f.r.Update(context.Background(), f.parent); err != nil {
				t.Fatal(err)
			}
			f.rpc.inclusion = Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
			f.reconcile(t)
			f.reconcile(t)
			if got := f.ledger(t).Status; got.Confirmed != 1 || got.Phase != "Paused" || f.rpc.sends != 1 {
				t.Fatalf("pause lost evidence or sent work: %#v", got)
			}
		})
	}
}

func TestExternalNonceChangeAndFailedExecutionRemainClosed(t *testing.T) {
	f := fixture(t)
	f.reconcile(t)
	f.rpc.inclusion = Inclusion{Found: true, Success: false, BlockID: strings.Repeat("b", 64)}
	f.reconcile(t)
	if got := f.ledger(t).Status; got.Phase != "Blocked" || got.Confirmed != 0 || !got.Outstanding {
		t.Fatal("failed execution counted as a transfer")
	}
	f.rpc.inclusion.Success = true
	f.reconcile(t)
	f.now = f.now.Add(time.Minute)
	f.rpc.nonce = 4
	f.reconcile(t)
	if f.ledger(t).Status.Phase != "Blocked" || f.rpc.sends != 1 {
		t.Fatal("external nonce drift was silently adopted")
	}
}

func TestAdmissionRejectsStaleOrForeignIngress(t *testing.T) {
	for name, change := range map[string]func(*productionFixture){
		"parent generation": func(f *productionFixture) { f.parent.Generation++ },
		"leaf owner":        func(f *productionFixture) { f.actor.OwnerReferences = nil },
		"pod owner":         func(f *productionFixture) { f.pod.OwnerReferences = nil },
		"image":             func(f *productionFixture) { f.pod.Spec.Containers[0].Image = "unqualified" },
		"config profile":    func(f *productionFixture) { f.r.Profile.ConfigDigest = "sha256:" + strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t)
			change(f)
			for _, o := range []client.Object{f.parent, f.actor, f.pod} {
				if err := f.r.Update(context.Background(), o); err != nil {
					t.Fatal(err)
				}
			}
			f.reconcile(t)
			if f.rpc.sends != 0 {
				t.Fatal("stale ingress authorized submission")
			}
		})
	}
}

// failingStatusClient simulates an API write acknowledgement lost after the CAS committed.
type failingStatusClient struct {
	client.Client
	failArm      bool
	failAccepted bool
	failRejected bool
	dropRejected bool
}

func (c *failingStatusClient) Status() client.SubResourceWriter {
	return &failingStatusWriter{SubResourceWriter: c.Client.Status(), owner: c}
}

// failingStatusWriter preserves server mutation while suppressing selected acknowledgements.
type failingStatusWriter struct {
	client.SubResourceWriter
	owner *failingStatusClient
}

func (w *failingStatusWriter) Patch(ctx context.Context, o client.Object, p client.Patch, options ...client.SubResourcePatchOption) error {
	if w.owner.dropRejected && o.(*stacksv1alpha1.StacksTransactionProduction).Status.RejectionReason != "" {
		w.owner.dropRejected = false
		return fmt.Errorf("API unavailable before persisting rejection")
	}
	if err := w.SubResourceWriter.Patch(ctx, o, p, options...); err != nil {
		return err
	}
	status := o.(*stacksv1alpha1.StacksTransactionProduction).Status
	if status.Outstanding && ((w.owner.failArm && !status.Accepted) || (w.owner.failAccepted && status.Accepted) || (w.owner.failRejected && status.RejectionReason != "")) {
		w.owner.failArm, w.owner.failAccepted, w.owner.failRejected = false, false, false
		return fmt.Errorf("write acknowledgement lost")
	}
	return nil
}

func TestLostArmAcknowledgementNeverSends(t *testing.T) {
	f := fixture(t)
	f.r.Client = &failingStatusClient{Client: f.r.Client, failArm: true}
	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
	if err == nil || f.rpc.sends != 0 || !f.ledger(t).Status.Outstanding {
		t.Fatal("lost arm acknowledgement authorized a send")
	}
	f.reconcile(t)
	if f.rpc.sends != 0 {
		t.Fatal("replacement reconcile resent a possibly armed transaction")
	}
}

func TestLostAcceptedAcknowledgementDoesNotLoseInclusion(t *testing.T) {
	f := fixture(t)
	f.r.Client = &failingStatusClient{Client: f.r.Client, failAccepted: true}
	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
	if err == nil || f.rpc.sends != 1 {
		t.Fatal("test did not lose the acknowledgement")
	}
	f.rpc.inclusion = Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	f.reconcile(t)
	if f.ledger(t).Status.Confirmed != 1 || f.rpc.sends != 1 {
		t.Fatal("accounting lost the exact inclusion")
	}
}

// barrierSigner holds both workers after they read the same idle ledger version.
type barrierSigner struct {
	arrived chan struct{}
	release chan struct{}
}

func (s barrierSigner) Sign(ctx context.Context, policy stacksv1alpha1.TransferPolicy, nonce int64) (SignedTransfer, error) {
	s.arrived <- struct{}{}
	select {
	case <-s.release:
		return fakeSigner{}.Sign(ctx, policy, nonce)
	case <-ctx.Done():
		return SignedTransfer{}, ctx.Err()
	}
}

func TestConcurrentWorkersAuthorizeOnlyOneSubmission(t *testing.T) {
	f := fixture(t)
	barrier := barrierSigner{arrived: make(chan struct{}, 2), release: make(chan struct{})}
	first, second := *f.r, *f.r
	otherRPC := &fakeRPC{}
	first.Signer, second.Signer, second.RPC = barrier, barrier, otherRPC
	results := make(chan error, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, r := range []*Reconciler{&first, &second} {
		go func() {
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-barrier.arrived:
		case <-ctx.Done():
			t.Fatal("workers did not reach the same authorization boundary")
		}
	}
	close(barrier.release)
	conflicts := 0
	for range 2 {
		err := <-results
		if apierrors.IsConflict(err) {
			conflicts++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if conflicts != 1 || f.rpc.sends+otherRPC.sends != 1 || !f.ledger(t).Status.Outstanding {
		t.Fatal("optimistic authorization did not exclude the second worker")
	}
}

func TestRejectedSubmissionRetainsNonceAndStillObservesExactInclusion(t *testing.T) {
	f := fixture(t)
	f.rpc.submitError = &submissionRejection{reason: "FeeTooLow"}
	f.reconcile(t)
	original := f.ledger(t).Status
	if original.Phase != "Blocked" || original.RejectionReason != "FeeTooLow" || original.Accepted || !original.Outstanding || *original.NextNonce != original.Nonce {
		t.Fatalf("rejection not retained: %#v", original)
	}
	replacement := *f.r
	f.r = &replacement
	f.now = f.now.Add(time.Hour)
	f.reconcile(t)
	// Unavailable ingress must not erase the known rejection or authorize another POST.
	f.r.Profile.ConfigDigest = "sha256:" + strings.Repeat("f", 64)
	f.reconcile(t)
	if got := f.ledger(t).Status; got.Phase != "Blocked" || got.RejectionReason != original.RejectionReason || got.TxID != original.TxID || !got.Outstanding || f.rpc.sends != 1 {
		t.Fatalf("rejection lost or retried: %#v", got)
	}
	f.r.Profile.ConfigDigest = f.actor.Spec.Config.SecretRef.ExpectedDigest
	f.rpc.inclusion = Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	f.reconcile(t)
	if got := f.ledger(t).Status; got.Confirmed != 1 || got.Outstanding || got.RejectionReason != "FeeTooLow" {
		t.Fatal("known rejection prevented later exact inclusion accounting")
	}
	f.rpc.nonce = 1
	f.rpc.submitError = nil
	f.reconcile(t)
	if got := f.ledger(t).Status; got.RejectionReason != "" || !got.Accepted || f.rpc.sends != 2 {
		t.Fatal("new transaction inherited old rejection evidence")
	}
}

func TestRejectionWriteFailureDoesNotAuthorizeAnotherSubmission(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			f := fixture(t)
			f.rpc.submitError = &submissionRejection{reason: "BadNonce"}
			f.r.Client = &failingStatusClient{Client: f.r.Client, failRejected: committed, dropRejected: !committed}
			_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
			if err == nil || f.rpc.sends != 1 {
				t.Fatal("test did not lose the rejection write")
			}
			replacement := *f.r
			f.r = &replacement
			f.reconcile(t)
			got := f.ledger(t).Status
			if !got.Outstanding || f.rpc.sends != 1 {
				t.Fatal("failed rejection write released the nonce or retried")
			}
			if committed && (got.RejectionReason != "BadNonce" || got.Phase != "Blocked") {
				t.Fatal("durable rejection lost after acknowledgement failure")
			}
			if !committed && (got.RejectionReason != "" || got.Phase != "Ambiguous") {
				t.Fatal("unpersisted rejection was reconstructed after restart")
			}
		})
	}
}

func TestNonceMismatchIsReevaluatedWithoutAdoptingAnotherNonce(t *testing.T) {
	f := fixture(t)
	f.rpc.nonce = 5
	f.reconcile(t)
	f.rpc.inclusion = Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	f.reconcile(t)
	f.now = f.now.Add(time.Minute)
	for _, nonce := range []int64{5, 9} {
		f.rpc.nonce = nonce
		f.reconcile(t)
		if got := f.ledger(t).Status; got.Phase != "Blocked" || *got.NextNonce != 6 || f.rpc.sends != 1 {
			t.Fatal("mismatched ingress nonce was adopted")
		}
	}
	f.rpc.nonce = 6
	f.reconcile(t)
	if got := f.ledger(t).Status; got.Nonce != 6 || !got.Outstanding || !got.Accepted || f.rpc.sends != 2 {
		t.Fatal("exact nonce match did not resume production")
	}
}
