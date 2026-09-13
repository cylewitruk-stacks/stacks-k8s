//go:build live

package publicintegration

import (
	"context"
	"fmt"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// sameActorStorageAcrossImageRoll requires a distinct image/process over the same participant and persistent data.
func sameActorStorageAcrossImageRoll(old, next actorEpoch) error {
	if old.Participant.UID == "" || next.Participant.UID != old.Participant.UID || old.Runtime.PodRef == nil || next.Runtime.PodRef == nil || next.Runtime.PodRef.UID == "" || old.Runtime.PodRef.UID == next.Runtime.PodRef.UID || old.Runtime.ImageID == "" || next.Runtime.ImageID == "" || old.Runtime.ImageID == next.Runtime.ImageID || old.Storage.Claim.UID == "" || next.Storage.Claim.UID != old.Storage.Claim.UID || old.Storage.Volume == "" || next.Storage.Volume != old.Storage.Volume {
		return fmt.Errorf("image roll did not preserve participant/storage with a distinct runtime image")
	}
	return nil
}

// qualifyActorImage changes one late follower's image through the public root override.
func (h *harness) qualifyActorImage(ctx context.Context, name, image string, before snapshot, old actorEpoch, guard actorGuard) (snapshot, actorEpoch, error) {
	if image == "" || image == h.config.stacksImage {
		return before, old, fmt.Errorf("mixed-image qualification requires a distinct preloaded image")
	}
	if err := h.changeActors(ctx, func(root *api.StacksNetwork) error {
		allocated := false
		for _, id := range root.Status.Identities {
			if id.Name == name && id.UID == old.Participant.UID && !id.Removing {
				allocated = true
			}
		}
		if !allocated {
			return fmt.Errorf("image-roll participant identity changed")
		}
		for i := range root.Spec.Participants {
			p := &root.Spec.Participants[i]
			if p.Name != name {
				continue
			}
			if p.Kind != "StacksNode" || p.Definition.Ref == nil || p.Definition.Ref.Name != "follower-01" || p.Overrides == nil || p.Overrides.StacksNode == nil {
				return fmt.Errorf("image-roll declaration changed")
			}
			p.Overrides.StacksNode.Image = ptr.To(image)
			return nil
		}
		return fmt.Errorf("image-roll declaration unavailable")
	}); err != nil {
		return before, old, err
	}
	var next actorEpoch
	observed, err := h.wait(ctx, "actor-image-rolled", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		p, ready := readyFollower(s, name, old.Height)
		if !ready || p.Status.Admission == nil || p.Status.Admission.Configuration.StacksNode == nil || ptr.Deref(p.Status.Admission.Configuration.StacksNode.Image, "") != image || p.Status.Runtime.PodRef.UID == old.Runtime.PodRef.UID {
			return false, nil
		}
		if err := h.checkActorGuard(ctx, guard, s); err != nil {
			return false, err
		}
		var pod corev1.Pod
		if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: p.Status.Runtime.PodRef.Name}, &pod); err != nil {
			return false, err
		}
		actual := false
		for _, c := range pod.Spec.Containers {
			if c.Name == "stacks-node" && c.Image == image {
				actual = true
			}
		}
		if !actual {
			return false, fmt.Errorf("rolled actor does not use the requested image")
		}
		storage, err := h.actorClaim(ctx, p.Status.Runtime)
		if err != nil {
			return false, err
		}
		next = actorEpoch{Participant: p.Identity, Runtime: *p.Status.Runtime.DeepCopy(), Storage: storage, Height: p.Status.Runtime.Protocol.StacksHeight}
		if err = sameActorStorageAcrossImageRoll(old, next); err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return observed, old, err
	}
	if err = h.recordActor("actor-image-rolled", next); err != nil {
		return observed, next, err
	}
	if err = h.event("mixed-actor-image", map[string]any{"requestedImage": image, "before": old, "after": next}); err != nil {
		return observed, next, err
	}
	return observed, next, nil
}
