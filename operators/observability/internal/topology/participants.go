package topology

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/canonical"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// VersionV1Alpha2 selects generated participant runtime identities.
	VersionV1Alpha2         = "v1alpha2"
	policyAnnotation        = api.AnnotationPolicyDigest
	configurationAnnotation = api.AnnotationConfigurationDigest
)

// directRead records objects for a final consistency check; Secret reads are never used.
type directRead struct {
	reader  client.Reader
	objects []client.Object
}

// get records one direct object read for the stability check.
func (d *directRead) get(ctx context.Context, key client.ObjectKey, object client.Object) error {
	if err := d.reader.Get(ctx, key, object); err != nil {
		if apierrors.IsNotFound(err) {
			return &NotReadyError{Reason: fmt.Sprintf("identity resource %s is absent", key)}
		}
		return err
	}
	d.objects = append(d.objects, object.DeepCopyObject().(client.Object))
	return nil
}

// stable rejects changes to the collected identity inputs, independently of progress heartbeats.
func (d *directRead) stable(ctx context.Context) error {
	for _, before := range d.objects {
		// REST decoding must not retain omitted fields or deleted map entries.
		after := reflect.New(reflect.TypeOf(before).Elem()).Interface().(client.Object)
		after.GetObjectKind().SetGroupVersionKind(before.GetObjectKind().GroupVersionKind())
		if err := d.reader.Get(ctx, client.ObjectKeyFromObject(before), after); err != nil {
			return &InconclusiveError{Reason: fmt.Sprintf("identity resource changed during observation: %v", err)}
		}
		left, err := identityInputs(before)
		if err != nil {
			return err
		}
		right, err := identityInputs(after)
		if err != nil {
			return err
		}
		if after.GetDeletionTimestamp() != nil || !reflect.DeepEqual(left, right) {
			return &InconclusiveError{Reason: fmt.Sprintf("identity resource %s changed during observation", client.ObjectKeyFromObject(before))}
		}
	}
	return nil
}

// identityInputs preserves declarations, ownership and runtime facts without unrelated status writers.
func identityInputs(object client.Object) (map[string]any, error) {
	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object.DeepCopyObject())
	if err != nil {
		return nil, fmt.Errorf("decode observed identity: %w", err)
	}
	unstructured.RemoveNestedField(value, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(value, "metadata", "managedFields")
	var fields []string
	switch object.(type) {
	case *api.StacksNetwork:
		fields = []string{"identities"}
	case *api.StacksNetworkParticipant:
		fields = []string{"admission", "runtime"}
	case *unstructured.Unstructured:
		if object.GetObjectKind().GroupVersionKind() == api.GroupVersion.WithKind("StacksNetworkParticipant") {
			fields = []string{"admission", "runtime"}
		}
	}
	if fields != nil {
		status := map[string]any{}
		for _, field := range fields {
			input, found, err := unstructured.NestedFieldCopy(value, "status", field)
			if err != nil {
				return nil, err
			}
			if found {
				status[field] = input
			}
		}
		value["status"] = status
		// Native chain telemetry does not contribute to an actor identity snapshot.
		unstructured.RemoveNestedField(value, "status", "runtime", "protocol")
	}
	return value, nil
}

// observeParticipants resolves the selected actor set from the exact allocation ledger.
func (r Reader) observeParticipants(ctx context.Context, namespace, name, expected string) (Snapshot, error) {
	reads := directRead{reader: r.APIReader}
	root := &api.StacksNetwork{}
	if err := reads.get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, root); err != nil {
		return Snapshot{}, err
	}
	if root.UID == "" || root.DeletionTimestamp != nil {
		return Snapshot{}, &NotReadyError{Reason: "network identity is unavailable or deleting"}
	}
	participants := &unstructured.UnstructuredList{}
	participants.SetGroupVersionKind(schema.GroupVersionKind{Group: api.GroupVersion.Group, Version: VersionV1Alpha2, Kind: "StacksNetworkParticipantList"})
	if err := r.APIReader.List(ctx, participants, client.InNamespace(namespace), client.Limit(1001)); err != nil {
		return Snapshot{}, err
	}
	if participants.GetContinue() != "" || len(participants.Items) > 1000 {
		return Snapshot{}, &InconclusiveError{Reason: "participant view exceeds the bounded observation"}
	}
	byUID := make(map[types.UID]*unstructured.Unstructured, len(participants.Items))
	for i := range participants.Items {
		p := &participants.Items[i]
		byUID[p.GetUID()] = p
	}
	identities := make(map[string]api.InstanceIdentity, len(root.Status.Identities))
	for _, id := range root.Status.Identities {
		if _, duplicate := identities[id.Name]; duplicate {
			return Snapshot{}, &InconclusiveError{Reason: "duplicate participant allocation identity"}
		}
		identities[id.Name] = id
	}
	actors := []observation.ObservedActorIdentity{}
	seen := map[string]bool{}
	for _, selected := range root.Spec.Participants {
		if actorContainerV2(selected.Kind) == "" {
			continue
		}
		if seen[selected.Name] {
			return Snapshot{}, &InconclusiveError{Reason: "duplicate selected actor"}
		}
		seen[selected.Name] = true
		id, ok := identities[selected.Name]
		if !ok || id.UID == "" {
			return Snapshot{}, &NotReadyError{Reason: "selected actor has no allocated identity"}
		}
		if id.Removing {
			return Snapshot{}, &InconclusiveError{Reason: "selected actor identity is being removed"}
		}
		raw := byUID[id.UID]
		if raw == nil {
			return Snapshot{}, &NotReadyError{Reason: "selected actor participant is absent"}
		}
		p, err := decodeParticipant(raw)
		if err != nil {
			if IsNotReady(err) {
				return Snapshot{}, err
			}
			return Snapshot{}, &InconclusiveError{Reason: err.Error()}
		}
		if p.Spec.NetworkUID != root.UID || p.Spec.ParticipantName != selected.Name || p.Spec.Kind != selected.Kind || !exactOwner(p, api.GroupVersion.String(), "StacksNetwork", root.Name, root.UID) || p.DeletionTimestamp != nil {
			return Snapshot{}, &InconclusiveError{Reason: "participant owner or allocation identity differs"}
		}
		reads.objects = append(reads.objects, raw.DeepCopy())
		actor, err := observeActor(ctx, &reads, p)
		if err != nil {
			return Snapshot{}, err
		}
		actors = append(actors, actor)
	}
	if len(actors) == 0 {
		return Snapshot{}, &NotReadyError{Reason: "network selects no observable actors"}
	}
	sort.Slice(actors, func(i, j int) bool {
		if actors[i].Kind != actors[j].Kind {
			return actors[i].Kind < actors[j].Kind
		}
		return actors[i].Name < actors[j].Name
	})
	binding := observation.NetworkBinding{Name: root.Name, UID: root.UID, ObservedGeneration: root.Generation, NetworkAPIVersion: api.GroupVersion.String()}
	digest, err := canonical.Digest(struct {
		Schema  string                              `json:"schema"`
		Network observation.NetworkBinding          `json:"network"`
		Actors  []observation.ObservedActorIdentity `json:"actors"`
	}{"observation.stacks.org/actor-snapshot/v1", binding, actors})
	if err != nil {
		return Snapshot{}, err
	}
	binding.SnapshotDigest = digest
	if expected != "" && digest != expected {
		return Snapshot{}, &InconclusiveError{Reason: "expected observation snapshot digest differs"}
	}
	if err := reads.stable(ctx); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Binding: binding, Actors: actors}, nil
}

// decodeParticipant rejects unknown configuration fields before checking the complete policy digest.
func decodeParticipant(raw *unstructured.Unstructured) (*api.StacksNetworkParticipant, error) {
	value, found, err := unstructured.NestedMap(raw.Object, "status", "admission", "configuration")
	if err != nil || !found {
		return nil, &NotReadyError{Reason: "participant has no complete admission"}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var configuration api.Configuration
	if err := decoder.Decode(&configuration); err != nil {
		return nil, fmt.Errorf("unsupported admitted policy: %w", err)
	}
	full, err := raw.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var p api.StacksNetworkParticipant
	if err := json.Unmarshal(full, &p); err != nil {
		return nil, err
	}
	encoded, err = json.Marshal(configuration)
	if err != nil {
		return nil, err
	}
	if p.Status.Admission == nil || bytesDigest(encoded) != p.Status.Admission.PolicyDigest {
		return nil, fmt.Errorf("complete admitted policy digest differs")
	}
	return &p, nil
}

// bytesDigest hashes the public typed JSON wire encoding.
func bytesDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// exactOwner verifies the complete controller owner reference.
func exactOwner(object metav1.Object, version, kind, name string, uid types.UID) bool {
	owner := metav1.GetControllerOf(object)
	return owner != nil && uid != "" && owner.APIVersion == version && owner.Kind == kind && owner.Name == name && owner.UID == uid
}

// actorContainerV2 maps the closed actor-kind union to its native container.
func actorContainerV2(kind api.ParticipantKind) string {
	switch kind {
	case api.ParticipantBitcoinNode:
		return "bitcoin"
	case api.ParticipantStacksNode:
		return "stacks-node"
	case api.ParticipantStacksSigner:
		return "stacks-signer"
	}
	return ""
}

// validBinding requires a complete object identity of the expected kind.
func validBinding(binding *common.Binding, kind string) bool {
	return binding != nil && binding.Kind == kind && binding.Name != "" && binding.UID != ""
}
