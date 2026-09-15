package networkruntime

import (
	"context"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// operationProjection isolates the runtime projection from bootstrap execution in the aggregate writer test.
type operationProjection struct {
	reconciler   *Reconciler
	participants []api.StacksNetworkParticipant
	now          time.Time
}

// Reconcile invokes the real protocol/diagnostic projection with controlled observations.
func (p *operationProjection) Reconcile(ctx context.Context, root *api.StacksNetwork) (ctrl.Result, error) {
	return ctrl.Result{}, p.reconciler.projectOperation(ctx, root, p.participants, p.now)
}

// rootPatchClient counts actual root status patches made by the aggregate.
type rootPatchClient struct {
	client.Client
	writes int
}

// Status wraps the original writer, preserving its merge/conflict semantics.
func (c *rootPatchClient) Status() client.SubResourceWriter {
	return rootPatchWriter{SubResourceWriter: c.Client.Status(), owner: c}
}

// rootPatchWriter records only root patches, not participant status operations.
type rootPatchWriter struct {
	client.SubResourceWriter
	owner *rootPatchClient
}

// Patch forwards each write to the real client implementation.
func (w rootPatchWriter) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.SubResourcePatchOption,
) error {
	if _, ok := obj.(*api.StacksNetwork); ok {
		w.owner.writes++
	}
	return w.SubResourceWriter.Patch(ctx, obj, patch, opts...)
}

// TestBurnchainTimestampRefreshDoesNotPatchRoot exercises the aggregate's real equality/patch path.
func TestBurnchainTimestampRefreshDoesNotPatchRoot(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root, g, ps, execution := operationalFixture(t, now)
	root.Finalizers = []string{"network.stacks.org/foundation"}
	set(root, "Initialized", metav1.ConditionTrue, "BootstrapCompleted", "complete")
	initial := &bitcoin.BitcoinInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "initial",
			Namespace:       root.Namespace,
			UID:             "initial",
			OwnerReferences: g.OwnerReferences,
		},
		Spec: bitcoin.BitcoinInitializationSpec{NetworkUID: root.UID},
	}
	root.Status.Bitcoin.InitializationRef = &common.Binding{
		Kind: "BitcoinInitialization",
		Name: initial.Name,
		UID:  initial.UID,
	}
	objects := []client.Object{root, g, initial, execution}
	for i := range ps {
		objects = append(objects, &ps[i])
	}
	r := fixtureReconciler(t)
	c := &rootPatchClient{
		Client: fake.NewClientBuilder().
			WithScheme(r.Scheme).
			WithStatusSubresource(&api.StacksNetwork{}, &api.StacksNetworkParticipant{}, &bitcoin.BitcoinExecution{}).
			WithObjects(objects...).
			Build(),
	}
	r.Client, r.Reader = c, c
	projection := &operationProjection{reconciler: r, participants: ps, now: now}
	aggregate := &foundation.Reconciler{Client: c, Reader: c, Scheme: r.Scheme, Runtime: projection}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(root)}
	reconcile := func() {
		t.Helper()
		if _, err := aggregate.Reconcile(t.Context(), request); err != nil {
			t.Fatal(err)
		}
		if err := c.Get(t.Context(), request.NamespacedName, root); err != nil {
			t.Fatal(err)
		}
	}
	reconcile()
	reconcile()
	baseline := c.writes
	if root.Status.BurnchainObservations == nil || root.Status.BurnchainObservations.Bitcoin == nil {
		t.Fatal("projection not exercised")
	}
	first := root.Status.BurnchainObservations.Bitcoin.FirstObservedAt
	for i := 1; i <= 5; i++ {
		projection.now = now.Add(time.Duration(i) * 2 * time.Second)
		execution.Status.Observation.ObservedAt = metav1.NewTime(projection.now)
		projection.participants[0].Status.Runtime.Protocol.ObservedAt = metav1.NewTime(projection.now)
		if err := c.Status().Update(t.Context(), execution); err != nil {
			t.Fatal(err)
		}
		reconcile()
	}
	if c.writes != baseline || !root.Status.BurnchainObservations.Bitcoin.FirstObservedAt.Equal(&first) {
		t.Fatalf("timestamp-only source updates patched root: %d -> %d", baseline, c.writes)
	}
	execution.Status.Observation.Height++
	if err := c.Status().Update(t.Context(), execution); err != nil {
		t.Fatal(err)
	}
	reconcile()
	if c.writes != baseline+1 ||
		!root.Status.BurnchainObservations.Bitcoin.FirstObservedAt.Equal(&execution.Status.Observation.ObservedAt) {
		t.Fatal("height change did not publish a new sequence")
	}
}

// TestBurnchainSequenceUsesCurrentSourceFreshness verifies retention beyond the freshness window and recovery.
func TestBurnchainSequenceUsesCurrentSourceFreshness(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root, g, ps, execution := operationalFixture(t, now)
	r := fixtureReconciler(t, execution)
	root.Status.BurnchainObservations = r.burnchainObservations(t.Context(), root, g, ps, now)
	first := root.Status.BurnchainObservations.Bitcoin.FirstObservedAt
	refresh := func(at time.Time) {
		t.Helper()
		execution.Status.Observation.ObservedAt = metav1.NewTime(at)
		ps[0].Status.Runtime.Protocol.ObservedAt = metav1.NewTime(at)
		if err := r.Client.Update(t.Context(), execution); err != nil {
			t.Fatal(err)
		}
	}
	later := now.Add(time.Minute)
	refresh(later)
	root.Status.BurnchainObservations = r.burnchainObservations(t.Context(), root, g, ps, later)
	if root.Status.BurnchainObservations == nil ||
		!root.Status.BurnchainObservations.Bitcoin.FirstObservedAt.Equal(&first) {
		t.Fatal("sequence start was incorrectly used for source freshness")
	}
	// Unavailable samples disappear, rather than keeping an apparently current historical height.
	root.Status.BurnchainObservations = r.burnchainObservations(t.Context(), root, g, ps, later.Add(17*time.Second))
	if root.Status.BurnchainObservations != nil {
		t.Fatal("empty or stale diagnostic retained")
	}
	recovered := later.Add(20 * time.Second)
	refresh(recovered)
	root.Status.BurnchainObservations = r.burnchainObservations(t.Context(), root, g, ps, recovered)
	if root.Status.BurnchainObservations == nil ||
		!root.Status.BurnchainObservations.Bitcoin.FirstObservedAt.Equal(&execution.Status.Observation.ObservedAt) {
		t.Fatal("recovery inherited stale sequence")
	}
	// Early lifecycle returns must withdraw a previous diagnostic too.
	root.Spec.Operation = "Stopped"
	if _, err := r.Reconcile(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if root.Status.BurnchainObservations != nil {
		t.Fatal("stopped network kept diagnostic")
	}
}

// TestRetainedHeightSequenceResetsOnEveryIdentityField protects replacement and reconfiguration attribution.
func TestRetainedHeightSequenceResetsOnEveryIdentityField(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	previous := &api.BurnHeightSample{
		Height:              100,
		FirstObservedAt:     metav1.NewTime(now),
		Participant:         common.Binding{Name: "actor", UID: "participant"},
		Pod:                 common.Binding{Name: "actor-0", UID: "pod"},
		ContainerID:         "process",
		ConfigurationDigest: "config",
	}
	for _, mode := range []string{"height", "participant", "pod", "container", "config", "unchanged"} {
		t.Run(mode, func(t *testing.T) {
			current := previous.DeepCopy()
			current.FirstObservedAt = metav1.NewTime(now.Add(time.Second))
			switch mode {
			case "height":
				current.Height++
			case "participant":
				current.Participant.UID = "replacement"
			case "pod":
				current.Pod.UID = "replacement"
			case "container":
				current.ContainerID = "replacement"
			case "config":
				current.ConfigurationDigest = "replacement"
			}
			got := retainHeightSample(previous, current)
			want := now.Add(time.Second)
			if mode == "unchanged" {
				want = now
			}
			if !got.FirstObservedAt.Time.Equal(want) || !previous.FirstObservedAt.Time.Equal(now) {
				t.Fatal("wrong sequence or mutated prior sample")
			}
		})
	}
}

// TestBurnchainProjectionBoundaryUsesGateCompletion distinguishes it from final Initialized=True.
func TestBurnchainProjectionBoundaryUsesGateCompletion(t *testing.T) {
	now := time.Now()
	root, g, ps, execution := operationalFixture(t, now)
	r := fixtureReconciler(t, g, execution)
	root.Status.Initialization.Completed = false
	root.Status.BurnchainObservations = &api.BurnchainObservations{Bitcoin: &api.BurnHeightSample{Height: 1}}
	if err := r.projectOperation(t.Context(), root, ps, now); err != nil {
		t.Fatal(err)
	}
	if root.Status.BurnchainObservations != nil {
		t.Fatal("diagnostic projected before gate completion")
	}
	root.Status.Initialization.Completed = true
	ps[2].Status.Execution.Traffic.Found = false
	if err := r.projectOperation(t.Context(), root, ps, now); err != nil {
		t.Fatal(err)
	}
	if root.Status.BurnchainObservations == nil {
		t.Fatal("diagnostic blocked on final transfer")
	}
}
