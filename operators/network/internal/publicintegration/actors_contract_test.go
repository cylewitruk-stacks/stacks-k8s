//go:build live

package publicintegration

import (
	"context"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// actorSnapshot supplies synthetic native observations solely to test rejection predicates.
func actorSnapshot() snapshot {
	at := time.Now().UTC()
	return snapshot{
		At: at,
		Status: api.StacksNetworkStatus{
			GenesisRef:        &common.Binding{Kind: "StacksGenesis", Name: "genesis", UID: "genesis"},
			ObservationPolicy: &api.ObservationPolicy{PollIntervalSeconds: 5, RPCAllowanceSeconds: 10},
		},
		Participants: []participantEvidence{
			{
				Identity: identity{Name: "generated", UID: "participant", Generation: 2},
				Name:     "late",
				Kind:     "StacksNode",
				Status: api.ParticipantStatus{
					Conditions: []metav1.Condition{
						{Type: "WorkloadReady", Status: metav1.ConditionTrue, ObservedGeneration: 2},
						{Type: "ConfigVerified", Status: metav1.ConditionTrue, ObservedGeneration: 2},
					},
					Runtime: &api.ParticipantRuntimeStatus{
						ObservedGeneration:  2,
						PodRef:              &common.Binding{Name: "pod", UID: "pod"},
						ContainerID:         "container",
						ConfigRef:           &common.Binding{Name: "config", UID: "config"},
						ConfigurationDigest: "config-digest",
						Protocol: &api.StacksProtocolObservation{
							Available:           true,
							FullySynced:         true,
							PodUID:              "pod",
							ContainerID:         "container",
							ConfigurationDigest: "config-digest",
							GenesisUID:          "genesis",
							IndexBlockID:        "block",
							StacksHeight:        10,
							ObservedAt:          metav1.NewTime(at),
						},
					},
				},
			},
		},
	}
}

func TestActorQualificationRejectsStaleOrMismatchedNativeEvidence(t *testing.T) {
	for _, mode := range []string{
		"valid",
		"stale",
		"wrong-pod",
		"wrong-container",
		"wrong-config",
		"wrong-genesis",
		"stale-generation",
		"unverified",
		"no-progress",
		"not-synced",
	} {
		t.Run(mode, func(t *testing.T) {
			s := actorSnapshot()
			p := &s.Participants[0]
			r := p.Status.Runtime
			switch mode {
			case "stale":
				r.Protocol.ObservedAt = metav1.NewTime(s.At.Add(-time.Hour))
			case "wrong-pod":
				r.PodRef.UID = "replacement"
			case "wrong-container":
				r.ContainerID = "replacement"
			case "wrong-config":
				r.ConfigurationDigest = "replacement"
			case "wrong-genesis":
				r.Protocol.GenesisUID = "replacement"
			case "stale-generation":
				p.Identity.Generation++
			case "unverified":
				p.Status.Conditions[1].Status = metav1.ConditionFalse
			case "no-progress":
				r.Protocol.StacksHeight = 5
			case "not-synced":
				r.Protocol.FullySynced = false
			}
			_, ready := readyFollower(s, "late", 5)
			if ready != (mode == "valid") {
				t.Fatalf("mode %s accepted=%v", mode, ready)
			}
		})
	}
}

func TestActorQualificationRequiresNewParticipantPodClaimAndVolume(t *testing.T) {
	old := actorEpoch{
		Participant: identity{UID: "first"},
		Runtime:     api.ParticipantRuntimeStatus{PodRef: &common.Binding{UID: "pod-old"}},
		Storage:     actorStorage{Claim: identity{Name: "claim-old", UID: "claim-old"}, Volume: "pv-old"},
	}
	for _, mode := range []string{
		"valid",
		"same-participant",
		"same-pod",
		"same-claim-uid",
		"same-claim-name",
		"same-volume",
		"empty-volume",
	} {
		t.Run(mode, func(t *testing.T) {
			next := actorEpoch{
				Participant: identity{UID: "second"},
				Runtime:     api.ParticipantRuntimeStatus{PodRef: &common.Binding{UID: "pod-new"}},
				Storage:     actorStorage{Claim: identity{Name: "claim-new", UID: "claim-new"}, Volume: "pv-new"},
			}
			switch mode {
			case "same-participant":
				next.Participant.UID = old.Participant.UID
			case "same-pod":
				next.Runtime.PodRef.UID = old.Runtime.PodRef.UID
			case "same-claim-uid":
				next.Storage.Claim.UID = old.Storage.Claim.UID
			case "same-claim-name":
				next.Storage.Claim.Name = old.Storage.Claim.Name
			case "same-volume":
				next.Storage.Volume = old.Storage.Volume
			case "empty-volume":
				next.Storage.Volume = ""
			}
			if err := newStorageEpoch(old, next); (err == nil) != (mode == "valid") {
				t.Fatalf("storage identity %s: %v", mode, err)
			}
		})
	}
}

func TestActorQualificationDetectsUnrelatedRollAndGenesisChange(t *testing.T) {
	for _, mode := range []string{
		"valid",
		"pod-roll",
		"container-restart",
		"genesis-replaced",
		"genesis-content",
		"genesis-binding",
	} {
		t.Run(mode, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := api.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			genesis := &api.StacksGenesis{
				ObjectMeta: metav1.ObjectMeta{Name: "genesis", Namespace: "test", UID: "genesis"},
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "test", UID: "pod"},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "core"}}},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:        "core",
							ContainerID: "container",
							State:       corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
						},
					},
				},
			}
			epoch, err := podEpoch(pod)
			if err != nil {
				t.Fatal(err)
			}
			guard := actorGuard{
				genesis: *genesis.DeepCopy(),
				binding: common.Binding{Kind: "StacksGenesis", Name: "genesis", UID: "genesis"},
				pods:    map[string]actorPodEpoch{pod.Name: epoch},
			}
			binding := guard.binding
			s := snapshot{Status: api.StacksNetworkStatus{GenesisRef: &binding}}
			switch mode {
			case "pod-roll":
				pod.UID = "replacement"
			case "container-restart":
				pod.Status.ContainerStatuses[0].ContainerID = "replacement"
			case "genesis-replaced":
				genesis.UID = "replacement"
			case "genesis-content":
				genesis.Spec.Chain.Profile = "changed-profile"
			case "genesis-binding":
				s.Status.GenesisRef.UID = "replacement"
			}
			h := harness{
				c:      fake.NewClientBuilder().WithScheme(scheme).WithObjects(genesis, pod).Build(),
				config: liveConfig{fixtureOptions: fixtureOptions{namespace: "test"}},
			}
			if err := h.checkActorGuard(context.Background(), guard, s); (err == nil) != (mode == "valid") {
				t.Fatalf("guard %s: %v", mode, err)
			}
		})
	}
}

func TestActorQualificationUsesReusableDeclarationsAndBoundedPublicOverrides(t *testing.T) {
	for _, variant := range []string{"minimal14", "full30"} {
		t.Run(variant, func(t *testing.T) {
			fixture, err := loadFixture(
				fixtureOptions{
					path:         defaultFixture,
					namespace:    "test",
					variant:      variant,
					bitcoinImage: "bitcoin:pinned",
					stacksImage:  "stacks:pinned",
					signerImage:  "stacks:pinned",
					cadence:      5 * time.Second,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			found := map[string]bool{}
			for _, o := range fixture.reusable {
				if o.GetKind() == "StacksNode" {
					if o.GetName() == "follower-01" {
						found["follower"] = true
					}
					account, _, _ := unstructured.NestedString(o.Object, "spec", "identityAccountRef", "name")
					if account == "traffic-recipient" {
						t.Fatal("late account already supplies a peer identity")
					}
				}
				if o.GetKind() == "StacksAccount" && o.GetName() == "traffic-recipient" {
					found["account"] = true
				}
			}
			if len(found) != 2 {
				t.Fatal("fixture lost reusable follower/account")
			}
			p := lateFollower("late", "stacks:pinned", true)
			node := p.Overrides.StacksNode
			if p.Definition.Ref == nil || p.Definition.Ref.Name != "follower-01" ||
				node.IdentityAccountRef.Name != "traffic-recipient" ||
				node.BitcoinNodeRef.Name != "btc-07" ||
				*node.Mining.Enabled ||
				!*node.Storage.RetainOnDelete ||
				*node.Image != "stacks:pinned" {
				t.Fatal("late follower changed declared public boundaries")
			}
		})
	}
}
