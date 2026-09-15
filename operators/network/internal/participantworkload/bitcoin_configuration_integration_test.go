//go:build integration

package participantworkload

import (
	"context"
	"encoding/json"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyBitcoinCandidateAPI validates real resolver outputs before genesis without actor activation.
func verifyBitcoinCandidateAPI(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	configuration := api.Configuration{
		BitcoinNode: &bitcoin.BitcoinNodeSpec{
			Observer: &bitcoin.BitcoinObserverSpec{Enabled: ptr.To(true), IntervalSeconds: ptr.To[int32](5)},
			ActorFields: common.ActorFields{
				Config: &common.Config{
					Overrides: &runtime.RawExtension{Raw: []byte(`{"dbcache":256,"regtest":{"maxconnections":32}}`)},
				},
			},
		},
	}
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test"},
		Spec: api.StacksNetworkSpec{
			Operation: "Running",
			Participants: []api.Participant{
				{Name: "core", Kind: "BitcoinNode", Definition: api.Definition{Inline: &configuration}},
			},
		},
	}
	must(c.Create(ctx, root))
	p := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      foundation.ParticipantName(string(root.UID), "core"),
			Namespace: root.Namespace,
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
			ParticipantName: "core",
			Kind:            "BitcoinNode",
			Configuration:   configuration,
		},
	}
	must(c.Create(ctx, p))
	root.Status.Identities = []api.InstanceIdentity{{Name: "core", UID: p.UID}}
	must(c.Status().Update(ctx, root))
	p.Status.Admission = &api.Admission{Configuration: configuration, PolicyDigest: digest(configuration)}
	in := foundation.CandidateConfiguration{
		Root:         root,
		Participants: map[string]*api.StacksNetworkParticipant{"core": p},
	}
	r := &Reconciler{
		Client:        c,
		Reader:        metadataOnlyReader{c},
		ResolverImage: "resolver:test",
		ObserverImage: "observer:test",
	}
	ready, err := r.ValidateConfiguration(ctx, in, "core")
	if err != nil || ready {
		t.Fatalf("candidate pending: ready=%v err=%v", ready, err)
	}
	var reports corev1.ConfigMapList
	must(
		c.List(
			ctx,
			&reports,
			client.InNamespace(p.Namespace),
			client.MatchingLabels{"network.stacks.org/participant-uid": string(p.UID)},
		),
	)
	if len(reports.Items) != 1 {
		t.Fatalf("reports=%d", len(reports.Items))
	}
	var input BitcoinConfigInput
	must(json.Unmarshal([]byte(reports.Items[0].Data["input.json"]), &input))
	var jobs batchv1.JobList
	must(
		c.List(
			ctx,
			&jobs,
			client.InNamespace(p.Namespace),
			client.MatchingLabels{"network.stacks.org/participant-uid": string(p.UID)},
		),
	)
	if len(jobs.Items) != 1 ||
		jobs.Items[0].Spec.Template.Spec.Containers[0].Args[1] != "--input-file=/input/input.json" {
		t.Fatal("resolver did not use bounded mounted input")
	}
	if input.ObserverCredentials == nil || input.ObserverCredentials.UID == "" {
		t.Fatal("observer binding missing from resolver input")
	}
	validObserver := &bitcoin.BitcoinNode{
		ObjectMeta: metav1.ObjectMeta{Name: "valid-observer", Namespace: p.Namespace},
		Spec: bitcoin.BitcoinNodeSpec{
			Observer: &bitcoin.BitcoinObserverSpec{Enabled: ptr.To(true), IntervalSeconds: ptr.To[int32](5)},
		},
	}
	must(c.Create(ctx, validObserver))
	must(c.Get(ctx, client.ObjectKeyFromObject(validObserver), validObserver))
	if validObserver.Spec.Observer == nil || !ptr.Deref(validObserver.Spec.Observer.Enabled, false) ||
		ptr.Deref(validObserver.Spec.Observer.IntervalSeconds, 0) != 5 {
		t.Fatal("observer fields were pruned")
	}
	for _, interval := range []int32{4, 301} {
		invalid := &bitcoin.BitcoinNode{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-observer", Namespace: p.Namespace},
			Spec: bitcoin.BitcoinNodeSpec{
				Observer: &bitcoin.BitcoinObserverSpec{Enabled: ptr.To(true), IntervalSeconds: &interval},
			},
		}
		if err := c.Create(ctx, invalid); !apierrors.IsInvalid(err) {
			t.Fatalf("expected observer interval admission rejection: %v", err)
		}
	}
	// Execute only the private Go resolver against the real API; no Core process is started.
	must(RunBitcoinConfigResolver(ctx, c, input))
	ready, err = r.ValidateConfiguration(ctx, in, "core")
	if err != nil || !ready {
		t.Fatalf("candidate agreement: ready=%v err=%v", ready, err)
	}
	var current api.StacksNetworkParticipant
	must(c.Get(ctx, client.ObjectKeyFromObject(p), &current))
	if current.Status.Admission != nil || current.Status.Runtime == nil || current.Status.Runtime.ConfigRef != nil {
		t.Fatal("candidate resolution published activation authority")
	}
	if current.Status.Runtime.ObserverRPCSecretRef == nil ||
		current.Status.Runtime.ObserverRPCSecretRef.UID != input.ObserverCredentials.UID {
		t.Fatal("observer credential pin not persisted by API server")
	}
	var observer corev1.Secret
	must(c.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: input.ObserverCredentials.Name}, &observer))
	if !ptr.Deref(observer.Immutable, false) || string(observer.Data["username"]) != "observer" {
		t.Fatal("observer credential unresolved")
	}
	var workloads appsv1.StatefulSetList
	must(
		c.List(
			ctx,
			&workloads,
			client.InNamespace(p.Namespace),
			client.MatchingLabels{"network.stacks.org/participant-uid": string(p.UID)},
		),
	)
	if len(workloads.Items) != 0 {
		t.Fatal("candidate resolver activated an actor")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
	if root.Status.GenesisRef != nil {
		t.Fatal("candidate resolver published genesis")
	}
	verifyObserverWorkloadTransitions(t, ctx, c, p, current.Status.Runtime)
	var output corev1.Secret
	must(c.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: input.Config.Name}, &output))
	must(c.Delete(ctx, &output))
	output.UID = ""
	output.ResourceVersion = ""
	must(c.Create(ctx, &output))
	if ready, err = r.ValidateConfiguration(ctx, in, "core"); err == nil || ready {
		t.Fatal("replacement output reused prior validation")
	}
}

// verifyObserverWorkloadTransitions exercises container/volume removal through real API merge patches.
// Envtest has no StatefulSet controller; Pod replacement itself is qualified live.
func verifyObserverWorkloadTransitions(t *testing.T, ctx context.Context, c client.Client,
	p *api.StacksNetworkParticipant, state *api.ParticipantRuntimeStatus,
) {
	t.Helper()
	r := Reconciler{Client: c, Reader: c}
	pin := *state.ObserverRPCSecretRef
	var uid types.UID
	var generation int64
	for _, enabled := range []bool{true, false, true} {
		p.Status.Admission.Configuration.BitcoinNode.Observer.Enabled = ptr.To(enabled)
		desired, err := StatefulSet(p, "config", "actor", 1)
		if err != nil {
			t.Fatal(err)
		}
		if err = attachObserver(p, state, desired, "worker:test"); err != nil {
			t.Fatal(err)
		}
		if err = r.reconcileStatefulSet(ctx, p, desired); err != nil {
			t.Fatal(err)
		}
		var actual appsv1.StatefulSet
		if err = c.Get(ctx, client.ObjectKeyFromObject(desired), &actual); err != nil {
			t.Fatal(err)
		}
		if uid != "" && (actual.UID != uid || actual.Generation <= generation) {
			t.Fatal("workload identity or revision did not persist")
		}
		uid, generation = actual.UID, actual.Generation
		containers, volumes := 0, 0
		for _, container := range actual.Spec.Template.Spec.Containers {
			if container.Name == "bitcoin-observer" {
				containers++
			}
		}
		for _, volume := range actual.Spec.Template.Spec.Volumes {
			if volume.Name == "bitcoin-observer" {
				volumes++
				if volume.Secret == nil || volume.Secret.SecretName != pin.Name {
					t.Fatal("credential mount rebound")
				}
			}
		}
		want := 0
		if enabled {
			want = 1
		}
		if containers != want || volumes != want || *state.ObserverRPCSecretRef != pin {
			t.Fatal("observer removal/re-enable changed identity or left runtime fields")
		}
		var persisted api.StacksNetworkParticipant
		if err = c.Get(ctx, client.ObjectKeyFromObject(p), &persisted); err != nil {
			t.Fatal(err)
		}
		if persisted.Status.Runtime.ObserverRPCSecretRef.UID != pin.UID {
			t.Fatal("persisted credential pin changed")
		}
	}
}
