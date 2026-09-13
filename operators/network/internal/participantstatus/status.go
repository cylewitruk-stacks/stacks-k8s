// Package participantstatus publishes disjoint status fields on generated participants.
package participantstatus

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/csaupgrade"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AggregateManager owns complete admission and aggregate resolution conditions.
const AggregateManager = "stacks-network-aggregate"

// Apply writes only the supplied owned fields. Callers must not pass fetched whole status.
// UID and resourceVersion preserve identity and optimistic concurrency with current intent.
// The response refreshes the caller's object for subsequent writes in the same reconciliation.
func Apply(
	ctx context.Context,
	c client.Client,
	p *api.StacksNetworkParticipant,
	owned api.ParticipantStatus,
	manager string,
) error {
	if p.UID == "" || p.ResourceVersion == "" || manager == "" {
		return fmt.Errorf("participant status requires persisted identity, revision and field manager")
	}
	// API decoding during migration must not mutate an aliased candidate policy.
	owned = *owned.DeepCopy()
	if manager == AggregateManager {
		if err := migrate(ctx, c, p); err != nil {
			return err
		}
	}
	patch := &api.StacksNetworkParticipant{
		TypeMeta: metav1.TypeMeta{APIVersion: api.GroupVersion.String(), Kind: api.KindStacksNetworkParticipant},
		ObjectMeta: metav1.ObjectMeta{
			Name:            p.Name,
			Namespace:       p.Namespace,
			UID:             p.UID,
			ResourceVersion: p.ResourceVersion,
		},
		Status: owned,
	}
	if err := c.Status().
		//nolint:staticcheck // Typed minimal SSA preserves the response; generated apply configurations are not available.
		Patch(ctx, patch, client.Apply, client.FieldOwner(manager), client.ForceOwnership); err != nil {
		return err
	}
	*p = *patch
	return nil
}

// Managed reports whether this status manager has established ownership on the server.
func Managed(p *api.StacksNetworkParticipant, manager string) bool {
	for _, fields := range p.ManagedFields {
		if fields.Manager == manager && fields.Subresource == "status" &&
			fields.Operation == metav1.ManagedFieldsOperationApply {
			return true
		}
	}
	return false
}

// NeedsMigration identifies preview status Update owners whose fields are entirely aggregate-owned.
func NeedsMigration(p *api.StacksNetworkParticipant) bool { return len(legacyManagers(p)) > 0 }

// legacyManagers excludes domain/external ownership and all spec managers.
func legacyManagers(p *api.StacksNetworkParticipant) sets.Set[string] {
	names := sets.New[string]()
	for _, entry := range p.ManagedFields {
		if entry.Manager != "foundation" || entry.Subresource != "status" ||
			entry.Operation != metav1.ManagedFieldsOperationUpdate {
			continue
		}
		// csaupgrade acts on every matching manager/subresource entry. One unsafe
		// entry vetoes conversion of that manager, including across API versions.
		if entry.FieldsType != managedFieldsFormat || entry.FieldsV1 == nil ||
			entry.APIVersion != api.GroupVersion.String() {
			return sets.New[string]()
		}
		var fields map[string]map[string]json.RawMessage
		if json.Unmarshal(entry.FieldsV1.GetRawBytes(), &fields) != nil || len(fields) != 1 {
			return sets.New[string]()
		}
		status, ok := fields["f:status"]
		if !ok || len(status) == 0 {
			return sets.New[string]()
		}
		for key, raw := range status {
			var children map[string]json.RawMessage
			if json.Unmarshal(raw, &children) != nil || children == nil {
				return sets.New[string]()
			}
			switch key {
			case ".", "f:admission":
			case "f:conditions":
				for condition := range children {
					if condition != "." && condition != `k:{"type":"Resolved"}` &&
						condition != `k:{"type":"PolicyDeferred"}` &&
						condition != `k:{"type":"WorkloadReady"}` {
						return sets.New[string]()
					}
				}
			default:
				return sets.New[string]()
			}
		}
		names.Insert(entry.Manager)
	}
	return names
}

// migrate uses the upstream Update-to-Apply conversion without a gap in status values.
func migrate(ctx context.Context, c client.Client, p *api.StacksNetworkParticipant) error {
	names := legacyManagers(p)
	if len(names) == 0 {
		return nil
	}
	data, err := csaupgrade.UpgradeManagedFieldsPatch(p, names, AggregateManager, csaupgrade.Subresource("status"))
	if err != nil || len(data) == 0 {
		return err
	}
	var operations []map[string]any
	if err := json.Unmarshal(data, &operations); err != nil {
		return err
	}
	operations = append(
		[]map[string]any{{"op": "test", "path": "/metadata/uid", "value": string(p.UID)}},
		operations...)
	data, err = json.Marshal(operations)
	if err != nil {
		return err
	}
	return c.Patch(ctx, p, client.RawPatch(types.JSONPatchType, data))
}

// OwnsCondition reports remaining ownership that must be relinquished during a domain handoff.
func OwnsCondition(p *api.StacksNetworkParticipant, manager, conditionType string) bool {
	key, _ := json.Marshal(map[string]string{"type": conditionType})
	for _, entry := range p.ManagedFields {
		if entry.Manager != manager || entry.Subresource != "status" || entry.FieldsV1 == nil {
			continue
		}
		var fields map[string]map[string]map[string]json.RawMessage
		if json.Unmarshal(entry.FieldsV1.GetRawBytes(), &fields) != nil {
			continue
		}
		if _, ok := fields["f:status"]["f:conditions"]["k:"+string(key)]; ok {
			return true
		}
	}
	return false
}

// managedFieldsFormat identifies a structural input name at this reflection/metadata boundary.
const managedFieldsFormat = "FieldsV1"
