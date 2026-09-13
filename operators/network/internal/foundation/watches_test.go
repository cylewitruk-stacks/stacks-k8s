package foundation

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// watchClient counts event-path IO and rejects unindexed scans or private input access.
type watchClient struct {
	client.Client
	// reads counts attempted Get/List operations.
	reads int
	// violations records any broad scan or private read, even when routing recovers.
	violations []string
}

// Get records any unexpected event-path direct lookup.
func (c *watchClient) Get(
	_ context.Context,
	_ client.ObjectKey,
	obj client.Object,
	_ ...client.GetOption,
) error {
	c.reads++
	err := fmt.Errorf("watch routing must use indexed public lists, got Get %T", obj)
	c.violations = append(c.violations, err.Error())
	return err
}

// List permits only namespaced public-resource index queries.
func (c *watchClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c.reads++
	options := &client.ListOptions{}
	for _, option := range opts {
		option.ApplyToList(options)
	}
	if options.Namespace == "" || options.FieldSelector == nil || options.FieldSelector.Empty() {
		err := fmt.Errorf("unindexed or unscoped watch scan: %T", list)
		c.violations = append(c.violations, err.Error())
		return err
	}
	if _, ok := list.(*corev1.SecretList); ok {
		c.violations = append(c.violations, "Secret payload read")
		return fmt.Errorf("Secret payload read")
	}
	return c.Client.List(ctx, list, opts...)
}

// newWatchClient supplies the same indexes as manager registration to routing tests.
func newWatchClient(t *testing.T, objects ...client.Object) *watchClient {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		api.AddToScheme,
		stacks.AddToScheme,
		bitcoin.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	b := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...)
	for _, input := range append([]client.Object{
		&api.StacksNetwork{},
		&api.StacksNetworkParticipant{},
	}, publicInputs()...) {
		b = b.WithIndex(input, inputDependencyIndex, inputReferences)
	}
	c := &watchClient{Client: b.Build()}
	t.Cleanup(func() {
		if len(c.violations) != 0 {
			t.Errorf("watch access violations: %v", c.violations)
		}
	})
	return c
}

// watchRoot selects a named participant while leaving its inputs unresolved.
func watchRoot(entry api.Participant) *api.StacksNetwork {
	return &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "lab", UID: "root"},
		Spec:       api.StacksNetworkSpec{Participants: []api.Participant{entry}},
	}
}

// assertWatchRequests checks exact routing and excludes accidental namespace fan-out.
func assertWatchRequests(t *testing.T, got []reconcile.Request, names ...string) {
	t.Helper()
	var actual []string
	for _, request := range got {
		actual = append(actual, request.Namespace+"/"+request.Name)
	}
	slices.Sort(actual)
	slices.Sort(names)
	if !slices.Equal(actual, names) {
		t.Fatalf("requests = %v, want %v", actual, names)
	}
}

func TestAdmissionEventsIgnoreRuntimeTraffic(t *testing.T) {
	root := watchRoot(api.Participant{Name: "btc", Kind: "BitcoinNode"})
	p := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{
			Name:       ParticipantName(string(root.UID), "btc"),
			Namespace:  root.Namespace,
			UID:        "participant",
			Generation: 1,
		},
		Spec: api.StacksNetworkParticipantSpec{Kind: "BitcoinNode"},
		Status: api.ParticipantStatus{
			Conditions: []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}},
		},
	}
	c := newWatchClient(t, root, p)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()
	notify := handler.EnqueueRequestsFromMapFunc(enqueueInputRoots(c))
	filter := admissionEvents()
	for i := 0; i < 100; i++ {
		updated := p.DeepCopy()
		updated.ResourceVersion = fmt.Sprint(i + 2)
		updated.Status.Runtime = &api.ParticipantRuntimeStatus{ObservedGeneration: 1, ImageID: fmt.Sprint(i)}
		updated.Status.Conditions[0].LastTransitionTime = metav1.NewTime(time.Now())
		updated.Status.Conditions = append(
			updated.Status.Conditions,
			metav1.Condition{Type: "WorkloadReady", Status: metav1.ConditionFalse, Reason: fmt.Sprint(i)},
		)
		e := event.UpdateEvent{ObjectOld: p, ObjectNew: updated}
		if filter.Update(e) {
			notify.Update(context.Background(), e, queue)
		}
	}
	if queue.Len() != 0 || c.reads != 0 {
		t.Fatalf("runtime traffic caused %d queued roots and %d reads", queue.Len(), c.reads)
	}
	updated := p.DeepCopy()
	updated.Status.Admission = &api.Admission{PolicyDigest: "new"}
	e := event.UpdateEvent{ObjectOld: p, ObjectNew: updated}
	if !filter.Update(e) {
		t.Fatal("lost admission change")
	}
	notify.Update(context.Background(), e, queue)
	if queue.Len() != 1 || c.reads != 4 {
		t.Fatalf("admission update caused %d queued roots and %d reads, want 1 and 4", queue.Len(), c.reads)
	}
}

func TestAdmissionEventsPreserveIdentityAndValidationTransitions(t *testing.T) {
	p := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{UID: "old", Generation: 1},
		Status: api.ParticipantStatus{
			Conditions: []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue, Reason: "Admitted"}},
		},
	}
	for name, change := range map[string]func(*api.StacksNetworkParticipant){
		"uid":        func(p *api.StacksNetworkParticipant) { p.UID = "replacement" },
		"generation": func(p *api.StacksNetworkParticipant) { p.Generation++ },
		"deletion":   func(p *api.StacksNetworkParticipant) { p.DeletionTimestamp = ptr.To(metav1.Now()) },
		"owner": func(p *api.StacksNetworkParticipant) {
			p.OwnerReferences = []metav1.OwnerReference{{UID: "foreign"}}
		},
		"finalizer":  func(p *api.StacksNetworkParticipant) { p.Finalizers = []string{"cleanup"} },
		"spec drift": func(p *api.StacksNetworkParticipant) { p.Spec.Source.UID = "other" },
		"resolution withdrawn": func(p *api.StacksNetworkParticipant) {
			p.Status.Conditions[0].Status = metav1.ConditionFalse
		},
		"resolution removed": func(p *api.StacksNetworkParticipant) { p.Status.Conditions = nil },
		"policy deferred": func(p *api.StacksNetworkParticipant) {
			p.Status.Conditions = append(p.Status.Conditions, metav1.Condition{
				Type:   "PolicyDeferred",
				Status: metav1.ConditionTrue,
			})
		},
		"config report": func(p *api.StacksNetworkParticipant) {
			p.Status.Conditions = append(p.Status.Conditions, metav1.Condition{
				Type:   "ConfigVerified",
				Status: metav1.ConditionFalse,
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			next := p.DeepCopy()
			change(next)
			if !admissionEvents().Update(event.UpdateEvent{ObjectOld: p, ObjectNew: next}) {
				t.Fatal("suppressed required transition")
			}
		})
	}
	if !admissionEvents().Create(event.CreateEvent{Object: p}) ||
		!admissionEvents().Delete(event.DeleteEvent{Object: p}) ||
		!admissionEvents().Generic(event.GenericEvent{Object: p}) {
		t.Fatal("suppressed create/delete/relist repair")
	}
}

func TestInputRoutingPreservesUnresolvedAndTransitiveDependencies(t *testing.T) {
	root := watchRoot(
		api.Participant{
			Name:       "btc",
			Kind:       "BitcoinNode",
			Definition: api.Definition{Ref: &common.NameRef{Name: "core"}},
		},
	)
	node := &bitcoin.BitcoinNode{
		ObjectMeta: metav1.ObjectMeta{Name: "core", Namespace: "lab"},
		Spec:       bitcoin.BitcoinNodeSpec{WalletRefs: ptr.To([]common.NameRef{{Name: "miner-wallet"}})},
	}
	wallet := &bitcoin.BitcoinWallet{
		ObjectMeta: metav1.ObjectMeta{Name: "miner-wallet", Namespace: "lab"},
		Spec: bitcoin.BitcoinWalletSpec{
			KeySource: &bitcoin.WalletKeySource{StacksMinerAccountRef: &common.NameRef{Name: "miner"}},
		},
	}
	account := &stacks.StacksAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "miner", Namespace: "lab"},
		Spec: stacks.StacksAccountSpec{
			Key: &common.KeySource{SecretRef: &common.SecretKeyRef{Name: "private", Key: "key"}},
		},
	}
	c := newWatchClient(t, root, node, wallet, account)
	secret := &metav1.PartialObjectMetadata{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: "private", Namespace: "lab"},
	}
	for _, input := range []client.Object{node, wallet, account, secret} {
		t.Run(fmt.Sprintf("%T", input), func(t *testing.T) {
			assertWatchRequests(t, enqueueInputRoots(c)(context.Background(), input), "lab/network")
		})
	}
	unrelated := node.DeepCopy()
	unrelated.Name = "parked"
	c.reads = 0
	assertWatchRequests(t, enqueueInputRoots(c)(context.Background(), unrelated))
	if c.reads != 2 {
		t.Fatalf("unrelated definition performed %d reads, want two indexed lookups", c.reads)
	}
	secret.Namespace = "other"
	assertWatchRequests(t, enqueueInputRoots(c)(context.Background(), secret))
	// Watch rebuilds after restart use only durable declarations, never process-local edges.
	restarted := newWatchClient(t, root, node, wallet, account)
	secret.Namespace = "lab"
	assertWatchRequests(t, enqueueInputRoots(restarted)(context.Background(), secret), "lab/network")
}

func TestInputRoutingBeforeParticipantAdmission(t *testing.T) {
	root := watchRoot(
		api.Participant{
			Name: "signer",
			Kind: "StacksSigner",
			Definition: api.Definition{
				Inline: &api.Configuration{
					StacksSigner: &stacks.StacksSignerSpec{AccountRef: &common.NameRef{Name: "missing"}},
				},
			},
			Overrides: &api.Configuration{
				StacksSigner: &stacks.StacksSignerSpec{
					ActorFields: common.ActorFields{
						Config: &common.Config{SecretRef: &common.SecretKeyRef{Name: "config", Key: "config.toml"}},
					},
				},
			},
		},
	)
	root.Spec.Genesis = &api.GenesisInput{
		Allocations: []api.GenesisAllocation{{AccountRef: common.NameRef{Name: "funded"}}},
	}
	root.Spec.EpochScheduleRef = &common.NameRef{Name: "epochs"}
	c := newWatchClient(t, root)
	for _, input := range []client.Object{
		&stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "missing", Namespace: "lab"}},
		&stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "funded", Namespace: "lab"}},
		&api.StacksEpochSchedule{ObjectMeta: metav1.ObjectMeta{Name: "epochs", Namespace: "lab"}},
		&metav1.PartialObjectMetadata{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "lab"},
		},
	} {
		assertWatchRequests(t, enqueueInputRoots(c)(context.Background(), input), "lab/network")
	}
}

func TestInputRoutingRetainsOldPolicyDependencies(t *testing.T) {
	root := watchRoot(api.Participant{Name: "signer", Kind: "StacksSigner"})
	p := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{Name: ParticipantName(string(root.UID), "signer"), Namespace: "lab"},
		Spec: api.StacksNetworkParticipantSpec{
			Kind:   "StacksSigner",
			Source: api.Source{Name: "old-source"},
			Configuration: api.Configuration{
				StacksSigner: &stacks.StacksSignerSpec{AccountRef: &common.NameRef{Name: "candidate"}},
			},
		},
		Status: api.ParticipantStatus{
			Admission: &api.Admission{Dependencies: []common.Binding{{Kind: "Secret", Name: "retained-key"}}},
		},
	}
	c := newWatchClient(t, root, p)
	for _, input := range []client.Object{
		&stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "candidate", Namespace: "lab"}},
		&stacks.StacksSigner{ObjectMeta: metav1.ObjectMeta{Name: "old-source", Namespace: "lab"}},
		&metav1.PartialObjectMetadata{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: "retained-key", Namespace: "lab"},
		},
	} {
		assertWatchRequests(t, enqueueInputRoots(c)(context.Background(), input), "lab/network")
	}
}

func TestIdentityInputRoutingIsTargeted(t *testing.T) {
	account := &stacks.StacksAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "account", Namespace: "lab", UID: "account-uid"},
		Spec:       stacks.StacksAccountSpec{Key: &common.KeySource{SecretRef: &common.SecretKeyRef{Name: "key"}}},
	}
	other := account.DeepCopy()
	other.Name = "unrelated"
	other.Spec.Key.SecretRef.Name = "other"
	wallet := &bitcoin.BitcoinWallet{
		ObjectMeta: metav1.ObjectMeta{Name: "derived", Namespace: "lab"},
		Spec: bitcoin.BitcoinWalletSpec{
			KeySource: &bitcoin.WalletKeySource{StacksMinerAccountRef: &common.NameRef{Name: "account"}},
		},
	}
	c := newWatchClient(t, account, other, wallet)
	secret := &metav1.PartialObjectMetadata{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: "key", Namespace: "lab"},
	}
	assertWatchRequests(
		t,
		enqueueIdentityInputs(c, &stacks.StacksAccount{})(context.Background(), secret),
		"lab/account",
	)
	assertWatchRequests(
		t,
		enqueueIdentityInputs(c, &bitcoin.BitcoinWallet{})(context.Background(), account),
		"lab/derived",
	)
	if c.reads != 2 {
		t.Fatalf("identity notifications used %d reads, want 2", c.reads)
	}
	secret.Name = "unrelated-secret"
	assertWatchRequests(t, enqueueIdentityInputs(c, &stacks.StacksAccount{})(context.Background(), secret))
}

func TestInputIndexIncludesGeneratedDefaultsAndRetainedCredentials(t *testing.T) {
	faucet := &stacks.StacksFaucet{ObjectMeta: metav1.ObjectMeta{Name: "faucet", Namespace: "lab"}}
	if !slices.Contains(inputReferences(faucet), "StacksAccount/faucet-account") {
		t.Fatal("missing default account before admission")
	}
	account := &stacks.StacksAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "account", Namespace: "lab", UID: "a"},
		Status:     common.ResolutionStatus{CredentialsRef: &common.SecretKeyRef{Name: "retained"}},
	}
	keys := inputReferences(account)
	if !slices.Contains(keys, "Secret/retained") ||
		!slices.Contains(keys, "Secret/"+RuntimeName("", "a", "StacksAccount", "account", "key")) {
		t.Fatal("missing generated or retained credential notification")
	}
	if faucet.Spec.AccountRef != nil {
		t.Fatal("index extraction mutated declaration")
	}
}

func TestGateCompletionRoutesAdmissionWithoutObservationChurn(t *testing.T) {
	root := watchRoot(api.Participant{Name: "worker", Kind: "StacksStacker"})
	root.Status.Initialization = &api.InitializationStatus{
		GenesisUID:    "genesis",
		GenesisDigest: "digest",
		Gates:         []api.GateObservation{{Name: "PrepareBitcoin"}},
	}
	timing := root.DeepCopy()
	now := metav1.Now()
	timing.Status.Initialization.Gates[0].FirstCeilingObservedAt = &now
	if admissionEvents().Update(event.UpdateEvent{ObjectOld: root, ObjectNew: timing}) {
		t.Fatal("timing observation routed admission")
	}
	completed := timing.DeepCopy()
	completed.Status.Initialization.Gates[0].CompletedAt = &now
	completed.Status.Initialization.GateIndex = 1
	completed.Status.Initialization.Completed = true
	if !admissionEvents().Update(event.UpdateEvent{ObjectOld: timing, ObjectNew: completed}) {
		t.Fatal("gate completion did not release admission work")
	}
}
