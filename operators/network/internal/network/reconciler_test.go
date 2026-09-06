package network

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
)

func TestReconcileCreatesCorrectsAndPrunesLeafResources(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	network := fixture()
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}, &networkv1alpha1.BitcoinNode{}, &networkv1alpha1.StacksNode{}, &networkv1alpha1.StacksSigner{}).WithObjects(network).Build()
	reconciler := &Reconciler{Client: kubeClient, APIReader: kubeClient, Scheme: scheme, Now: func() time.Time { return time.Unix(1, 0) }}
	request := reconcile.Request{NamespacedName: clientObjectKey(network)}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	bitcoin := &networkv1alpha1.BitcoinNode{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: "test", Name: "testnet-bitcoin"}, bitcoin); err != nil {
		t.Fatal(err)
	}
	if metav1.GetControllerOf(bitcoin) == nil || metav1.GetControllerOf(bitcoin).UID != network.UID {
		t.Fatal("compiled child is not owned by the network")
	}
	bitcoin.Spec.Image = "tampered:image"
	if err := kubeClient.Update(ctx, bitcoin); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(bitcoin), bitcoin); err != nil {
		t.Fatal(err)
	}
	if bitcoin.Spec.Image != "bitcoin:test" {
		t.Fatalf("direct child edit survived: %q", bitcoin.Spec.Image)
	}
	current := &networkv1alpha1.StacksNetwork{}
	if err := kubeClient.Get(ctx, request.NamespacedName, current); err != nil {
		t.Fatal(err)
	}
	driftedSigner := &networkv1alpha1.StacksSigner{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: "test", Name: "testnet-signer-1"}, driftedSigner); err != nil {
		t.Fatal(err)
	}
	driftedSigner.Labels = map[string]string{"network.stacks.org/network": "wrong"}
	if err := kubeClient.Update(ctx, driftedSigner); err != nil {
		t.Fatal(err)
	}
	current.Spec.Signers = nil
	current.Spec.StacksNodes = current.Spec.StacksNodes[:1]
	if err := kubeClient.Update(ctx, current); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: "test", Name: "testnet-signer-1"}, &networkv1alpha1.StacksSigner{}); client.IgnoreNotFound(err) != nil || err == nil {
		t.Fatalf("retired signer lookup = %v", err)
	}
	if err := kubeClient.Get(ctx, request.NamespacedName, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Phase != "Progressing" || current.Status.InventoryReady || current.Status.InventoryDigest != "" {
		t.Fatalf("retirement status = %#v", current.Status)
	}
	if current.Status.TargetDeclarations == nil || len(current.Status.TargetDeclarations.Actors) != 2 {
		t.Fatalf("retirement lost the current two-actor catalog: %#v", current.Status.TargetDeclarations)
	}
}

func TestReconcileRefusesUnownedChildCollision(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	network := fixture()
	collision := &networkv1alpha1.BitcoinNode{ObjectMeta: metav1.ObjectMeta{Name: "testnet-bitcoin", Namespace: "test", UID: "other"}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}).WithObjects(network, collision).Build()
	reconciler := &Reconciler{Client: kubeClient, APIReader: kubeClient, Scheme: scheme, Now: time.Now}
	if _, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clientObjectKey(network)}); err == nil {
		t.Fatal("Reconcile adopted an unowned collision")
	}
	current := &networkv1alpha1.StacksNetwork{}
	if err := kubeClient.Get(ctx, clientObjectKey(network), current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Phase != "Degraded" || current.Status.InventoryDigest != "" {
		t.Fatalf("collision status = %#v", current.Status)
	}
}

func TestTransientSynchronizationConflictPreservesLastStatus(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	network := fixture()
	network.Status = networkv1alpha1.StacksNetworkStatus{ObservedGeneration: network.Generation, Phase: "Ready", InventoryReady: true,
		InventoryDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}).WithObjects(network).Build()
	reconciler := &Reconciler{Client: &conflictListClient{Client: base}, APIReader: base, Scheme: scheme, Now: time.Now}
	_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clientObjectKey(network)})
	if !errors.IsConflict(err) {
		t.Fatalf("synchronization error = %v", err)
	}
	current := &networkv1alpha1.StacksNetwork{}
	if err := base.Get(ctx, clientObjectKey(network), current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Phase != "Ready" || !current.Status.InventoryReady || current.Status.InventoryDigest != network.Status.InventoryDigest {
		t.Fatalf("transient conflict rewrote status: %#v", current.Status)
	}
}

func TestReconcileReportsDerivedNameCollisionAndClearsReadyCondition(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	network := fixture()
	network.Status = networkv1alpha1.StacksNetworkStatus{
		ObservedGeneration: network.Generation, Phase: "Ready", InventoryReady: true, InventoryDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: network.Generation, Reason: "TopologyReady"},
			{Type: "Suspended", Status: metav1.ConditionTrue, ObservedGeneration: network.Generation, Reason: "NetworkSuspended"},
		},
	}
	controller := true
	collision := &networkv1alpha1.BitcoinNode{ObjectMeta: metav1.ObjectMeta{
		Name: "testnet-bitcoin", Namespace: "test", UID: "other",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: networkv1alpha1.GroupVersion.String(), Kind: "StacksNetwork", Name: "testnet-bitcoin-owner", UID: "other-network", Controller: &controller}},
	}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}).WithObjects(network, collision).Build()
	reconciler := &Reconciler{Client: kubeClient, APIReader: kubeClient, Scheme: scheme, Now: time.Now}
	_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clientObjectKey(network)})
	if err == nil || !strings.Contains(err.Error(), "derived resource name collision") {
		t.Fatalf("collision error = %v", err)
	}
	current := &networkv1alpha1.StacksNetwork{}
	if err := kubeClient.Get(ctx, clientObjectKey(network), current); err != nil {
		t.Fatal(err)
	}
	ready := meta.FindStatusCondition(current.Status.Conditions, "Ready")
	suspended := meta.FindStatusCondition(current.Status.Conditions, "Suspended")
	if current.Status.Phase != "Degraded" || current.Status.InventoryDigest != "" || ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != "ReconciliationFailed" || suspended == nil || suspended.Status != metav1.ConditionFalse {
		t.Fatalf("collision status = %#v", current.Status)
	}
}

func TestInventoryIsCompleteStableAndWithdrawnOnPartialAdmission(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	network := fixture()
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}, &networkv1alpha1.BitcoinNode{}, &networkv1alpha1.StacksNode{}, &networkv1alpha1.StacksSigner{}).WithObjects(network).Build()
	now := time.Unix(42, 0).UTC()
	reconciler := &Reconciler{Client: kubeClient, APIReader: kubeClient, Scheme: scheme, Now: func() time.Time { return now }}
	request := reconcile.Request{NamespacedName: clientObjectKey(network)}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	setReadyStatuses(t, ctx, kubeClient)
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	current := &networkv1alpha1.StacksNetwork{}
	if err := kubeClient.Get(ctx, request.NamespacedName, current); err != nil {
		t.Fatal(err)
	}
	firstDigest := current.Status.InventoryDigest
	if !current.Status.InventoryReady || firstDigest == "" || len(current.Status.Actors) != 4 {
		t.Fatalf("ready inventory = %#v", current.Status)
	}
	if current.Status.InventoryObservedAt == nil || !current.Status.InventoryObservedAt.Time.Equal(now) {
		t.Fatalf("observation time = %v", current.Status.InventoryObservedAt)
	}
	follower := &networkv1alpha1.StacksNode{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: "test", Name: "testnet-follower"}, follower); err != nil {
		t.Fatal(err)
	}
	follower.Status.Ready = false
	follower.Status.Identity = nil
	if err := kubeClient.Status().Update(ctx, follower); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(ctx, request.NamespacedName, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.InventoryReady || current.Status.InventoryDigest != "" || current.Status.InventoryObservedAt != nil || len(current.Status.Actors) != 0 {
		t.Fatalf("partial inventory was not withdrawn: %#v", current.Status)
	}
}

func TestReconcileSuspensionPublishesCanonicalConditions(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	network := fixture()
	network.Spec.Suspended = true
	network.Status = networkv1alpha1.StacksNetworkStatus{Phase: "Ready", InventoryReady: true, InventoryDigest: strings.Repeat("a", 64), Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "TopologyReady"}}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}, &networkv1alpha1.BitcoinNode{}, &networkv1alpha1.StacksNode{}, &networkv1alpha1.StacksSigner{}).WithObjects(network).Build()
	reconciler := &Reconciler{Client: kubeClient, APIReader: kubeClient, Scheme: scheme, Now: time.Now}
	if _, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clientObjectKey(network)}); err != nil {
		t.Fatal(err)
	}
	current := &networkv1alpha1.StacksNetwork{}
	if err := kubeClient.Get(ctx, clientObjectKey(network), current); err != nil {
		t.Fatal(err)
	}
	ready, suspended := meta.FindStatusCondition(current.Status.Conditions, "Ready"), meta.FindStatusCondition(current.Status.Conditions, "Suspended")
	if current.Status.Phase != "Suspended" || current.Status.InventoryReady || current.Status.InventoryDigest != "" || ready == nil || ready.Status != metav1.ConditionFalse || suspended == nil || suspended.Status != metav1.ConditionTrue {
		t.Fatalf("suspended status = %#v", current.Status)
	}
	current.Spec.Suspended = false
	current.Spec.Signers = nil
	if err := kubeClient.Update(ctx, current); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clientObjectKey(network)}); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(ctx, clientObjectKey(network), current); err != nil {
		t.Fatal(err)
	}
	suspended = meta.FindStatusCondition(current.Status.Conditions, "Suspended")
	if current.Status.Phase != "Progressing" || suspended == nil || suspended.Status != metav1.ConditionFalse || suspended.Reason != "NetworkActive" {
		t.Fatalf("active transition retained suspension: %#v", current.Status)
	}
}

func TestReconcileWithdrawsReadyInventoryOnDirectReadFailure(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	network := fixture()
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}, &networkv1alpha1.BitcoinNode{}, &networkv1alpha1.StacksNode{}, &networkv1alpha1.StacksSigner{}).WithObjects(network).Build()
	reconciler := &Reconciler{Client: kubeClient, APIReader: kubeClient, Scheme: scheme, Now: time.Now}
	request := reconcile.Request{NamespacedName: clientObjectKey(network)}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	setReadyStatuses(t, ctx, kubeClient)
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	reconciler.APIReader = &failingListReader{Reader: kubeClient}
	if _, err := reconciler.Reconcile(ctx, request); err == nil {
		t.Fatal("direct-read failure was hidden")
	}
	current := &networkv1alpha1.StacksNetwork{}
	if err := kubeClient.Get(ctx, request.NamespacedName, current); err != nil {
		t.Fatal(err)
	}
	ready := meta.FindStatusCondition(current.Status.Conditions, "Ready")
	if current.Status.Phase != "Degraded" || current.Status.InventoryReady || current.Status.InventoryDigest != "" || len(current.Status.Actors) != 0 || ready == nil || ready.Reason != "ObservationFailed" || ready.Status != metav1.ConditionFalse {
		t.Fatalf("failed observation status = %#v", current.Status)
	}
}

func TestReconcileDoesNotRewriteUnchangedChildrenOnStatusEvent(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	network := fixture()
	base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}, &networkv1alpha1.BitcoinNode{}, &networkv1alpha1.StacksNode{}, &networkv1alpha1.StacksSigner{}).WithObjects(network).Build()
	counting := &childMutationCountingClient{Client: base}
	reconciler := &Reconciler{Client: counting, APIReader: counting, Scheme: scheme, Now: time.Now}
	request := reconcile.Request{NamespacedName: clientObjectKey(network)}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	counting.childMutations = 0
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if counting.childMutations != 0 {
		t.Fatalf("unchanged child mutations = %d", counting.childMutations)
	}
}

func TestDirectObservationRejectsOwnedChildMissingFromCachedPruneView(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	network := fixture()
	cached := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}, &networkv1alpha1.BitcoinNode{}, &networkv1alpha1.StacksNode{}, &networkv1alpha1.StacksSigner{}).WithObjects(network).Build()
	initial := &Reconciler{Client: cached, APIReader: cached, Scheme: scheme, Now: time.Now}
	request := reconcile.Request{NamespacedName: clientObjectKey(network)}
	if _, err := initial.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	setReadyStatuses(t, ctx, cached)
	directObjects := []client.Object{}
	bitcoin := &networkv1alpha1.BitcoinNodeList{}
	stacks := &networkv1alpha1.StacksNodeList{}
	signers := &networkv1alpha1.StacksSignerList{}
	for _, list := range []client.ObjectList{bitcoin, stacks, signers} {
		if err := cached.List(ctx, list, client.InNamespace(network.Namespace)); err != nil {
			t.Fatal(err)
		}
	}
	for index := range bitcoin.Items {
		directObjects = append(directObjects, bitcoin.Items[index].DeepCopy())
	}
	for index := range stacks.Items {
		directObjects = append(directObjects, stacks.Items[index].DeepCopy())
	}
	for index := range signers.Items {
		directObjects = append(directObjects, signers.Items[index].DeepCopy())
	}
	controller := true
	directObjects = append(directObjects, &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{
		Name: "retired", Namespace: network.Namespace, UID: "retired-uid",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: networkv1alpha1.GroupVersion.String(), Kind: "StacksNetwork", Name: network.Name, UID: network.UID, Controller: &controller}},
	}})
	direct := fake.NewClientBuilder().WithScheme(scheme).WithObjects(directObjects...).Build()
	reconciler := &Reconciler{Client: cached, APIReader: direct, Scheme: scheme, Now: time.Now}
	if _, err := reconciler.Reconcile(ctx, request); err == nil || !strings.Contains(err.Error(), "owned leaf set") {
		t.Fatalf("direct owned-child drift error = %v", err)
	}
	current := &networkv1alpha1.StacksNetwork{}
	if err := cached.Get(ctx, request.NamespacedName, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Phase != "Degraded" || current.Status.InventoryReady || current.Status.InventoryDigest != "" {
		t.Fatalf("direct owned-child drift status = %#v", current.Status)
	}
}

type failingListReader struct{ client.Reader }

func (r *failingListReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.NewServiceUnavailable("direct read unavailable")
}

type conflictListClient struct{ client.Client }

func (c *conflictListClient) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.NewConflict(schema.GroupResource{Group: networkv1alpha1.GroupVersion.Group, Resource: "bitcoinnodes"}, "testnet", fmt.Errorf("simulated resource-version conflict"))
}

type childMutationCountingClient struct {
	client.Client
	childMutations int
}

func (c *childMutationCountingClient) Create(ctx context.Context, object client.Object, options ...client.CreateOption) error {
	c.count(object)
	return c.Client.Create(ctx, object, options...)
}

func (c *childMutationCountingClient) Update(ctx context.Context, object client.Object, options ...client.UpdateOption) error {
	c.count(object)
	return c.Client.Update(ctx, object, options...)
}

func (c *childMutationCountingClient) Patch(ctx context.Context, object client.Object, patch client.Patch, options ...client.PatchOption) error {
	c.count(object)
	return c.Client.Patch(ctx, object, patch, options...)
}

func (c *childMutationCountingClient) count(object client.Object) {
	switch object.(type) {
	case *networkv1alpha1.BitcoinNode, *networkv1alpha1.StacksNode, *networkv1alpha1.StacksSigner:
		c.childMutations++
	}
}

func setReadyStatuses(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	bitcoin := &networkv1alpha1.BitcoinNode{}
	mustGet(t, ctx, c, "testnet-bitcoin", bitcoin)
	bitcoin.Status = readyStatus(bitcoin.Generation, "BitcoinNode", "bitcoin", "miner", bitcoin.Name)
	if err := c.Status().Update(ctx, bitcoin); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"follower", "signer-node-1"} {
		object := &networkv1alpha1.StacksNode{}
		mustGet(t, ctx, c, "testnet-"+name, object)
		object.Status = readyStatus(object.Generation, "StacksNode", name, string(object.Spec.Role), object.Name)
		if err := c.Status().Update(ctx, object); err != nil {
			t.Fatal(err)
		}
	}
	signer := &networkv1alpha1.StacksSigner{}
	mustGet(t, ctx, c, "testnet-signer-1", signer)
	signer.Status = readyStatus(signer.Generation, "StacksSigner", "signer-1", "signer", signer.Name)
	if err := c.Status().Update(ctx, signer); err != nil {
		t.Fatal(err)
	}
}

func readyStatus(generation int64, kind, actor, role, resource string) networkv1alpha1.ActorStatus {
	return networkv1alpha1.ActorStatus{ObservedGeneration: generation, Phase: "Ready", Ready: true, Identity: &networkv1alpha1.ActorIdentity{Kind: kind, Name: actor, Role: role, ResourceName: resource, ServiceName: resource, StatefulSetName: resource, StatefulSetUID: "sts-" + actor, ControllerRevision: "revision-" + actor, PodName: resource + "-0", PodUID: "pod-" + actor, RequestedImage: "image:test", RuntimeImageID: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ConfigDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
}
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := networkv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := bitcoinv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}
func clientObjectKey(object client.Object) client.ObjectKey {
	return client.ObjectKeyFromObject(object)
}
func mustGet(t *testing.T, ctx context.Context, c client.Client, name string, object client.Object) {
	t.Helper()
	if err := c.Get(ctx, client.ObjectKey{Namespace: "test", Name: name}, object); err != nil {
		t.Fatal(err)
	}
}
