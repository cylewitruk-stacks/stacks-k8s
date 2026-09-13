package observation

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	observationv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/topology"
)

func TestReconcilePublishesReadySnapshot(t *testing.T) {
	object, reconciler, observer := testReconciler(t, topology.Snapshot{
		Binding: observationv1alpha1.NetworkBinding{
			Name:               "network",
			UID:                "network-uid",
			ObservedGeneration: 3,
			InventoryDigest:    testDigest,
		},
		Actors: []observationv1alpha1.ObservedActorIdentity{
			{Kind: "BitcoinNode", Name: "bitcoin", EvidenceClass: "orchestrator-observed"},
		},
	}, nil)
	result, err := reconciler.Reconcile(
		context.Background(),
		ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)},
	)
	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	updated := getObservation(t, reconciler.Client, object)
	if updated.Status.Phase != observationv1alpha1.ObservationReady || updated.Status.Binding == nil ||
		len(updated.Status.Actors) != 1 {
		t.Fatalf("status = %#v", updated.Status)
	}
	if observer.calls != 1 || updated.Status.CompletedAt == nil {
		t.Fatalf("calls = %d, completed = %v", observer.calls, updated.Status.CompletedAt)
	}
}

func TestReconcileKeepsIncompleteTopologyPending(t *testing.T) {
	object, reconciler, _ := testReconciler(
		t,
		topology.Snapshot{},
		&topology.NotReadyError{Reason: "inventory incomplete"},
	)
	result, err := reconciler.Reconcile(
		context.Background(),
		ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)},
	)
	if err != nil || result.RequeueAfter != pendingRequeue {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	updated := getObservation(t, reconciler.Client, object)
	if updated.Status.Phase != observationv1alpha1.ObservationPending || updated.Status.CompletedAt != nil {
		t.Fatalf("status = %#v", updated.Status)
	}
}

func TestReconcileTerminatesInconclusiveAndDoesNotRepeat(t *testing.T) {
	object, reconciler, observer := testReconciler(
		t,
		topology.Snapshot{},
		&topology.InconclusiveError{Reason: "identity changed"},
	)
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	updated := getObservation(t, reconciler.Client, object)
	if updated.Status.Phase != observationv1alpha1.ObservationInconclusive || updated.Status.CompletedAt == nil ||
		observer.calls != 1 {
		t.Fatalf("status = %#v, calls = %d", updated.Status, observer.calls)
	}
}

func TestReconcilePublishesPendingToInconclusiveConditionTransition(t *testing.T) {
	object, reconciler, observer := testReconciler(
		t,
		topology.Snapshot{},
		&topology.NotReadyError{Reason: "inventory incomplete"},
	)
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	observer.err = &topology.InconclusiveError{Reason: "identity changed"}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	updated := getObservation(t, reconciler.Client, object)
	ready := meta.FindStatusCondition(updated.Status.Conditions, "Ready")
	if updated.Status.Phase != observationv1alpha1.ObservationInconclusive || ready == nil ||
		ready.Status != metav1.ConditionFalse ||
		ready.Reason != "IdentityNotEstablished" {
		t.Fatalf("status = %#v", updated.Status)
	}
}

func TestReconcilePublishesPendingToReadyConditionTransition(t *testing.T) {
	object, reconciler, observer := testReconciler(
		t,
		topology.Snapshot{},
		&topology.NotReadyError{Reason: "inventory incomplete"},
	)
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	observer.err = nil
	observer.snapshot = topology.Snapshot{
		Binding: observationv1alpha1.NetworkBinding{
			Name:               "network",
			UID:                "network-uid",
			ObservedGeneration: 3,
			InventoryDigest:    testDigest,
		},
		Actors: []observationv1alpha1.ObservedActorIdentity{
			{Kind: "BitcoinNode", Name: "bitcoin", EvidenceClass: "orchestrator-observed"},
		},
	}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	updated := getObservation(t, reconciler.Client, object)
	ready := meta.FindStatusCondition(updated.Status.Conditions, "Ready")
	if updated.Status.Phase != observationv1alpha1.ObservationReady || ready == nil ||
		ready.Status != metav1.ConditionTrue ||
		ready.Reason != "IdentityVerified" {
		t.Fatalf("status = %#v", updated.Status)
	}
}

func TestReconcileTerminatesWhenPendingDeadlineExpires(t *testing.T) {
	object, reconciler, _ := testReconciler(
		t,
		topology.Snapshot{},
		&topology.NotReadyError{Reason: "inventory incomplete"},
	)
	now := time.Unix(1_700_000_000, 0).UTC()
	started := metav1.NewTime(now.Add(-5 * time.Minute))
	current := getObservation(t, reconciler.Client, object)
	current.Status = observationv1alpha1.NetworkObservationStatus{
		ObservedGeneration: current.Generation,
		Phase:              observationv1alpha1.ObservationPending,
		StartedAt:          &started,
	}
	if err := reconciler.Client.Status().Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(
		context.Background(),
		ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)},
	); err != nil {
		t.Fatal(err)
	}
	updated := getObservation(t, reconciler.Client, object)
	if updated.Status.Phase != observationv1alpha1.ObservationInconclusive || updated.Status.CompletedAt == nil {
		t.Fatalf("status = %#v", updated.Status)
	}
}

func TestTopologyEventEnqueuesOnlyPendingObservationsForNetwork(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := observationv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	pending := &observationv1alpha1.NetworkObservation{
		ObjectMeta: metav1.ObjectMeta{Name: "pending", Namespace: "test"},
		Spec: observationv1alpha1.NetworkObservationSpec{
			NetworkRef: observationv1alpha1.LocalObjectReference{Name: "network"},
		},
	}
	terminal := pending.DeepCopy()
	terminal.Name = "terminal"
	terminal.Generation = 1
	terminal.Status = observationv1alpha1.NetworkObservationStatus{
		ObservedGeneration: 1,
		Phase:              observationv1alpha1.ObservationReady,
	}
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pending, terminal).
		WithIndex(
			&observationv1alpha1.NetworkObservation{},
			networkReferenceField,
			func(object client.Object) []string {
				return []string{object.(*observationv1alpha1.NetworkObservation).Spec.NetworkRef.Name}
			},
		).
		Build()
	reconciler := &Reconciler{Client: kubeClient}
	network := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "network.stacks.org/v1alpha1",
			"kind":       "StacksNetwork",
			"metadata":   map[string]any{"name": "network", "namespace": "test"},
		},
	}
	requests := reconciler.observationsForNetwork(context.Background(), network)
	if len(requests) != 1 || requests[0].Name != "pending" {
		t.Fatalf("requests = %#v", requests)
	}
}

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type stubObserver struct {
	snapshot topology.Snapshot
	err      error
	calls    int
	expected string
}

func (s *stubObserver) Observe(_ context.Context, _, _, expected string) (topology.Snapshot, error) {
	s.expected = expected
	s.calls++
	return s.snapshot, s.err
}

func testReconciler(
	t *testing.T,
	snapshot topology.Snapshot,
	observeErr error,
) (*observationv1alpha1.NetworkObservation, *Reconciler, *stubObserver) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := observationv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	object := &observationv1alpha1.NetworkObservation{
		ObjectMeta: metav1.ObjectMeta{Name: "observation", Namespace: "test", Generation: 1},
		Spec: observationv1alpha1.NetworkObservationSpec{
			NetworkRef: observationv1alpha1.LocalObjectReference{Name: "network"},
		},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(object).WithObjects(object).Build()
	observer := &stubObserver{snapshot: snapshot, err: observeErr}
	reconciler := &Reconciler{
		Client:           kubeClient,
		TopologyObserver: observer,
		Now:              func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	}
	return object, reconciler, observer
}

func getObservation(
	t *testing.T,
	kubeClient client.Client,
	object *observationv1alpha1.NetworkObservation,
) *observationv1alpha1.NetworkObservation {
	t.Helper()
	updated := &observationv1alpha1.NetworkObservation{}
	if err := kubeClient.Get(
		context.Background(),
		types.NamespacedName{Namespace: object.Namespace, Name: object.Name},
		updated,
	); err != nil {
		t.Fatal(err)
	}
	return updated
}

func TestReconcileRejectsLegacyExpectationWithoutObservation(t *testing.T) {
	object, reconciler, observer := testReconciler(t, topology.Snapshot{}, nil)
	object.Spec.ExpectedInventoryDigest = testDigest
	if err := reconciler.Update(t.Context(), object); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(
		t.Context(),
		ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)},
	); err != nil {
		t.Fatal(err)
	}
	actual := getObservation(t, reconciler.Client, object)
	if actual.Status.Phase != observationv1alpha1.ObservationInconclusive || observer.calls != 0 {
		t.Fatal("legacy expectation reached participant reader")
	}
}

func TestReconcilePinsSnapshotAndPreservesCompletion(t *testing.T) {
	snapshot := topology.Snapshot{
		Binding: observationv1alpha1.NetworkBinding{
			Name:               "network",
			UID:                "root",
			ObservedGeneration: 1,
			NetworkAPIVersion:  "network.stacks.org/v1alpha2",
			SnapshotDigest:     testDigest,
		},
	}
	object, reconciler, observer := testReconciler(t, snapshot, nil)
	object.Spec.ExpectedSnapshotDigest = testDigest
	if err := reconciler.Update(t.Context(), object); err != nil {
		t.Fatal(err)
	}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)}
	if _, err := reconciler.Reconcile(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if observer.expected != testDigest {
		t.Fatalf("snapshot expectation = %q", observer.expected)
	}
	actual := getObservation(t, reconciler.Client, object)
	if actual.Status.Binding == nil || actual.Status.Binding.SnapshotDigest != testDigest ||
		actual.Status.Binding.InventoryDigest != "" {
		t.Fatalf("binding: %+v", actual.Status.Binding)
	}
	if _, err := reconciler.Reconcile(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if observer.calls != 1 {
		t.Fatal("reconciliation overwrote completed observation")
	}
}
