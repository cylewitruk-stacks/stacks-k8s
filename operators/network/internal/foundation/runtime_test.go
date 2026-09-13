package foundation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// runtimeFunc supplies a projection hook without simulating workload authorization.
type runtimeFunc func(context.Context, *api.StacksNetwork) (ctrl.Result, error)

// Reconcile invokes the test projection.
func (f runtimeFunc) Reconcile(ctx context.Context, root *api.StacksNetwork) (ctrl.Result, error) {
	return f(ctx, root)
}

// runtimeAccess separates root projections from topology reads in regression checks.
type runtimeAccess struct {
	client.Client
	// rootReads counts fresh root reads.
	rootReads int
	// graphReads counts every non-root read/list.
	graphReads int
	// statusWrites counts root/participant status writes.
	statusWrites int
}

// Get records the boundary between the root and its dependency graph.
func (c *runtimeAccess) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*api.StacksNetwork); ok {
		c.rootReads++
	} else {
		c.graphReads++
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

// List records graph inventory reads.
func (c *runtimeAccess) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	c.graphReads++
	return c.Client.List(ctx, obj, opts...)
}

// Status counts writes without changing the underlying fake's patch behavior.
func (c *runtimeAccess) Status() client.SubResourceWriter {
	return runtimeStatusWriter{SubResourceWriter: c.Client.Status(), writes: &c.statusWrites}
}

// runtimeStatusWriter counts status writes performed by the aggregate.
type runtimeStatusWriter struct {
	client.SubResourceWriter
	// writes points to the owning client's counter.
	writes *int
}

// Patch forwards the original status operation and its concurrency guards.
func (w runtimeStatusWriter) Patch(ctx context.Context, obj client.Object, p client.Patch, opts ...client.SubResourcePatchOption) error {
	*w.writes++
	return w.SubResourceWriter.Patch(ctx, obj, p, opts...)
}

// runtimeFixture supplies a persisted root with explicit profile/finalizer and no hidden admission.
func runtimeFixture(t *testing.T, extra ...client.Object) (*api.StacksNetwork, *runtimeAccess) {
	t.Helper()
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "lab", UID: "root", ResourceVersion: "1", Generation: 1, Finalizers: []string{foundationFinalizer}}, Spec: api.StacksNetworkSpec{Operation: "Running", Profile: "regtest-pox4-pox5-v1"}}
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := bitcoin.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	objects := append([]client.Object{root}, extra...)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&api.StacksNetwork{}, &api.StacksNetworkParticipant{}).WithObjects(objects...).Build()
	return root, &runtimeAccess{Client: c}
}

func TestRuntimeRequestsNeverRebuildTopology(t *testing.T) {
	root, c := runtimeFixture(t)
	calls := 0
	r := &Reconciler{Client: c, Reader: c, Runtime: runtimeFunc(func(_ context.Context, current *api.StacksNetwork) (ctrl.Result, error) {
		calls++
		if current.UID != root.UID {
			t.Fatal("runtime did not get current root identity")
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	})}
	request := networkRequest{key: client.ObjectKeyFromObject(root), runtimeOnly: true}
	for i := 0; i < 100; i++ {
		result, err := r.reconcileRequest(context.Background(), request)
		if err != nil || result.RequeueAfter != 5*time.Second {
			t.Fatalf("runtime deadline lost: %+v, %v", result, err)
		}
	}
	if calls != 100 || c.rootReads != 100 || c.graphReads != 0 || c.statusWrites != 0 {
		t.Fatalf("runtime-only work: hooks=%d roots=%d graph=%d writes=%d", calls, c.rootReads, c.graphReads, c.statusWrites)
	}
	// Full requests still enter the real topology resolver and preserve its retry.
	result, err := r.reconcileRequest(context.Background(), networkRequest{key: request.key})
	if err != nil || result.RequeueAfter != 5*time.Second || c.graphReads == 0 || calls != 100 {
		t.Fatalf("full validation bypassed or mixed with projection: %+v, %v, graph=%d hooks=%d", result, err, c.graphReads, calls)
	}
}

func TestRuntimeFailureCannotReplaceTopologyRetry(t *testing.T) {
	root, c := runtimeFixture(t)
	failure := errors.New("observation unavailable")
	r := &Reconciler{Client: c, Reader: c, Runtime: runtimeFunc(func(context.Context, *api.StacksNetwork) (ctrl.Result, error) { return ctrl.Result{}, failure })}
	key := client.ObjectKeyFromObject(root)
	full, err := r.reconcileRequest(context.Background(), networkRequest{key: key})
	if err != nil || full.RequeueAfter != 5*time.Second {
		t.Fatalf("topology retry changed: %+v %v", full, err)
	}
	if _, err := r.reconcileRequest(context.Background(), networkRequest{key: key, runtimeOnly: true}); !errors.Is(err, failure) {
		t.Fatalf("runtime error hidden: %v", err)
	}
	if full.RequeueAfter != 5*time.Second {
		t.Fatal("runtime failure replaced full request deadline")
	}
}

func TestRuntimeExpiryTimerNeedsNoNewObservation(t *testing.T) {
	root, c := runtimeFixture(t)
	observed := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	now := observed.Add(time.Second)
	expires := observed.Add(16 * time.Second)
	r := &Reconciler{Client: c, Reader: c, Runtime: runtimeFunc(func(_ context.Context, root *api.StacksNetwork) (ctrl.Result, error) {
		status, reason := metav1.ConditionFalse, "ObservationStale"
		var remaining time.Duration
		if now.Before(expires) {
			status, reason = metav1.ConditionTrue, "Fresh"
			remaining = expires.Sub(now)
		}
		condition(&root.Status.Conditions, root.Generation, "Operational", status, reason, reason)
		return ctrl.Result{RequeueAfter: remaining}, nil
	})}
	request := networkRequest{key: client.ObjectKeyFromObject(root), runtimeOnly: true}
	first, err := r.reconcileRequest(context.Background(), request)
	if err != nil || first.RequeueAfter != 15*time.Second {
		t.Fatalf("expiry not scheduled: %+v %v", first, err)
	}
	now = expires
	if _, err := r.reconcileRequest(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := c.Client.Get(context.Background(), request.key, root); err != nil {
		t.Fatal(err)
	}
	if !meta.IsStatusConditionFalse(root.Status.Conditions, "Operational") || c.graphReads != 0 || c.statusWrites != 2 {
		t.Fatalf("expiry did not use runtime projection only: %+v graph=%d writes=%d", root.Status, c.graphReads, c.statusWrites)
	}
}

func TestFullAndRuntimeQueueKeysCannotCoalesceAwayValidation(t *testing.T) {
	root := watchRoot(api.Participant{Name: "btc", Kind: "BitcoinNode"})
	mapRoot := func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(root)}}
	}
	notify := handler.TypedEnqueueRequestsFromMapFunc(inputRequests(mapRoot, true))
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[networkRequest]())
	defer queue.ShutDown()
	runtimeKey := networkRequest{key: client.ObjectKeyFromObject(root), runtimeOnly: true}
	queue.Add(runtimeKey)
	processing, shutdown := queue.Get()
	if shutdown || processing != runtimeKey {
		t.Fatal("missing initial runtime request")
	}
	for i := 0; i < 100; i++ {
		notify.Create(context.Background(), event.CreateEvent{Object: root}, queue)
	}
	queue.Done(processing)
	if queue.Len() != 2 {
		t.Fatalf("coalescing lost a queue scope: %d", queue.Len())
	}
	seen := map[bool]bool{}
	for i := 0; i < 2; i++ {
		request, _ := queue.Get()
		seen[request.runtimeOnly] = true
		queue.Done(request)
	}
	if !seen[true] || !seen[false] {
		t.Fatal("runtime event suppressed full validation")
	}
	if got := inputRequests(mapRoot, false)(context.Background(), root); len(got) != 1 || got[0].runtimeOnly {
		t.Fatal("nil-runtime setup changed foundation routing")
	}
}

func TestRuntimeEventRoutingUsesOnlyRuntimeKeys(t *testing.T) {
	root := watchRoot(api.Participant{Name: "btc", Kind: "BitcoinNode"})
	root.Status.Bitcoin = &api.BitcoinRuntimeStatus{ExecutionRefs: []common.Binding{{Name: "execution"}}, InitializationRef: &common.Binding{Name: "initialization"}}
	c := newWatchClient(t, root)
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: ParticipantName(string(root.UID), "btc"), Namespace: "lab"}}
	for _, obj := range []client.Object{p, &bitcoin.BitcoinExecution{ObjectMeta: metav1.ObjectMeta{Name: "execution", Namespace: "lab"}}, &bitcoin.BitcoinInitialization{ObjectMeta: metav1.ObjectMeta{Name: "initialization", Namespace: "lab"}}} {
		before := c.reads
		requests := runtimeRequests(c)(context.Background(), obj)
		if len(requests) != 1 || !requests[0].runtimeOnly || requests[0].key != client.ObjectKeyFromObject(root) || c.reads-before != 1 {
			t.Fatalf("runtime input %T used wrong scope/cost: %+v reads=%d", obj, requests, c.reads-before)
		}
	}
	updated := p.DeepCopy()
	updated.Status.Runtime = &api.ParticipantRuntimeStatus{Terminated: true}
	updated.Status.Conditions = []metav1.Condition{{Type: "WorkloadReady", Status: metav1.ConditionFalse}}
	change := event.UpdateEvent{ObjectOld: p, ObjectNew: updated}
	if admissionEvents().Update(change) || !runtimeEvents().Update(change) {
		t.Fatal("runtime transition entered admission routing or was suppressed")
	}
	old := &bitcoin.BitcoinExecution{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1"}, Status: bitcoin.BitcoinExecutionStatus{Observation: &bitcoin.BitcoinObservation{ObservedAt: metav1.NewTime(time.Now())}}}
	current := old.DeepCopy()
	current.ResourceVersion = "2"
	if runtimeEvents().Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}) {
		t.Fatal("unchanged heartbeat enqueued projection")
	}
	current.Status.Observation.ObservedAt = metav1.NewTime(old.Status.Observation.ObservedAt.Add(time.Second))
	if runtimeEvents().Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}) {
		t.Fatal("timestamp-only heartbeat bypassed the expiry timer")
	}
	rootAfter := root.DeepCopy()
	rootAfter.Status.Phase = "Running"
	if admissionEvents().Update(event.UpdateEvent{ObjectOld: root, ObjectNew: rootAfter}) {
		t.Fatal("runtime phase change rebuilt topology")
	}
	condition(&rootAfter.Status.Conditions, rootAfter.Generation, "Failed", metav1.ConditionTrue, "WorkerLost", "Worker lost")
	if !admissionEvents().Update(event.UpdateEvent{ObjectOld: root, ObjectNew: rootAfter}) {
		t.Fatal("failure latch did not reach admission routing")
	}
}

func TestTopologyReportPreservesRuntimeStatus(t *testing.T) {
	root, c := runtimeFixture(t)
	root.Status.Phase = "Running"
	root.Status.GenesisRef = &common.Binding{Name: "genesis", UID: "genesis"}
	for _, typ := range []string{"Initialized", "Running", "Operational"} {
		condition(&root.Status.Conditions, root.Generation, typ, metav1.ConditionTrue, "Observed", "Observed")
	}
	if err := c.Client.Status().Update(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	before := root.DeepCopy()
	r := &Reconciler{Client: c, Reader: c, Runtime: runtimeFunc(func(context.Context, *api.StacksNetwork) (ctrl.Result, error) { return ctrl.Result{}, nil })}
	if _, err := r.report(context.Background(), root, before, "Initializing", "GenesisCaptured", "Captured"); err != nil {
		t.Fatal(err)
	}
	if root.Status.Phase != "Running" {
		t.Fatal("topology report reset runtime phase")
	}
	for _, typ := range []string{"Initialized", "Running", "Operational"} {
		if !reflect.DeepEqual(meta.FindStatusCondition(root.Status.Conditions, typ), meta.FindStatusCondition(before.Status.Conditions, typ)) {
			t.Fatalf("topology overwrote %s", typ)
		}
	}
}

func TestFailedRootStillWithdrawsAndDeletesParticipants(t *testing.T) {
	for _, phase := range []string{"Failed", "Stopped"} {
		t.Run(phase, func(t *testing.T) {
			root, c := runtimeFixture(t)
			root.Status.Phase = phase
			root.Status.GenesisRef = &common.Binding{Name: "missing-genesis", UID: "genesis"}
			root.Status.Identities = []api.InstanceIdentity{{Name: "removed", UID: "participant"}}
			condition(&root.Status.Conditions, root.Generation, "Failed", metav1.ConditionTrue, "WorkerLost", "Worker lost")
			if err := c.Client.Status().Update(context.Background(), root); err != nil {
				t.Fatal(err)
			}
			p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: ParticipantName(string(root.UID), "removed"), Namespace: root.Namespace, UID: "participant", OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetwork", Name: root.Name, UID: root.UID, Controller: ptr.To(true)}}}}
			if err := c.Client.Create(context.Background(), p); err != nil {
				t.Fatal(err)
			}
			r := &Reconciler{Client: c, Reader: c}
			request := networkRequest{key: client.ObjectKeyFromObject(root)}
			if result, err := r.reconcileRequest(context.Background(), request); err != nil || result.Requeue || result.RequeueAfter != time.Millisecond {
				t.Fatalf("withdrawal not recorded first: %+v %v", result, err)
			}
			if err := c.Client.Get(context.Background(), client.ObjectKeyFromObject(p), p); err != nil {
				t.Fatal("participant deleted before withdrawal was recorded")
			}
			if err := c.Client.Get(context.Background(), request.key, root); err != nil {
				t.Fatal(err)
			}
			if !root.Status.Identities[0].Removing || !meta.IsStatusConditionTrue(root.Status.Conditions, "Failed") || root.Status.Phase != phase {
				t.Fatal("withdrawal erased failure latch/phase")
			}
			if _, err := r.reconcileRequest(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if err := c.Client.Get(context.Background(), client.ObjectKeyFromObject(p), p); !apierrors.IsNotFound(err) {
				t.Fatalf("failed root did not delete omitted participant: %v", err)
			}
			if c.graphReads != 1 {
				t.Fatalf("failure cleanup read frozen inputs or allocated topology: graph reads=%d", c.graphReads)
			}
		})
	}
}

func TestRuntimeCannotClearFailedFact(t *testing.T) {
	root, c := runtimeFixture(t)
	root.Status.Phase = "Failed"
	condition(&root.Status.Conditions, root.Generation, "Failed", metav1.ConditionTrue, "WorkerLost", "Worker lost")
	if err := c.Client.Status().Update(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	r := &Reconciler{Client: c, Reader: c, Runtime: runtimeFunc(func(_ context.Context, root *api.StacksNetwork) (ctrl.Result, error) {
		root.Status.Phase = "Stopped"
		root.Status.Conditions = nil
		return ctrl.Result{}, nil
	})}
	if _, err := r.reconcileRequest(context.Background(), networkRequest{key: client.ObjectKeyFromObject(root), runtimeOnly: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Client.Get(context.Background(), client.ObjectKeyFromObject(root), root); err != nil {
		t.Fatal(err)
	}
	if root.Status.Phase != "Stopped" || !meta.IsStatusConditionTrue(root.Status.Conditions, "Failed") {
		t.Fatal("runtime disposal cleared failure evidence")
	}
}

func TestWorkerAndControlObservationsDoNotRebuildAdmission(t *testing.T) {
	before := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "participant", Namespace: "test", UID: "participant", Generation: 1}}
	for _, kind := range []string{"stacks", "bitcoin"} {
		t.Run(kind, func(t *testing.T) {
			after := before.DeepCopy()
			if kind == "stacks" {
				after.Status.Execution = &api.WorkerExecutionStatus{PodUID: "worker", ProcessNonce: "process", Phase: "Paused", ObservedAt: metav1.Now()}
			} else {
				after.Status.BitcoinControl = &api.BitcoinControlRuntimeStatus{ObservedGeneration: 1, Terminated: true}
			}
			update := event.UpdateEvent{ObjectOld: before, ObjectNew: after}
			if !runtimeEvents().Update(update) {
				t.Fatal("worker observation did not wake lifecycle projection")
			}
			if admissionEvents().Update(update) {
				t.Fatal("worker observation rebuilt admission")
			}
		})
	}
}

func TestRuntimeNotificationsCoalesceStatusAndDoNotDelayControl(t *testing.T) {
	root := watchRoot(api.Participant{Name: "btc", Kind: "BitcoinNode"})
	c := newWatchClient(t, root)
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: ParticipantName(string(root.UID), "btc"), Namespace: "lab", Generation: 1}}
	current := p.DeepCopy()
	current.Status.Runtime = &api.ParticipantRuntimeStatus{Terminated: true}
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[networkRequest]())
	defer queue.ShutDown()
	h := runtimeNotifications(c)
	for range 100 {
		h.Update(context.Background(), event.UpdateEvent{ObjectOld: p, ObjectNew: current}, queue)
	}
	if queue.Len() != 0 {
		t.Fatal("status notification bypassed coalescing")
	}
	deadline := time.Now().Add(4 * time.Second)
	for queue.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if queue.Len() != 1 {
		t.Fatalf("coalesced queue length=%d", queue.Len())
	}
	request, _ := queue.Get()
	queue.Done(request)
	if !request.runtimeOnly {
		t.Fatal("projection rebuilt admission")
	}
	current.Generation++
	h.Update(context.Background(), event.UpdateEvent{ObjectOld: p, ObjectNew: current}, queue)
	if queue.Len() != 1 {
		t.Fatal("control change delayed")
	}
}
