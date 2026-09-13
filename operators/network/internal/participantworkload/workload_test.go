package participantworkload

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// participantFixture supplies an admitted standalone Bitcoin policy.
func participantFixture() *api.StacksNetworkParticipant {
	configuration := api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{}}
	return &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "participant",
			Namespace:  "test",
			UID:        "participant-uid",
			Generation: 1,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: api.GroupVersion.String(),
					Kind:       "StacksNetwork",
					Name:       "network",
					UID:        "network-uid",
					Controller: ptr.To(true),
				},
			},
		},
		Spec: api.StacksNetworkParticipantSpec{
			NetworkUID:      "network-uid",
			ParticipantName: "btc-01",
			Kind:            "BitcoinNode",
			Configuration:   configuration,
		},
		Status: api.ParticipantStatus{
			Admission: &api.Admission{Configuration: configuration, PolicyDigest: digest(configuration)},
		},
	}
}

// testClient registers replacement and core resources without legacy APIs.
func testClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		appsv1.AddToScheme,
		api.AddToScheme,
		bitcoin.AddToScheme,
		stacks.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&corev1.Pod{}, &appsv1.StatefulSet{}, &api.StacksNetworkParticipant{}).
		WithObjects(objects...).
		Build()
}

func TestStatefulSetStorageAndCredentialBoundary(t *testing.T) {
	for _, retain := range []bool{false, true} {
		p := participantFixture()
		p.Status.Admission.Configuration.BitcoinNode.Storage = &common.Storage{
			Size:           ptr.To("5Gi"),
			RetainOnDelete: ptr.To(retain),
			Class:          ptr.To(""),
		}
		workload, err := StatefulSet(p, "server-config", "restricted-credentials", 1)
		if err != nil {
			t.Fatal(err)
		}
		policy := workload.Spec.PersistentVolumeClaimRetentionPolicy
		if policy.WhenScaled != appsv1.RetainPersistentVolumeClaimRetentionPolicyType ||
			(policy.WhenDeleted == appsv1.RetainPersistentVolumeClaimRetentionPolicyType) != retain {
			t.Fatal("incorrect PVC lifecycle")
		}
		claim := workload.Spec.VolumeClaimTemplates[0]
		if len(claim.OwnerReferences) != 0 || claim.Spec.StorageClassName == nil || *claim.Spec.StorageClassName != "" {
			t.Fatal("claim adds ownership or loses explicit empty class")
		}
		pod := workload.Spec.Template
		if len(pod.Finalizers) != 1 || pod.Finalizers[0] != PodFinalizer {
			t.Fatal("Pod must retain termination evidence from creation")
		}
		if pod.Labels["network.stacks.org/role"] != "actor" ||
			pod.Labels["network.stacks.org/participant-uid"] != string(p.UID) ||
			ptr.Deref(pod.Spec.AutomountServiceAccountToken, true) {
			t.Fatal("actor identity/API scope mismatch")
		}
		for _, env := range pod.Spec.Containers[0].Env {
			if env.ValueFrom.SecretKeyRef.Name != "restricted-credentials" {
				t.Fatal("actor received another RPC principal")
			}
		}
		fresh := p.DeepCopy()
		fresh.UID = "replacement"
		if Name(fresh, "actor") == Name(p, "actor") || len(workload.Name) > 52 {
			t.Fatal("runtime name does not isolate participant identity")
		}
	}
	p := participantFixture()
	p.Status.Admission.Configuration.BitcoinNode.Storage = &common.Storage{Ephemeral: ptr.To(true)}
	workload, err := StatefulSet(p, "config", "actor", 0)
	if err != nil || len(workload.Spec.VolumeClaimTemplates) != 0 {
		t.Fatalf("ephemeral policy: %v", err)
	}
	for _, volume := range workload.Spec.Template.Spec.Volumes {
		if volume.Name == "data" && volume.EmptyDir != nil {
			return
		}
	}
	t.Fatal("ephemeral data volume missing")
}

// TestActorWritableTemporaryStorage keeps native database scratch separate from persistent data.
func TestActorWritableTemporaryStorage(t *testing.T) {
	for _, kind := range []api.ParticipantKind{"BitcoinNode", "StacksNode", "StacksSigner"} {
		t.Run(string(kind), func(t *testing.T) {
			p := participantFixture()
			if kind != "BitcoinNode" {
				p = stacksParticipantFixture(kind)
			}
			workload, err := StatefulSet(p, "config", "credentials", 1)
			if err != nil {
				t.Fatal(err)
			}
			pod := workload.Spec.Template.Spec
			container := pod.Containers[0]
			if !ptr.Deref(container.SecurityContext.ReadOnlyRootFilesystem, false) ||
				ptr.Deref(pod.SecurityContext.FSGroup, 0) == 0 {
				t.Fatal("native actor must retain read-only root and non-root volume access")
			}
			mounted, ephemeral := false, false
			for _, mount := range container.VolumeMounts {
				if mount.MountPath == "/tmp" {
					mounted = mount.Name == "tmp" && !mount.ReadOnly && mount.SubPath == ""
				}
			}
			for _, volume := range pod.Volumes {
				if volume.Name == "tmp" {
					ephemeral = volume.EmptyDir != nil && volume.PersistentVolumeClaim == nil && volume.Secret == nil
				}
			}
			if !mounted || !ephemeral || len(workload.Spec.VolumeClaimTemplates) != 1 ||
				workload.Spec.VolumeClaimTemplates[0].Name != "data" {
				t.Fatal("writable temporary storage must not use a retained data claim")
			}
		})
	}
}

func TestResolverRetriesKeepExactCredentialsAndPublicReport(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprint(commit), func(t *testing.T) {
			p := participantFixture()
			config := &corev1.Secret{ObjectMeta: objectMeta(p, "config", "support")}
			config.UID = "config-uid"
			control := &corev1.Secret{ObjectMeta: objectMeta(p, "control", "support")}
			control.UID = "control-uid"
			actor := &corev1.Secret{ObjectMeta: objectMeta(p, "actor-rpc", "support")}
			actor.UID = "actor-uid"
			report := &corev1.ConfigMap{ObjectMeta: objectMeta(p, "report", "support")}
			report.UID = "report-uid"
			base := testClient(t, config, control, actor, report)
			input := BitcoinConfigInput{
				Namespace:          p.Namespace,
				ParticipantUID:     p.UID,
				PolicyDigest:       "policy",
				Config:             *binding("Secret", config),
				ControlCredentials: *binding("Secret", control),
				ActorCredentials:   *binding("Secret", actor),
				Report:             *binding("ConfigMap", report),
				Seeds:              []string{"peer.test.svc"},
			}
			interrupted := &lostCredentialWrite{Client: base, commit: commit}
			ctx := context.Background()
			if err := RunBitcoinConfigResolver(ctx, interrupted, input); err == nil {
				t.Fatal("lost acknowledgement not surfaced")
			}
			if err := base.Get(ctx, client.ObjectKeyFromObject(control), control); err != nil {
				t.Fatal(err)
			}
			original := string(control.Data["password"])
			if err := RunBitcoinConfigResolver(ctx, base, input); err != nil {
				t.Fatal(err)
			}
			for _, object := range []client.Object{config, control, actor, report} {
				if err := base.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
					t.Fatal(err)
				}
			}
			if commit && original != string(control.Data["password"]) {
				t.Fatal("retry replaced committed credential")
			}
			if string(actor.Data["password"]) == string(control.Data["password"]) {
				t.Fatal("RPC principals share credentials")
			}
			for _, secret := range []*corev1.Secret{control, actor} {
				if strings.Contains(report.Data["report.json"], string(secret.Data["password"])) ||
					strings.Contains(string(config.Data["bitcoin.conf"]), string(secret.Data["password"])) {
					t.Fatal("private RPC credential escaped into public/server material")
				}
			}
			rendered, err := renderConfig(input.Seeds, actor, control)
			if err != nil || rendered != string(config.Data["bitcoin.conf"]) {
				t.Fatalf("configuration rendering is not deterministic: %v", err)
			}
			server := string(config.Data["bitcoin.conf"])
			if strings.Contains(actorMethods, "generatetoaddress") || strings.Contains(actorMethods, "gettxout") ||
				!strings.Contains(controlMethods, "gettxout") ||
				!strings.Contains(server, "rpcwhitelistdefault=1") ||
				!strings.Contains(server, "rpcwhitelist=control:") {
				t.Fatal("RPC method separation missing")
			}
			for _, method := range []string{
				"listdescriptors",
				"validateaddress",
				"getdescriptorinfo",
				"gettxout",
				"listunspent",
				"generatetoaddress",
				"createwallet",
				"loadwallet",
				"importdescriptors",
				"unloadwallet",
			} {
				if !renderedRPCAllows(server, "control", method) {
					t.Fatalf("control profile denies required %s", method)
				}
			}
			for _, method := range []string{
				"generatetoaddress",
				"createwallet",
				"importdescriptors",
				"invalidateblock",
				"reconsiderblock",
			} {
				if renderedRPCAllows(server, "actor", method) {
					t.Fatalf("actor profile permits managed mutation %s", method)
				}
			}
			before := report.Data["report.json"]
			if err := RunBitcoinConfigResolver(ctx, base, input); err != nil {
				t.Fatal(err)
			}
			if err := base.Get(ctx, client.ObjectKeyFromObject(report), report); err != nil {
				t.Fatal(err)
			}
			if report.Data["report.json"] != before {
				t.Fatal("idempotent resolver changed report")
			}
			input.ActorCredentials.UID = "replacement"
			if err := RunBitcoinConfigResolver(ctx, base, input); err == nil {
				t.Fatal("accepted replacement credential identity")
			}
		})
	}
}

// lostCredentialWrite models both committed and rejected writes with lost acknowledgements.
type lostCredentialWrite struct {
	client.Client
	commit, failed bool
}

func (c *lostCredentialWrite) Patch(
	ctx context.Context,
	object client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	if _, ok := object.(*corev1.Secret); ok && !c.failed {
		c.failed = true
		if c.commit {
			if err := c.Client.Patch(ctx, object, patch, opts...); err != nil {
				return err
			}
		}
		return fmt.Errorf("acknowledgement lost")
	}
	return c.Client.Patch(ctx, object, patch, opts...)
}

func TestFreshAdmissionAndGenesisGates(t *testing.T) {
	p := participantFixture()
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: p.Namespace, UID: p.Spec.NetworkUID},
		Spec: api.StacksNetworkSpec{
			Operation:    "Running",
			Participants: []api.Participant{{Name: p.Spec.ParticipantName, Kind: p.Spec.Kind}},
		},
		Status: api.StacksNetworkStatus{
			Identities: []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: p.UID}},
		},
	}
	genesis := &api.StacksGenesis{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "genesis",
			Namespace: p.Namespace,
			UID:       "genesis-uid",
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
		Spec: api.StacksGenesisSpec{Source: api.GenesisSource{NetworkUID: root.UID}},
	}
	root.Status.GenesisRef = binding("StacksGenesis", genesis)
	root.Status.GenesisDigest = digest(genesis.Spec.Chain)
	c := testClient(t, p, root, genesis)
	r := Reconciler{Client: c, Reader: c}
	if err := r.authorized(context.Background(), root, p); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*api.StacksNetwork){
		func(n *api.StacksNetwork) { n.Spec.Operation = "Stopped" },
		func(n *api.StacksNetwork) { n.Status.GenesisRef.UID = "replacement" },
		func(n *api.StacksNetwork) { n.Status.GenesisDigest = "corrupt" },
		func(n *api.StacksNetwork) { n.Spec.Participants = nil },
		func(n *api.StacksNetwork) { n.Status.Identities[0].Removing = true },
	} {
		current := root.DeepCopy()
		mutate(current)
		if err := r.authorized(context.Background(), current, p); err == nil {
			t.Fatal("accepted invalid lifecycle/genesis identity")
		}
	}
}

func TestShutdownRetainsUnknownAndSettlesTerminatedPod(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		p := participantFixture()
		workload, err := StatefulSet(p, "config", "actor", 0)
		if err != nil {
			t.Fatal(err)
		}
		workload.UID = "workload-uid"
		workload.Generation = 1
		workload.Status.ObservedGeneration = 1
		state := api.ParticipantRuntimeStatus{
			PodRef: &common.Binding{Kind: "Pod", Name: workload.Name + "-0", UID: "pod-uid"},
		}
		objects := []client.Object{workload}
		if terminal {
			objects = append(
				objects,
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:       workload.Name + "-0",
						Namespace:  p.Namespace,
						UID:        "pod-uid",
						Labels:     Labels(p, "actor"),
						Finalizers: []string{PodFinalizer},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "apps/v1",
								Kind:       "StatefulSet",
								Name:       workload.Name,
								UID:        workload.UID,
								Controller: ptr.To(true),
							},
						},
					},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "bitcoin"}}},
					Status: corev1.PodStatus{
						Phase: corev1.PodSucceeded,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "bitcoin",
								State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
							},
						},
					},
				},
			)
		}
		c := testClient(t, objects...)
		r := Reconciler{Client: c, Reader: c}
		reason, err := r.shutdown(context.Background(), p, &state, false)
		if err != nil {
			t.Fatal(err)
		}
		if state.Terminated != terminal || !terminal && reason != "TerminationUnknown" {
			t.Fatalf("termination=%v reason=%s", state.Terminated, reason)
		}
	}
}

func TestControllerInspectsSecretMetadataOnly(t *testing.T) {
	p := participantFixture()
	secret := &corev1.Secret{
		ObjectMeta: objectMeta(p, "rpc-control", "support"),
		Data:       map[string][]byte{"password": []byte("must-not-read")},
	}
	secret.UID = "secret-uid"
	c := testClient(t, secret)
	reader := &denySecretData{Client: c}
	r := Reconciler{Client: c, Reader: reader}
	ref, err := r.emptySecret(context.Background(), p, "rpc-control", binding("Secret", secret))
	if err != nil || ref.UID != secret.UID {
		t.Fatalf("metadata binding: %v", err)
	}
	foreign := *binding("Secret", secret)
	foreign.UID = types.UID("replacement")
	if _, err := r.emptySecret(context.Background(), p, "rpc-control", &foreign); err == nil {
		t.Fatal("rebound a replacement Secret")
	}
}

// denySecretData prevents accidentally introducing private data reads in shared controllers.
type denySecretData struct{ client.Client }

func (c *denySecretData) Get(
	ctx context.Context,
	key client.ObjectKey,
	object client.Object,
	opts ...client.GetOption,
) error {
	if _, ok := object.(*corev1.Secret); ok {
		return fmt.Errorf("shared controller read Secret data")
	}
	return c.Client.Get(ctx, key, object, opts...)
}

func TestPeerSeedsUseAllocatedUIDsWithoutReadyDependencies(t *testing.T) {
	p := participantFixture()
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Namespace: p.Namespace, UID: p.Spec.NetworkUID},
		Spec:       api.StacksNetworkSpec{Participants: []api.Participant{{Name: "btc-02", Kind: "BitcoinNode"}}},
		Status:     api.StacksNetworkStatus{Identities: []api.InstanceIdentity{{Name: "btc-02", UID: "peer-uid"}}},
	}
	seeds, err := peerSeeds(root, p)
	if err != nil || len(seeds) != 1 {
		t.Fatalf("seed resolution: %v %v", seeds, err)
	}
	root.Status.Identities[0].UID = "new-peer-uid"
	next, err := peerSeeds(root, p)
	if err != nil || reflect.DeepEqual(seeds, next) {
		t.Fatal("peer seed DNS ignored instance identity")
	}
}

func TestTerminalStopWaitsForControlDrain(t *testing.T) {
	p := participantFixture()
	workload, err := StatefulSet(p, "config", "actor", 1)
	if err != nil {
		t.Fatal(err)
	}
	workload.UID = "workload-uid"
	c := testClient(t, workload)
	ctx := context.Background()
	state := api.ParticipantRuntimeStatus{}
	ready, unavailable := false, false
	r := Reconciler{
		Client: c,
		Reader: c,
		BeforeStop: func(context.Context, *api.StacksNetworkParticipant) (bool, error) {
			if unavailable {
				return false, fmt.Errorf("control status unavailable")
			}
			return ready, nil
		},
	}
	for _, failed := range []bool{false, true} {
		unavailable = failed
		reason, err := r.stopActor(ctx, p, &state, true)
		if (err != nil) != failed || !failed && reason != "DrainingControl" {
			t.Fatalf("drain result: %s %v", reason, err)
		}
		if err := c.Get(ctx, client.ObjectKeyFromObject(workload), workload); err != nil {
			t.Fatal(err)
		}
		if *workload.Spec.Replicas != 1 || state.Terminated {
			t.Fatal("closed RPC before drainage acknowledgement")
		}
	}
	ready, unavailable = true, false
	if _, err := r.stopActor(ctx, p, &state, false); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(workload), workload); err != nil {
		t.Fatal(err)
	}
	if *workload.Spec.Replicas != 0 {
		t.Fatal("acknowledged drainage did not permit shutdown")
	}
}

// renderedRPCAllows checks exact method tokens in the configuration consumed by Core.
func renderedRPCAllows(config, user, method string) bool {
	prefix := "rpcwhitelist=" + user + ":"
	for _, line := range strings.Split(config, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		for _, allowed := range strings.Split(strings.TrimPrefix(line, prefix), ",") {
			if allowed == method {
				return true
			}
		}
	}
	return false
}

// TestActorCanObserveLoadedWalletsWithoutManagingThem matches Core 31.1 miner startup.
func TestActorCanObserveLoadedWalletsWithoutManagingThem(t *testing.T) {
	methods := map[string]bool{}
	for _, method := range strings.Split(actorMethods, ",") {
		methods[method] = true
	}
	if !methods["listwallets"] {
		t.Fatal("miner cannot check its controller-loaded wallet")
	}
	for _, method := range []string{
		"loadwallet",
		"createwallet",
		"unloadwallet",
		"importdescriptors",
		"generatetoaddress",
		"invalidateblock",
		"reconsiderblock",
	} {
		if methods[method] {
			t.Fatalf("actor gained control method %s", method)
		}
	}
}

func TestCompletedStopDoesNotDependOnGarbageCollectedDrainRecord(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		confirmed, podPresent bool
		wantCalls             int
	}{
		{name: "confirmed and absent", confirmed: true},
		{name: "absence is not termination proof", wantCalls: 1},
		{name: "current Pod still requires drain", confirmed: true, podPresent: true, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := participantFixture()
			workload, err := StatefulSet(p, "config", "actor", 0)
			if err != nil {
				t.Fatal(err)
			}
			workload.UID, workload.Generation, workload.Status.ObservedGeneration = "workload-uid", 1, 1
			objects := []client.Object{workload}
			if tc.podPresent {
				objects = append(
					objects,
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: p.Namespace,
							Name:      workload.Name + "-0",
							UID:       "new-pod",
						},
					},
				)
			}
			c := testClient(t, objects...)
			calls := 0
			r := Reconciler{
				Client: c,
				Reader: c,
				BeforeStop: func(context.Context, *api.StacksNetworkParticipant) (bool, error) {
					calls++
					return false, fmt.Errorf("execution record unavailable")
				},
			}
			state := api.ParticipantRuntimeStatus{
				PodRef:     &common.Binding{Kind: "Pod", Name: workload.Name + "-0", UID: "old-pod"},
				Terminated: tc.confirmed,
			}
			_, err = r.stopActor(context.Background(), p, &state, true)
			if calls != tc.wantCalls || (err != nil) != (tc.wantCalls > 0) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			var current appsv1.StatefulSet
			getErr := c.Get(context.Background(), client.ObjectKeyFromObject(workload), &current)
			if tc.wantCalls > 0 && getErr != nil {
				t.Fatal("undrained workload removed")
			}
			if tc.wantCalls == 0 && getErr == nil && current.DeletionTimestamp == nil {
				t.Fatal("terminated workload retained")
			}
		})
	}
}
