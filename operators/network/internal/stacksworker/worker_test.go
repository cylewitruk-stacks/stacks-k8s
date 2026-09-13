package stacksworker

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fixture supplies one admitted inactive transaction-production candidate.
func fixture(t *testing.T) (*api.StacksNetwork, *api.StacksNetworkParticipant, *corev1.Pod, Profile) {
	t.Helper()
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "network-uid", Generation: 1},
		Spec: api.StacksNetworkSpec{
			Operation:    "Running",
			Participants: []api.Participant{{Name: "producer", Kind: "StacksTransactionProduction"}},
		},
		Status: api.StacksNetworkStatus{
			Identities: []api.InstanceIdentity{{Name: "producer", UID: "participant-uid"}},
			GenesisRef: &common.Binding{Kind: "StacksGenesis", Name: "genesis", UID: "genesis-uid"},
		},
	}
	configuration := api.Configuration{StacksTransactionProduction: &stacks.StacksTransactionProductionSpec{}}
	p := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{
			Name:       foundation.ParticipantName(string(root.UID), "producer"),
			Namespace:  root.Namespace,
			UID:        "participant-uid",
			Generation: 1,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: api.GroupVersion.String(),
					Kind:       "StacksNetwork",
					Name:       root.Name,
					UID:        root.UID,
					Controller: ptr.To(true),
				},
			},
		},
		Spec: api.StacksNetworkParticipantSpec{
			NetworkUID:      root.UID,
			ParticipantName: "producer",
			Kind:            "StacksTransactionProduction",
			Configuration:   configuration,
		},
		Status: api.ParticipantStatus{
			Admission: &api.Admission{Configuration: configuration, PolicyDigest: foundation.Digest(configuration)},
		},
	}
	p.Status.Conditions = []metav1.Condition{
		{
			Type:               "AdmissionReady",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: p.Generation,
			Reason:             "RetainedPolicyEligible",
			Message:            "eligible",
			LastTransitionTime: metav1.Now(),
		},
	}
	profile := Profile{
		Image:         "worker:test",
		Configuration: common.Binding{Kind: "ConfigMap", Name: "bootstrap", UID: "config-uid"},
		Keys: []KeyMount{
			{
				Role:   "sender",
				Secret: common.Binding{Kind: "Secret", Name: "sender-key", UID: "key-uid"},
				Key:    "privateKey",
			},
		},
		Reads: []ReadBinding{
			{APIVersion: "stacks.stacks.org/v1alpha2", Resource: "stacksaccounts", Name: "sender"},
		},
	}
	pod, err := Pod(p, profile)
	if err != nil {
		t.Fatal(err)
	}
	pod.UID = "pod-uid"
	pod.Spec.NodeName = "node"
	pod.Status.Phase = corev1.PodRunning
	p.Status.Runtime = &api.ParticipantRuntimeStatus{
		WorkerCandidate: &api.WorkerCandidate{Pod: podBinding(pod), ProfileDigest: profile.Digest()},
	}
	return root, p, pod, profile
}

// fakeClient registers only replacement and native Kubernetes APIs.
func fakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		api.AddToScheme,
		stacks.AddToScheme,
		corev1.AddToScheme,
		rbacv1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&api.StacksNetwork{}, &api.StacksNetworkParticipant{}, &corev1.Pod{}).
		WithObjects(objects...).
		Build()
}

func TestExactBindingSurvivesLostWriteAndClassifiesBoundLoss(t *testing.T) {
	root, p, pod, _ := fixture(t)
	now := time.Now()
	first := ProjectSession(root, p, pod, nil, now)
	if !first.Changed || first.Failed || root.Status.Identities[0].Worker.Pod.UID != pod.UID {
		t.Fatalf("binding: %+v", first)
	}
	bound := root.DeepCopy()
	again := ProjectSession(root, p, pod, nil, now)
	if again.Changed || !reflect.DeepEqual(root, bound) {
		t.Fatal("lost acknowledgement/operator restart changed exact binding")
	}
	for _, test := range []struct {
		name            string
		pod             *corev1.Pod
		err             error
		failed, unknown bool
	}{
		{"read error", nil, fmt.Errorf("timeout"), false, true},
		{"missing", nil, apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, pod.Name), true, true},
		{"replacement", func() *corev1.Pod { x := pod.DeepCopy(); x.UID = "replacement"; return x }(), nil, true, true},
		{"terminal", func() *corev1.Pod {
			x := pod.DeepCopy()
			x.Status.Phase = corev1.PodFailed
			return x
		}(), nil, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := bound.DeepCopy()
			fact := ProjectSession(snapshot, p, test.pod, test.err, now)
			if fact.Failed != test.failed || fact.Unknown != test.unknown ||
				snapshot.Status.Identities[0].Worker.Pod.UID != pod.UID {
				t.Fatalf("unexpected session fact: %+v", fact)
			}
		})
	}
}

func TestOrderedDisposalRetainsUnsettledAndExactTermination(t *testing.T) {
	for _, settled := range []bool{false, true} {
		t.Run(fmt.Sprint(settled), func(t *testing.T) {
			root, p, pod, profile := fixture(t)
			now := time.Now()
			ProjectSession(root, p, pod, nil, now)
			root.Spec.Operation = "Stopped"
			root.Generation++
			fact := ProjectSession(root, p, pod, nil, now)
			session := root.Status.Identities[0].Worker
			if !fact.Changed || session.Shutdown == nil || session.Disposal != nil {
				t.Fatal("shutdown must be recorded before acknowledgement")
			}
			phase, pending := api.WorkerPhaseUnsettled, int32(1)
			if settled {
				phase, pending = "Settled", 0
			}
			p.Status.Execution = &api.WorkerExecutionStatus{
				PodUID:            pod.UID,
				ProcessNonce:      "process",
				ProfileDigest:     profile.Digest(),
				NetworkGeneration: root.Generation,
				Reason:            "NetworkStopped",
				Phase:             phase,
				Pending:           pending,
				ObservedAt:        metav1.NewTime(now),
			}
			fact = ProjectSession(root, p, pod, nil, now)
			if !fact.Changed || fact.Failed == settled || session.Disposal == nil || session.Disposal.Terminated {
				t.Fatalf("disposition: %+v", fact)
			}
			pod.Status.Phase = corev1.PodSucceeded
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{
					Name:  "worker",
					State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
				},
			}
			fact = ProjectSession(root, p, pod, nil, now)
			if !fact.Changed || !session.Disposal.Terminated {
				t.Fatal("exact process exit not retained")
			}
			missing := apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, pod.Name)
			fact = ProjectSession(root, p, nil, missing, now)
			if fact.Unknown || fact.Reason != "WorkerDisposed" {
				t.Fatal("acknowledged disposal was mistaken for unexpected worker loss")
			}
		})
	}
	root, p, pod, _ := fixture(t)
	now := time.Now()
	ProjectSession(root, p, pod, nil, now)
	root.Spec.Operation = "Stopped"
	ProjectSession(root, p, pod, nil, now)
	fact := ProjectSession(root, p, pod, nil, now.Add(ShutdownBound))
	if !fact.Failed || !fact.Unknown || root.Status.Identities[0].Worker.Disposal.Outcome != "Unsettled" {
		t.Fatal("bounded settlement timeout invented a clean outcome")
	}
}

func TestWorkerRBACAndStaticProfileDoNotGrantSecretOrTopologyAPI(t *testing.T) {
	_, p, pod, profile := fixture(t)
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever || len(pod.Spec.Containers) != 1 ||
		len(pod.Spec.InitContainers) != 0 ||
		pod.Spec.Containers[0].VolumeMounts[1].MountPath != "/keys/sender" ||
		pod.Spec.Volumes[1].Secret.Items[0].Key != "privateKey" {
		t.Fatal("standalone role key boundary missing")
	}
	for _, rule := range Rules(p, profile) {
		if len(rule.ResourceNames) != 1 || rule.ResourceNames[0] == "" || rule.ResourceNames[0] == "*" {
			t.Fatal("unnamed API permission")
		}
		for _, resource := range rule.Resources {
			if resource == "secrets" || resource == "deployments" || resource == "pods/exec" {
				t.Fatal("worker has private/topology API access")
			}
			for _, verb := range rule.Verbs {
				if verb != "get" && verb != "list" && verb != "watch" &&
					(verb != "patch" ||
						resource != "stacksnetworkparticipants/status" ||
						rule.ResourceNames[0] != p.Name) {
					t.Fatal("unexpected worker mutation permission")
				}
			}
		}
	}
	before := profile.Digest()
	p.Generation++
	p.Status.Admission.PolicyDigest = "new-policy"
	p.Spec.Control = &api.Control{Paused: ptr.To(true)}
	next, err := Pod(p, profile)
	if err != nil || profile.Digest() != before || next.Annotations[profileLabel] != pod.Annotations[profileLabel] {
		t.Fatal("mutable execution policy rolled the fixed worker profile")
	}
	for _, version := range []string{"v1", "*/v1", "*/v1alpha2", "network.stacks.org/v1alpha2"} {
		attack := profile
		attack.Reads = []ReadBinding{{APIVersion: version, Resource: "secrets", Name: "foreign"}}
		if _, err := attack.Normalize(); err == nil {
			t.Fatalf("private resource read accepted through %s", version)
		}
	}
	profile.Reads = append(profile.Reads, ReadBinding{APIVersion: "v1", Resource: "secrets", Name: "foreign"})
	if _, err := profile.Normalize(); err == nil {
		t.Fatal("public read extension granted Secret access")
	}
}

// statusClient checks minimal execution SSA and optionally loses an acknowledgement once.
type statusClient struct {
	client.Client
	lose    bool
	commit  bool
	patches int
}

// statusWriter adapts fake status writes while retaining inspection of the real SSA request.
type statusWriter struct {
	client.SubResourceWriter
	parent *statusClient
}

func (c *statusClient) Status() client.SubResourceWriter {
	return &statusWriter{SubResourceWriter: c.Client.Status(), parent: c}
}

func (w *statusWriter) Patch(
	ctx context.Context,
	object client.Object,
	patch client.Patch,
	options ...client.SubResourcePatchOption,
) error {
	p, ok := object.(*api.StacksNetworkParticipant)
	if !ok || p.Status.Execution == nil || p.Status.Runtime != nil || p.Status.Admission != nil ||
		p.Status.BitcoinControl != nil ||
		p.Status.Scheduling != nil ||
		len(p.Status.Conditions) > 0 {
		return fmt.Errorf("worker attempted to write another status owner")
	}
	opts := &client.SubResourcePatchOptions{}
	for _, option := range options {
		option.ApplyToSubResourcePatch(opts)
	}
	if opts.FieldManager != "stacks-network-worker-execution" || opts.Force == nil || !*opts.Force {
		return fmt.Errorf("worker execution field manager changed")
	}
	if patch.Type() != types.ApplyPatchType {
		return fmt.Errorf("worker did not use minimal SSA")
	}
	w.parent.patches++
	lose := w.parent.lose
	w.parent.lose = false
	if !lose || w.parent.commit {
		var current api.StacksNetworkParticipant
		if err := w.parent.Get(ctx, client.ObjectKeyFromObject(p), &current); err != nil {
			return err
		}
		if current.UID != p.UID {
			return fmt.Errorf("worker participant UID changed")
		}
		current.Status.Execution = p.Status.Execution
		if err := w.parent.Client.Status().Update(ctx, &current); err != nil {
			return err
		}
		*p = current
	}
	if lose {
		return fmt.Errorf("execution status acknowledgement lost")
	}
	return nil
}

// testRole retains process-local calls across duplicate reconciliations.
type testRole struct {
	steps    int
	drains   int
	snapshot Snapshot
}

func (r *testRole) Step(_ context.Context, s Snapshot) (RoleResult, error) {
	r.steps++
	r.snapshot = s
	return RoleResult{
		AppliedPolicyDigest: s.Participant.Status.Admission.PolicyDigest,
		// #nosec G115 -- Small deterministic fixture counters/values are bounded by the test setup.
		Pending:      int32(r.steps),
		Reason:       "Observed",
		RequeueAfter: time.Second,
	}, nil
}

func (r *testRole) Drain(context.Context, Snapshot) (DrainResult, error) {
	r.drains++
	return DrainResult{Done: true, Settled: true}, nil
}

func TestExecutionPublicationGateAndSnapshotBoundAuthorization(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprint(commit), func(t *testing.T) {
			ctx := context.Background()
			root, p, pod, profile := fixture(t)
			ProjectSession(root, p, pod, nil, time.Now())
			c := &statusClient{Client: fakeClient(t, root, p, pod)}
			role := &testRole{}
			r := Runtime{
				Client:          c,
				Namespace:       p.Namespace,
				ParticipantName: p.Name,
				NetworkUID:      root.UID,
				ParticipantUID:  p.UID,
				PodUID:          pod.UID,
				PodName:         pod.Name,
				Profile:         profile,
				Role:            role,
				Prerequisites:   func(context.Context, Snapshot) error { return nil },
				nonce:           "process",
			}
			if _, err := r.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			if role.steps != 1 {
				t.Fatal("bound role did not run")
			}
			if err := role.snapshot.Authorize(ctx); err != nil {
				t.Fatal(err)
			}
			c.lose, c.commit = true, commit
			if _, err := r.Reconcile(ctx); err == nil {
				t.Fatal("lost publication acknowledgement not surfaced")
			}
			if role.steps != 2 || r.pendingReport == nil {
				t.Fatal("role result was not retained")
			}
			at := r.pendingReport.ObservedAt
			if _, err := r.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			if role.steps != 2 || r.pendingReport != nil {
				t.Fatal("new role work ran before publication recovery")
			}
			var current api.StacksNetworkParticipant
			if err := c.Get(ctx, client.ObjectKeyFromObject(p), &current); err != nil {
				t.Fatal(err)
			}
			if !current.Status.Execution.ObservedAt.Equal(&at) {
				t.Fatal("lost acknowledgement retry regenerated observation time")
			}
			base := root.DeepCopy()
			root.Spec.Operation = "Paused"
			root.Generation++
			if err := c.Patch(ctx, root, client.MergeFrom(base)); err != nil {
				t.Fatal(err)
			}
			if err := role.snapshot.Authorize(ctx); err == nil {
				t.Fatal("prepared invocation survived changed root control")
			}
			baseP := current.DeepCopy()
			current.Status.Admission.Configuration.StacksTransactionProduction.Interval = ptr.To(common.Duration("7s"))
			current.Status.Admission.PolicyDigest = foundation.Digest(current.Status.Admission.Configuration)
			if err := c.Client.Status().Patch(ctx, &current, client.MergeFrom(baseP)); err != nil {
				t.Fatal(err)
			}
			if err := role.snapshot.Authorize(ctx); err == nil {
				t.Fatal("prepared invocation survived changed admitted policy")
			}
			if strings.Contains(fmt.Sprint(current.Status.Execution), "privateKey") {
				t.Fatal("execution exposes key material")
			}
		})
	}
}

// scriptedRole supplies controlled old-policy observations and mutable backing storage.
type scriptedRole struct {
	result RoleResult
	calls  int
}

func (r *scriptedRole) Step(context.Context, Snapshot) (RoleResult, error) {
	r.calls++
	return r.result, nil
}

func (r *scriptedRole) Drain(context.Context, Snapshot) (DrainResult, error) {
	return DrainResult{Done: true, Settled: true}, nil
}

func TestPriorAppliedPolicyObservationRetainedAcrossPublicationFailure(t *testing.T) {
	ctx := context.Background()
	root, p, pod, profile := fixture(t)
	ProjectSession(root, p, pod, nil, time.Now())
	old := p.Status.Admission.PolicyDigest
	p.Status.Execution = &api.WorkerExecutionStatus{
		PodUID:              pod.UID,
		ProcessNonce:        "process",
		ProfileDigest:       profile.Digest(),
		AppliedPolicyDigest: old,
		Pending:             1,
		Phase:               "Active",
		ObservedAt:          metav1.Now(),
	}
	p.Status.Admission.Configuration.StacksTransactionProduction.Interval = ptr.To(common.Duration("9s"))
	p.Status.Admission.PolicyDigest = foundation.Digest(p.Status.Admission.Configuration)
	c := &statusClient{Client: fakeClient(t, root, p, pod), lose: true}
	role := &scriptedRole{
		result: RoleResult{
			AppliedPolicyDigest: old,
			Transactions:        &api.TransactionExecutionStatus{Included: 1},
			Reason:              "Included",
		},
	}
	r := Runtime{
		Client:          c,
		Namespace:       p.Namespace,
		ParticipantName: p.Name,
		ParticipantUID:  p.UID,
		NetworkUID:      root.UID,
		PodName:         pod.Name,
		PodUID:          pod.UID,
		Profile:         profile,
		Role:            role,
		nonce:           "process",
	}
	if _, err := r.Reconcile(ctx); err == nil || r.pendingReport == nil {
		t.Fatal("old pending result not retained")
	}
	role.result.Transactions.Included = 900
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if role.calls != 1 {
		t.Fatal("publication recovery ran another operation")
	}
	var current api.StacksNetworkParticipant
	if err := c.Get(ctx, client.ObjectKeyFromObject(p), &current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Execution.AppliedPolicyDigest != old || current.Status.Execution.Transactions.Included != 1 ||
		current.Status.Execution.Pending != 0 {
		t.Fatal("old operation evidence lost or aliased")
	}
	role.result.AppliedPolicyDigest = "arbitrary-old-policy"
	if result, err := r.Reconcile(ctx); err == nil || !result.Exit {
		t.Fatal("unrecorded old policy authorized")
	}
}

func TestInactiveCandidateNeverExecutesAndProcessRestartExits(t *testing.T) {
	root, p, pod, profile := fixture(t)
	c := &statusClient{Client: fakeClient(t, root, p, pod)}
	role := &testRole{}
	r := Runtime{
		Client:          c,
		Namespace:       p.Namespace,
		ParticipantName: p.Name,
		ParticipantUID:  p.UID,
		NetworkUID:      root.UID,
		PodName:         pod.Name,
		PodUID:          pod.UID,
		Profile:         profile,
		Role:            role,
	}
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if role.steps != 0 {
		t.Fatal("inactive candidate executed")
	}
	restarted := r
	restarted.nonce = "different-process"
	if result, err := restarted.Reconcile(context.Background()); err == nil || !result.Exit {
		t.Fatal("same Pod process restart recovered ephemeral state")
	}
}

func TestMutablePodProcessFieldsCannotKeepProfileAuthorization(t *testing.T) {
	for _, change := range []func(*corev1.Pod){
		func(p *corev1.Pod) { p.Spec.Containers[0].Image = "other:image" },
		func(p *corev1.Pod) { p.Spec.Containers[0].Args = []string{"other"} },
		func(p *corev1.Pod) { p.Spec.Volumes[1].Secret.SecretName = "other-key" },
		func(p *corev1.Pod) { p.Spec.Containers[0].Env[0].Value = "other" },
	} {
		root, p, pod, _ := fixture(t)
		change(pod)
		if _, err := profileFromPod(pod); err == nil {
			t.Fatal("changed Pod process accepted")
		}
		if fact := ProjectSession(
			root,
			p,
			pod,
			nil,
			time.Now(),
		); !fact.Failed ||
			root.Status.Identities[0].Worker != nil {
			t.Fatal("changed candidate bound")
		}
	}
}

func TestMutableReadsRetainOldNamedDependenciesUntilPolicyAdopted(t *testing.T) {
	root, p, _, profile := fixture(t)
	c := fakeClient(t, root, p)
	r := Reconciler{Client: c, Reader: c}
	if err := r.ensureSupport(context.Background(), p, profile); err != nil {
		t.Fatal(err)
	}
	before := profile.Digest()
	profile.Reads = []ReadBinding{
		{APIVersion: "stacks.stacks.org/v1alpha2", Resource: "stacksaccounts", Name: "new-recipient"},
	}
	if profile.Digest() != before {
		t.Fatal("read permission edit rolled worker identity")
	}
	p.Status.Execution = &api.WorkerExecutionStatus{Pending: 1, AppliedPolicyDigest: "prior"}
	if err := r.ensureSupport(context.Background(), p, profile); err != nil {
		t.Fatal(err)
	}
	var role rbacv1.Role
	if err := c.Get(context.Background(), client.ObjectKey{
		Namespace: p.Namespace,
		Name:      Name(p),
	}, &role); err != nil {
		t.Fatal(err)
	}
	if len(role.Rules) != 6 {
		t.Fatalf("pending dependency was removed: %v", role.Rules)
	}
	p.Status.Execution.Pending = 0
	p.Status.Execution.AppliedPolicyDigest = p.Status.Admission.PolicyDigest
	if err := r.ensureSupport(context.Background(), p, profile); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(&role), &role); err != nil {
		t.Fatal(err)
	}
	if len(role.Rules) != 5 {
		t.Fatal("settled obsolete dependency retained")
	}
}

func TestSuccessfulObservationHeartbeatDoesNotRefreshCachedEvidence(t *testing.T) {
	ctx := context.Background()
	root, p, pod, profile := fixture(t)
	ProjectSession(root, p, pod, nil, time.Now())
	c := &statusClient{Client: fakeClient(t, root, p, pod)}
	role := &scriptedRole{result: RoleResult{
		Reason:              "Current",
		AppliedPolicyDigest: p.Status.Admission.PolicyDigest,
	}}
	now := time.Now().Truncate(time.Second)
	r := Runtime{
		Client:          c,
		Namespace:       p.Namespace,
		ParticipantName: p.Name,
		ParticipantUID:  p.UID,
		NetworkUID:      root.UID,
		PodName:         pod.Name,
		PodUID:          pod.UID,
		Profile:         profile,
		Role:            role,
		Now:             func() time.Time { return now },
	}
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	initial := c.patches
	now = now.Add(4 * time.Second)
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if c.patches != initial {
		t.Fatal("unchanged observations caused unbounded heartbeat")
	}
	now = now.Add(time.Second)
	c.lose = true
	if _, err := r.Reconcile(ctx); err == nil || r.pendingReport == nil {
		t.Fatal("heartbeat publication failure not retained")
	}
	original := r.pendingReport.ObservedAt
	now = now.Add(time.Minute)
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	var current api.StacksNetworkParticipant
	if err := c.Get(ctx, client.ObjectKeyFromObject(p), &current); err != nil {
		t.Fatal(err)
	}
	if !current.Status.Execution.ObservedAt.Equal(&original) {
		t.Fatal("cached heartbeat retry freshened old evidence")
	}
}

// publicOnlyClient rejects private key reads and supplies server-like UIDs for generated CMs.
type publicOnlyClient struct {
	client.Client
	privateReads int
}

func (c *publicOnlyClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	object client.Object,
	options ...client.GetOption,
) error {
	if _, private := object.(*corev1.Secret); private {
		c.privateReads++
		return fmt.Errorf("shared operator read private material")
	}
	return c.Client.Get(ctx, key, object, options...)
}

func (c *publicOnlyClient) Create(ctx context.Context, object client.Object, options ...client.CreateOption) error {
	if object.GetUID() == "" {
		object.SetUID(types.UID("created-" + object.GetName()))
	}
	return c.Client.Create(ctx, object, options...)
}

func TestTransactionBootstrapPinsMetadataAndIgnoresMutablePolicy(t *testing.T) {
	ctx := context.Background()
	root, p, _, _ := fixture(t)
	account := &stacks.StacksAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "sender", Namespace: p.Namespace, UID: "account-uid", Generation: 1},
		Status: common.ResolutionStatus{
			ObservedGeneration: 1,
			Digest:             "public-digest",
			Identity:           &common.PublicIdentity{Address: "STTEST", PublicKey: "02public"},
			CredentialsRef:     &common.SecretKeyRef{Name: "sender-key", Key: "privateKey"},
			CredentialsUID:     "key-uid",
			Conditions: []metav1.Condition{
				{Type: "Resolved", Status: metav1.ConditionTrue, Reason: "Resolved", LastTransitionTime: metav1.Now()},
			},
		},
	}
	key := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "sender-key", Namespace: p.Namespace, UID: "key-uid"},
		Immutable:  ptr.To(true),
		Data:       map[string][]byte{"privateKey": []byte("never-read")},
	}
	genesis := &api.StacksGenesis{
		ObjectMeta: metav1.ObjectMeta{
			Name:      root.Status.GenesisRef.Name,
			Namespace: root.Namespace,
			UID:       root.Status.GenesisRef.UID,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: api.GroupVersion.String(),
					Kind:       "StacksNetwork",
					Name:       root.Name,
					UID:        root.UID,
					Controller: ptr.To(true),
				},
			},
		},
	}
	root.Status.GenesisDigest = foundation.Digest(genesis.Spec.Chain)
	p.Status.Admission.Configuration.StacksTransactionProduction.AccountRef = &common.NameRef{Name: account.Name}
	p.Status.Admission.Dependencies = []common.Binding{
		{Kind: "StacksAccount", Name: account.Name, UID: account.UID, Fingerprint: account.Status.Digest},
	}
	c := &publicOnlyClient{Client: fakeClient(t, root, p, account, key, genesis)}
	resolver := TransactionProfiles{Client: c, Reader: c, Image: "worker:fixed"}
	first, err := resolver.Resolve(ctx, root, p)
	if err != nil {
		t.Fatal(err)
	}
	p.Status.Admission.Configuration.StacksTransactionProduction.Interval = ptr.To(common.Duration("19s"))
	p.Status.Admission.Configuration.StacksTransactionProduction.FeeMicroSTX = ptr.To(common.Amount("900"))
	p.Status.Admission.PolicyDigest = foundation.Digest(p.Status.Admission.Configuration)
	second, err := resolver.Resolve(ctx, root, p)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != second.Digest() || first.Configuration.UID != second.Configuration.UID || c.privateReads != 0 {
		t.Fatal("live policy changed immutable bootstrap or exposed private key")
	}
	if first.Keys[0].Role != "sender" || first.Keys[0].Secret.UID != key.UID || first.Keys[0].Key != "privateKey" {
		t.Fatal("sender mount identity not pinned")
	}
	key.UID = "replacement"
	if err := c.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	key.ResourceVersion = ""
	if err := c.Client.Create(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(ctx, root, p); err == nil {
		t.Fatal("same-name sender credential replacement accepted")
	}
}

func TestStackerMountsAllFixedRolesBeforeFirstActivation(t *testing.T) {
	ctx := context.Background()
	root, p, _, _ := fixture(t)
	p.Spec.Kind = "StacksStacker"
	policy := &stacks.StacksStackerSpec{
		HolderAccountRef:        &common.NameRef{Name: "holder"},
		AdministratorAccountRef: &common.NameRef{Name: "admin"},
		SignerRef:               &common.NameRef{Name: "consensus-signer"},
	}
	p.Status.Admission.Configuration = api.Configuration{StacksStacker: policy}
	signer := p.DeepCopy()
	signer.Name = foundation.ParticipantName(string(root.UID), "consensus-signer")
	signer.UID = "signer-uid"
	signer.Spec.Kind = "StacksSigner"
	signer.Spec.ParticipantName = "consensus-signer"
	signer.Status.Admission = &api.Admission{
		Configuration: api.Configuration{
			StacksSigner: &stacks.StacksSignerSpec{AccountRef: &common.NameRef{Name: "consensus"}},
		},
	}
	p.Status.Admission.Dependencies = []common.Binding{
		{Kind: "StacksNetworkParticipant", Name: signer.Name, UID: signer.UID},
	}
	genesis := &api.StacksGenesis{
		ObjectMeta: metav1.ObjectMeta{
			Name:      root.Status.GenesisRef.Name,
			Namespace: p.Namespace,
			UID:       root.Status.GenesisRef.UID,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: api.GroupVersion.String(),
					Kind:       "StacksNetwork",
					Name:       root.Name,
					UID:        root.UID,
					Controller: ptr.To(true),
				},
			},
		},
	}
	root.Status.GenesisDigest = foundation.Digest(genesis.Spec.Chain)
	objects := []client.Object{root, p, signer, genesis}
	for _, name := range []string{"holder", "admin", "consensus"} {
		account := &stacks.StacksAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  p.Namespace,
				UID:        types.UID(name + "-account"),
				Generation: 1,
			},
			Status: common.ResolutionStatus{
				ObservedGeneration: 1,
				Digest:             name + "-public",
				Identity:           &common.PublicIdentity{Address: "ST" + name, PublicKey: "02" + name},
				CredentialsRef:     &common.SecretKeyRef{Name: name + "-key", Key: "privateKey"},
				CredentialsUID:     types.UID(name + "-key-uid"),
				Conditions: []metav1.Condition{
					{
						Type:               "Resolved",
						Status:             metav1.ConditionTrue,
						Reason:             "Resolved",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
		}
		key := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      account.Status.CredentialsRef.Name,
				Namespace: p.Namespace,
				UID:       account.Status.CredentialsUID,
			},
			Immutable: ptr.To(true),
			Data:      map[string][]byte{"privateKey": []byte("private")},
		}
		b := common.Binding{Kind: "StacksAccount", Name: name, UID: account.UID, Fingerprint: account.Status.Digest}
		if name == "consensus" {
			signer.Status.Admission.Dependencies = append(signer.Status.Admission.Dependencies, b)
		} else {
			p.Status.Admission.Dependencies = append(p.Status.Admission.Dependencies, b)
		}
		objects = append(objects, account, key)
	}
	c := &publicOnlyClient{Client: fakeClient(t, objects...)}
	profiles := Profiles{Client: c, Reader: c, Image: "worker:fixed"}
	profile, err := profiles.Resolve(ctx, root, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Keys) != 3 || profile.Keys[0].Role != "administrator" || profile.Keys[1].Role != "consensus" ||
		profile.Keys[2].Role != "holder" {
		t.Fatalf("incomplete first-activation key roles: %+v", profile.Keys)
	}
	seen := map[string]bool{}
	for _, read := range profile.Reads {
		seen[read.Name] = true
	}
	for _, name := range []string{"holder", "admin", "consensus", signer.Name, genesis.Name} {
		if !seen[name] {
			t.Fatalf("public prerequisite %s not scoped", name)
		}
	}
	pod, err := Pod(p, profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range profile.Keys {
		if key.Secret.Name == "" || key.Secret.UID == "" {
			t.Fatal("key mount identity omitted")
		}
	}
	if len(pod.Spec.Containers) != 1 || c.privateReads != 0 {
		t.Fatal("stacker crossed private key process boundary")
	}
}

func TestRetryCandidateCanBindBeforeItsFirstStatusPublication(t *testing.T) {
	root, p, pod, profile := fixture(t)
	p.Status.Execution = &api.WorkerExecutionStatus{
		PodUID:        "old-never-bound-pod",
		ProcessNonce:  "old-inactive-process",
		ProfileDigest: profile.Digest(),
		Phase:         "Inactive",
	}
	if fact := ProjectSession(root, p, pod, nil, time.Now()); !fact.Changed || fact.Failed {
		t.Fatal("retry candidate did not bind")
	}
	c := &statusClient{Client: fakeClient(t, root, p, pod)}
	role := &testRole{}
	r := Runtime{
		Client:          c,
		Namespace:       p.Namespace,
		ParticipantName: p.Name,
		ParticipantUID:  p.UID,
		NetworkUID:      root.UID,
		PodName:         pod.Name,
		PodUID:          pod.UID,
		Profile:         profile,
		Role:            role,
	}
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if role.steps != 1 {
		t.Fatal("inactive prior candidate status blocked the authorized process")
	}
}

func TestPoX5ObservationsClearWithoutDiscardingAdministratorAccounting(t *testing.T) {
	ctx := context.Background()
	root, p, pod, profile := fixture(t)
	ProjectSession(root, p, pod, nil, time.Now())
	p.Status.Execution = &api.WorkerExecutionStatus{
		PodUID:                    pod.UID,
		ProcessNonce:              "process",
		ProfileDigest:             profile.Digest(),
		AppliedPolicyDigest:       p.Status.Admission.PolicyDigest,
		Phase:                     "Active",
		ObservedAt:                metav1.Now(),
		PoX5:                      &api.PoX5EnrollmentObservation{TargetCycle: 15, TargetCycleMatched: true},
		AdministratorTransactions: &api.TransactionExecutionStatus{Included: 2},
	}
	c := &statusClient{Client: fakeClient(t, root, p, pod)}
	role := &scriptedRole{result: RoleResult{AppliedPolicyDigest: p.Status.Admission.PolicyDigest}}
	r := Runtime{
		Client:          c,
		Namespace:       p.Namespace,
		ParticipantName: p.Name,
		ParticipantUID:  p.UID,
		NetworkUID:      root.UID,
		PodName:         pod.Name,
		PodUID:          pod.UID,
		Profile:         profile,
		Role:            role,
		nonce:           "process",
	}
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	var current api.StacksNetworkParticipant
	if err := c.Get(ctx, client.ObjectKeyFromObject(p), &current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Execution.PoX5 != nil || current.Status.Execution.AdministratorTransactions == nil ||
		current.Status.Execution.AdministratorTransactions.Included != 2 {
		t.Fatal("stale enrollment survived or cumulative administrator evidence disappeared")
	}
	role.result.PoX5 = &api.PoX5EnrollmentObservation{TargetCycle: 15}
	role.result.AdministratorTransactions = &api.TransactionExecutionStatus{Included: 3}
	c.lose = true
	if _, err := r.Reconcile(ctx); err == nil {
		t.Fatal("failed status publication not surfaced")
	}
	role.result.PoX5.TargetCycle = 99
	role.result.AdministratorTransactions.Included = 99
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(p), &current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Execution.PoX5.TargetCycle != 15 ||
		current.Status.Execution.AdministratorTransactions.Included != 3 {
		t.Fatal("publication retry aliased mutable role evidence")
	}
}
