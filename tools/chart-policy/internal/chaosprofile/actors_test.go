package chaosprofile

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// actorFixture models public replacement identities without synthesizing labels in the resolver.
func actorFixture(t *testing.T) (client.Client, *unstructured.Unstructured, *unstructured.Unstructured, *corev1.Pod) {
	t.Helper()
	uid := "11111111-1111-1111-1111-111111111111"
	participantUID := "22222222-2222-2222-2222-222222222222"
	root := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "network.stacks.org/v1alpha2",
			"kind":       "StacksNetwork",
			"metadata":   map[string]any{"name": "network", "namespace": "test", "uid": uid},
			"spec": map[string]any{
				"participants": []any{map[string]any{"name": "bitcoin", "kind": "BitcoinNode"}},
			},
			"status": map[string]any{"identities": []any{map[string]any{"name": "bitcoin", "uid": participantUID}}},
		},
	}
	participant := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "network.stacks.org/v1alpha2",
			"kind":       "StacksNetworkParticipant",
			"metadata":   map[string]any{"name": "generated-participant", "namespace": "test", "uid": participantUID},
			"spec":       map[string]any{"networkUID": uid, "participantName": "bitcoin", "kind": "BitcoinNode"},
			"status": map[string]any{
				"runtime": map[string]any{"podRef": map[string]any{"name": "actor-0", "uid": "pod-uid"}},
			},
		},
	}
	participant.SetOwnerReferences(
		[]metav1.OwnerReference{
			{
				APIVersion: root.GetAPIVersion(),
				Kind:       root.GetKind(),
				Name:       root.GetName(),
				UID:        root.GetUID(),
				Controller: ptr.To(true),
			},
		},
	)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "actor-0",
			Namespace: "test",
			UID:       "pod-uid",
			Labels: (ActorIdentity{
				NetworkName:    "network",
				NetworkUID:     types.UID(uid),
				Name:           "bitcoin",
				ParticipantUID: types.UID(participantUID),
			}).Labels(),
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(root, participant, pod).Build()
	return c, root, participant, pod
}

func TestResolveActorRequiresCurrentPublicIdentities(t *testing.T) {
	for _, mode := range []string{
		"valid",
		"legacy",
		"removed",
		"participant-owner",
		"network-uid",
		"missing-runtime",
		"control-role",
		"replaced-pod",
		"duplicate-pod",
	} {
		t.Run(mode, func(t *testing.T) {
			c, root, p, pod := actorFixture(t)
			ctx := context.Background()
			update := func(o client.Object) {
				t.Helper()
				if err := c.Update(ctx, o); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "legacy":
				if err := c.Delete(ctx, root); err != nil {
					t.Fatal(err)
				}
			case "removed":
				_ = unstructured.SetNestedSlice(root.Object, []any{}, "spec", "participants")
				update(root)
			case "participant-owner":
				p.SetOwnerReferences(nil)
				update(p)
			case "network-uid":
				_ = unstructured.SetNestedField(p.Object, "replacement", "spec", "networkUID")
				update(p)
			case "missing-runtime":
				unstructured.RemoveNestedField(p.Object, "status", "runtime")
				update(p)
			case "control-role":
				pod.Labels["network.stacks.org/role"] = "control"
				update(pod)
			case "replaced-pod":
				pod.UID = "replacement"
				update(pod)
			case "duplicate-pod":
				snapshot := pod.DeepCopy()
				snapshot.Name = "other"
				snapshot.UID = "other"
				snapshot.ResourceVersion = ""
				if err := c.Create(ctx, snapshot); err != nil {
					t.Fatal(err)
				}
			}
			identity, err := ResolveActor(ctx, c, "test", "bitcoin")
			if mode == "valid" {
				if err != nil || identity.ParticipantUID != p.GetUID() || identity.NetworkUID != root.GetUID() ||
					identity.PodUID != pod.UID {
					t.Fatalf("current identity failed: %+v %v", identity, err)
				}
			} else if err == nil {
				t.Fatal("invalid actor identity accepted")
			}
		})
	}
}

func TestBindActorsRejectsSameParticipant(t *testing.T) {
	c, _, _, _ := actorFixture(t)
	fault := &unstructured.Unstructured{
		Object: map[string]any{"metadata": map[string]any{"namespace": "test"}, "spec": map[string]any{}},
	}
	if err := BindActors(context.Background(), c, fault, "bitcoin", "bitcoin"); err == nil {
		t.Fatal("same participant selected twice")
	}
}

func TestBindActorsUsesCurrentUIDsOnBothSelectors(t *testing.T) {
	c, root, p, pod := actorFixture(t)
	ctx := context.Background()
	targetUID := "33333333-3333-3333-3333-333333333333"
	participants, _, _ := unstructured.NestedSlice(root.Object, "spec", "participants")
	participants = append(participants, map[string]any{"name": "bitcoin-2", "kind": "BitcoinNode"})
	_ = unstructured.SetNestedSlice(root.Object, participants, "spec", "participants")
	identities, _, _ := unstructured.NestedSlice(root.Object, "status", "identities")
	identities = append(identities, map[string]any{"name": "bitcoin-2", "uid": targetUID})
	_ = unstructured.SetNestedSlice(root.Object, identities, "status", "identities")
	if err := c.Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	target := p.DeepCopy()
	target.SetName("target-participant")
	target.SetUID(types.UID(targetUID))
	target.SetResourceVersion("")
	_ = unstructured.SetNestedField(target.Object, "bitcoin-2", "spec", "participantName")
	_ = unstructured.SetNestedField(target.Object, "target-0", "status", "runtime", "podRef", "name")
	_ = unstructured.SetNestedField(target.Object, "target-pod-uid", "status", "runtime", "podRef", "uid")
	if err := c.Create(ctx, target); err != nil {
		t.Fatal(err)
	}
	targetPod := pod.DeepCopy()
	targetPod.Name = "target-0"
	targetPod.UID = "target-pod-uid"
	targetPod.ResourceVersion = ""
	targetPod.Labels["network.stacks.org/participant-uid"] = targetUID
	targetPod.Labels["network.stacks.org/actor"] = "bitcoin-2"
	if err := c.Create(ctx, targetPod); err != nil {
		t.Fatal(err)
	}
	fault := &unstructured.Unstructured{
		Object: map[string]any{"metadata": map[string]any{"namespace": "test"}, "spec": map[string]any{}},
	}
	if err := BindActors(ctx, c, fault, "bitcoin", "bitcoin-2"); err != nil {
		t.Fatal(err)
	}
	for i, uid := range []string{string(p.GetUID()), targetUID} {
		path := []string{"spec", "selector", "labelSelectors"}
		if i == 1 {
			path = []string{"spec", "target", "selector", "labelSelectors"}
		}
		labels, _, _ := unstructured.NestedStringMap(fault.Object, path...)
		if len(labels) != 5 || labels["network.stacks.org/participant-uid"] != uid ||
			labels["network.stacks.org/network-uid"] != string(root.GetUID()) ||
			labels["network.stacks.org/role"] != "actor" {
			t.Fatal("selector did not use exact current public identities")
		}
	}
}
