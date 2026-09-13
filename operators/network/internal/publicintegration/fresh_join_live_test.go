//go:build live

package publicintegration

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// storageInventory distinguishes new provisioning from reuse of existing storage.
type storageInventory struct {
	claims, volumes map[types.UID]bool
}

// captureStorageInventory records existing storage identities before selecting the new actor.
func (h *harness) captureStorageInventory(ctx context.Context) (storageInventory, error) {
	out := storageInventory{claims: map[types.UID]bool{}, volumes: map[types.UID]bool{}}
	var claims corev1.PersistentVolumeClaimList
	if err := h.c.List(ctx, &claims, client.InNamespace(h.config.namespace)); err != nil {
		return out, err
	}
	var volumes corev1.PersistentVolumeList
	if err := h.c.List(ctx, &volumes); err != nil {
		return out, err
	}
	for _, claim := range claims.Items {
		out.claims[claim.UID] = true
	}
	for _, volume := range volumes.Items {
		out.volumes[volume.UID] = true
	}
	return out, nil
}

// freshStorage rejects old claims/volumes, clones, snapshots and mismatched bindings.
func freshStorage(before storageInventory, claim *corev1.PersistentVolumeClaim, volume *corev1.PersistentVolume) bool {
	return claim.UID != "" && volume.UID != "" && !before.claims[claim.UID] && !before.volumes[volume.UID] &&
		claim.DeletionTimestamp == nil &&
		volume.DeletionTimestamp == nil &&
		claim.Spec.DataSource == nil &&
		claim.Spec.DataSourceRef == nil &&
		claim.Status.Phase == corev1.ClaimBound &&
		volume.Status.Phase == corev1.VolumeBound &&
		claim.Spec.VolumeName == volume.Name &&
		volume.Spec.ClaimRef != nil &&
		volume.Spec.ClaimRef.UID == claim.UID &&
		volume.Spec.ClaimRef.Name == claim.Name &&
		volume.Spec.ClaimRef.Namespace == claim.Namespace
}

// followerOnCohortTip requires a fresh PoX-5 observation matching an independently running cohort node.
func followerOnCohortTip(s snapshot, name string, minimum uint64) bool {
	follower, ready := readyFollower(s, name, minimum)
	if !ready || follower.Status.Runtime.Protocol.PoXContract != "ST000000000000000000002AMW42H.pox-5" {
		return false
	}
	for _, p := range s.Participants {
		if p.Name == name || p.Kind != "StacksNode" {
			continue
		}
		peer, current := readyFollower(s, p.Name, minimum)
		if current && peer.Identity.UID != follower.Identity.UID &&
			peer.Status.Runtime.PodRef.UID != follower.Status.Runtime.PodRef.UID &&
			peer.Status.Runtime.Protocol.IndexBlockID == follower.Status.Runtime.Protocol.IndexBlockID {
			return true
		}
	}
	return false
}

// qualifyFreshFollower proves new storage, post-join progress and convergence without process replacement.
func (h *harness) qualifyFreshFollower(
	ctx context.Context,
	joined snapshot,
	name string,
	original actorEpoch,
	guard actorGuard,
	before storageInventory,
) (snapshot, error) {
	var claim corev1.PersistentVolumeClaim
	if err := h.c.Get(
		ctx,
		client.ObjectKey{Namespace: h.config.namespace, Name: original.Storage.Claim.Name},
		&claim,
	); err != nil {
		return joined, err
	}
	var volume corev1.PersistentVolume
	if err := h.c.Get(ctx, client.ObjectKey{Name: claim.Spec.VolumeName}, &volume); err != nil {
		return joined, err
	}
	if claim.UID != original.Storage.Claim.UID || !freshStorage(before, &claim, &volume) {
		return joined, fmt.Errorf("fresh follower did not receive newly provisioned storage without a data source")
	}
	if err := h.event(
		"fresh-follower-storage",
		map[string]any{
			"participant": name,
			"claim":       objectIdentity(&claim),
			"volume":      objectIdentity(&volume),
			"dataSource":  "none",
		},
	); err != nil {
		return joined, err
	}
	if _, err := h.awaitProgress(ctx, name+"-network-progress", joined); err != nil {
		return joined, err
	}
	return h.wait(
		ctx,
		name+"-canonical-progress",
		h.config.progressTimeout,
		true,
		func(s snapshot) (bool, error) {
			p, ready := readyFollower(s, name, original.Height)
			if !ready {
				return false, nil
			}
			if p.Identity.UID != original.Participant.UID ||
				p.Status.Runtime.PodRef.UID != original.Runtime.PodRef.UID ||
				p.Status.Runtime.ContainerID != original.Runtime.ContainerID {
				return false, fmt.Errorf("fresh follower replaced its process during catch-up qualification")
			}
			if err := h.checkActorGuard(ctx, guard, s); err != nil {
				return false, err
			}
			return followerOnCohortTip(s, name, original.Height), nil
		},
	)
}
