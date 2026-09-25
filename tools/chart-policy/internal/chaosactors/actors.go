// Package chaosactors resolves current network actor identities for native fault fixtures.
package chaosactors

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ActorIdentity binds a native selector to one current replacement actor Pod.
type ActorIdentity struct {
	// NetworkName and NetworkUID identify the current environment.
	NetworkName string
	NetworkUID  types.UID
	// Name and ParticipantUID identify the selected logical actor instance.
	Name           string
	ParticipantUID types.UID
	// PodName and PodUID retain the current runtime observation.
	PodName string
	PodUID  types.UID
}

// Labels returns a selector scoped to the current actor participant.
func (a ActorIdentity) Labels() map[string]string {
	return map[string]string{
		"network.stacks.org/network":         a.NetworkName,
		"network.stacks.org/network-uid":     string(a.NetworkUID),
		"network.stacks.org/participant-uid": string(a.ParticipantUID),
		"network.stacks.org/actor":           a.Name,
		"network.stacks.org/role":            "actor",
	}
}

// ResolveActor reads current public identities and validates the exact observed actor Pod.
// Legacy fixtures have no replacement participant identity and are explicitly incompatible.
func ResolveActor(ctx context.Context, reader client.Reader, namespace, actor string) (ActorIdentity, error) {
	var result ActorIdentity
	root := &unstructured.Unstructured{}
	root.SetGroupVersionKind(
		schema.GroupVersionKind{Group: "network.stacks.org", Version: "v1alpha2", Kind: "StacksNetwork"},
	)
	if err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "network"}, root); err != nil {
		return result, fmt.Errorf(
			"current v1alpha2 network required; legacy live fixtures are incompatible with UID-scoped selector: %w",
			err,
		)
	}
	if root.GetUID() == "" || root.GetDeletionTimestamp() != nil {
		return result, fmt.Errorf("network identity unavailable")
	}
	selectedKind := ""
	participants, _, _ := unstructured.NestedSlice(root.Object, "spec", "participants")
	for _, raw := range participants {
		p, _ := raw.(map[string]any)
		if p["name"] == actor &&
			(p["kind"] == "BitcoinNode" || p["kind"] == "StacksNode" || p["kind"] == "StacksSigner") {
			selectedKind, _ = p["kind"].(string)
		}
	}
	if selectedKind == "" {
		return result, fmt.Errorf("actor %s is not currently selected", actor)
	}
	identities, _, _ := unstructured.NestedSlice(root.Object, "status", "identities")
	uid := ""
	for _, raw := range identities {
		id, _ := raw.(map[string]any)
		if id["name"] == actor && id["removing"] != true {
			uid, _ = id["uid"].(string)
		}
	}
	if uid == "" {
		return result, fmt.Errorf("actor participant UID unavailable")
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(
		schema.GroupVersionKind{Group: "network.stacks.org", Version: "v1alpha2", Kind: "StacksNetworkParticipantList"},
	)
	if err := reader.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return result, err
	}
	var participant *unstructured.Unstructured
	for i := range list.Items {
		p := &list.Items[i]
		if string(p.GetUID()) == uid {
			participant = p
		}
	}
	if participant == nil || participant.GetDeletionTimestamp() != nil || !metav1.IsControlledBy(participant, root) {
		return result, fmt.Errorf("current actor participant ownership unavailable")
	}
	networkUID, _, _ := unstructured.NestedString(participant.Object, "spec", "networkUID")
	logicalName, _, _ := unstructured.NestedString(participant.Object, "spec", "participantName")
	kind, _, _ := unstructured.NestedString(participant.Object, "spec", "kind")
	podName, _, _ := unstructured.NestedString(participant.Object, "status", "runtime", "podRef", "name")
	podUID, _, _ := unstructured.NestedString(participant.Object, "status", "runtime", "podRef", "uid")
	if networkUID != string(root.GetUID()) || logicalName != actor || kind != selectedKind || podName == "" ||
		podUID == "" {
		return result, fmt.Errorf("actor runtime identity unavailable")
	}
	result = ActorIdentity{
		NetworkName:    root.GetName(),
		NetworkUID:     root.GetUID(),
		Name:           actor,
		ParticipantUID: participant.GetUID(),
		PodName:        podName,
		PodUID:         types.UID(podUID),
	}
	var pods corev1.PodList
	if err := reader.List(
		ctx,
		&pods,
		client.InNamespace(namespace),
		client.MatchingLabels(result.Labels()),
	); err != nil {
		return ActorIdentity{}, err
	}
	if len(pods.Items) != 1 || pods.Items[0].Name != podName || string(pods.Items[0].UID) != podUID ||
		pods.Items[0].DeletionTimestamp != nil ||
		pods.Items[0].Status.Phase != corev1.PodRunning {
		return ActorIdentity{}, fmt.Errorf("exact actor selector must resolve one current running Pod")
	}
	return result, nil
}

// BindActors replaces example placeholders using fresh identities on both sides.
func BindActors(
	ctx context.Context,
	reader client.Reader,
	fault *unstructured.Unstructured,
	source, target string,
) error {
	a, err := ResolveActor(ctx, reader, fault.GetNamespace(), source)
	if err != nil {
		return err
	}
	b, err := ResolveActor(ctx, reader, fault.GetNamespace(), target)
	if err != nil {
		return err
	}
	if a.NetworkUID != b.NetworkUID || a.ParticipantUID == b.ParticipantUID || a.Name == b.Name {
		return fmt.Errorf("fault requires distinct actors in one current network")
	}
	for i, actor := range []ActorIdentity{a, b} {
		path := []string{"spec", "selector"}
		if i == 1 {
			path = []string{"spec", "target", "selector"}
		}
		if err := unstructured.SetNestedStringMap(
			fault.Object,
			actor.Labels(),
			append(path, "labelSelectors")...); err != nil {
			return err
		}
		if err := unstructured.SetNestedStringSlice(
			fault.Object,
			[]string{fault.GetNamespace()},
			append(path, "namespaces")...); err != nil {
			return err
		}
	}
	labels := fault.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels["network.stacks.org/network"] = a.NetworkName
	labels["network.stacks.org/network-uid"] = string(a.NetworkUID)
	fault.SetLabels(labels)
	return nil
}
