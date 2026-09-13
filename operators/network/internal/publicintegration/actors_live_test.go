//go:build live

package publicintegration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// actorGuard retains immutable genesis and unrelated Pod/process identities.
type actorGuard struct {
	genesis api.StacksGenesis
	binding common.Binding
	pods    map[string]actorPodEpoch
}

// actorPodEpoch identifies actual processes without collecting Pod configuration.
type actorPodEpoch struct {
	UID        types.UID         `json:"uid"`
	Containers map[string]string `json:"containers"`
}

// actorStorage records the actual claim and persistent-volume identity mounted by an actor.
type actorStorage struct {
	Claim  identity `json:"claim"`
	Volume string   `json:"volume"`
}

// actorEpoch retains one late actor's public runtime and storage evidence.
type actorEpoch struct {
	Participant identity                     `json:"participant"`
	Runtime     api.ParticipantRuntimeStatus `json:"runtime"`
	Storage     actorStorage                 `json:"storage"`
	Height      uint64                       `json:"height"`
}

// lateFollower reuses a node definition and a recipient account with no existing peer role.
func lateFollower(name, image string, retain bool) api.Participant {
	return api.Participant{
		Name:       name,
		Kind:       "StacksNode",
		Definition: api.Definition{Ref: &common.NameRef{Name: "follower-01"}},
		Overrides: &api.Configuration{
			StacksNode: &stacks.StacksNodeSpec{
				ActorFields: common.ActorFields{
					Image:   ptr.To(image),
					Storage: &common.Storage{Size: ptr.To("1Gi"), RetainOnDelete: ptr.To(retain)},
				},
				BitcoinNodeRef:     &common.NameRef{Name: "btc-07"},
				IdentityAccountRef: &common.NameRef{Name: "traffic-recipient"},
				Mining:             &stacks.Mining{Enabled: ptr.To(false)},
			},
		},
	}
}

// podEpoch requires a current running process identity for each declared container.
func podEpoch(pod *corev1.Pod) (actorPodEpoch, error) {
	out := actorPodEpoch{UID: pod.UID, Containers: map[string]string{}}
	if pod.UID == "" || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return out, fmt.Errorf("actor qualification requires running Pods")
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.ContainerID == "" || status.State.Running == nil {
			return out, fmt.Errorf("Pod container process unavailable")
		}
		out.Containers[status.Name] = status.ContainerID
	}
	if len(out.Containers) != len(pod.Spec.Containers) || len(out.Containers) == 0 {
		return out, fmt.Errorf("incomplete Pod process observation")
	}
	return out, nil
}

// captureActorGuard includes every existing actor and bound management/control worker.
func (h *harness) captureActorGuard(ctx context.Context, s snapshot) (actorGuard, error) {
	guard := actorGuard{pods: map[string]actorPodEpoch{}}
	if s.Status.GenesisRef == nil || s.Status.GenesisRef.UID == "" {
		return guard, fmt.Errorf("frozen genesis unavailable")
	}
	guard.binding = *s.Status.GenesisRef
	if err := h.c.Get(
		ctx,
		client.ObjectKey{Namespace: h.config.namespace, Name: guard.binding.Name},
		&guard.genesis,
	); err != nil {
		return guard, err
	}
	if guard.genesis.UID != guard.binding.UID {
		return guard, fmt.Errorf("frozen genesis identity changed")
	}
	names := map[string]types.UID{}
	for name, worker := range workerIdentities(s) {
		names[name] = worker.UID
	}
	for _, p := range s.Participants {
		if p.Status.Runtime != nil && p.Status.Runtime.PodRef != nil {
			names[p.Status.Runtime.PodRef.Name] = p.Status.Runtime.PodRef.UID
		}
	}
	if len(names) == 0 {
		return guard, fmt.Errorf("no unrelated runtime Pods to qualify")
	}
	for name, uid := range names {
		var pod corev1.Pod
		if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: name}, &pod); err != nil {
			return guard, err
		}
		epoch, err := podEpoch(&pod)
		if err != nil {
			return guard, err
		}
		if epoch.UID != uid {
			return guard, fmt.Errorf("baseline Pod identity changed")
		}
		guard.pods[name] = epoch
	}
	return guard, nil
}

// checkActorGuard checks exact immutable genesis and unrelated process identity at each stage.
func (h *harness) checkActorGuard(ctx context.Context, guard actorGuard, s snapshot) error {
	if s.Status.GenesisRef == nil || *s.Status.GenesisRef != guard.binding {
		return fmt.Errorf("actor lifecycle changed frozen genesis binding")
	}
	var genesis api.StacksGenesis
	if err := h.c.Get(
		ctx,
		client.ObjectKey{Namespace: h.config.namespace, Name: guard.binding.Name},
		&genesis,
	); err != nil {
		return err
	}
	if genesis.UID != guard.genesis.UID || genesis.DeletionTimestamp != nil ||
		!reflect.DeepEqual(genesis.Spec, guard.genesis.Spec) {
		return fmt.Errorf("actor lifecycle changed frozen genesis")
	}
	for name, expected := range guard.pods {
		var pod corev1.Pod
		if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: name}, &pod); err != nil {
			return err
		}
		actual, err := podEpoch(&pod)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("unrelated Pod or container rolled: %s", name)
		}
	}
	return nil
}

// changeActors modifies only participants of the exact running root using optimistic concurrency.
func (h *harness) changeActors(ctx context.Context, change func(*api.StacksNetwork) error) error {
	for range 5 {
		var root api.StacksNetwork
		if err := h.c.Get(ctx, client.ObjectKey{
			Namespace: h.config.namespace,
			Name:      "network",
		}, &root); err != nil {
			return err
		}
		if root.UID != h.rootUID || root.DeletionTimestamp != nil || root.Spec.Operation != "Running" {
			return fmt.Errorf("actor edit requires the same running root")
		}
		base := root.DeepCopy()
		if err := change(&root); err != nil {
			return err
		}
		err := h.c.Patch(ctx, &root, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
		if apierrors.IsConflict(err) {
			continue
		}
		return err
	}
	return fmt.Errorf("actor declaration edits repeatedly conflicted")
}

// addLateFollower refuses reuse of any allocated logical participant name.
func (h *harness) addLateFollower(ctx context.Context, name string, retain bool) error {
	return h.changeActors(ctx, func(root *api.StacksNetwork) error {
		for _, id := range root.Status.Identities {
			if id.Name == name {
				return fmt.Errorf("late participant name was already allocated")
			}
		}
		for _, p := range root.Spec.Participants {
			if p.Name == name {
				return fmt.Errorf("late participant already selected")
			}
		}
		root.Spec.Participants = append(root.Spec.Participants, lateFollower(name, h.config.stacksImage, retain))
		return nil
	})
}

// controlLateFollower edits only the declared late participant, or removes it permanently.
func (h *harness) controlLateFollower(ctx context.Context, name string, suspended *bool) error {
	return h.changeActors(ctx, func(root *api.StacksNetwork) error {
		for i := range root.Spec.Participants {
			p := &root.Spec.Participants[i]
			if p.Name != name {
				continue
			}
			if p.Kind != "StacksNode" || p.Definition.Ref == nil || p.Definition.Ref.Name != "follower-01" {
				return fmt.Errorf("late follower declaration changed")
			}
			if suspended == nil {
				root.Spec.Participants = append(root.Spec.Participants[:i], root.Spec.Participants[i+1:]...)
			} else {
				p.Control = &api.Control{Suspended: ptr.To(*suspended)}
			}
			return nil
		}
		return fmt.Errorf("late follower declaration disappeared")
	})
}

// readyFollower requires current native synchronization tied to this Pod/config/genesis.
func readyFollower(s snapshot, name string, minimum uint64) (participantEvidence, bool) {
	for _, p := range s.Participants {
		if p.Name != name || p.Kind != "StacksNode" {
			continue
		}
		r := p.Status.Runtime
		c := meta.FindStatusCondition(p.Status.Conditions, "WorkloadReady")
		verified := meta.FindStatusCondition(p.Status.Conditions, "ConfigVerified")
		if verified == nil || verified.Status != metav1.ConditionTrue ||
			verified.ObservedGeneration != p.Identity.Generation {
			return p, false
		}
		if r == nil || r.Terminated || r.PodRef == nil || r.ConfigRef == nil ||
			r.ObservedGeneration != p.Identity.Generation ||
			c == nil ||
			c.Status != metav1.ConditionTrue ||
			c.ObservedGeneration != p.Identity.Generation ||
			r.Protocol == nil ||
			s.Status.ObservationPolicy == nil ||
			s.Status.GenesisRef == nil {
			return p, false
		}
		view := r.Protocol
		freshness := time.Duration(
			3*s.Status.ObservationPolicy.PollIntervalSeconds+s.Status.ObservationPolicy.RPCAllowanceSeconds,
		) * time.Second
		ready := view.Available && view.FullySynced && view.PodUID == r.PodRef.UID &&
			view.ContainerID == r.ContainerID &&
			view.ConfigurationDigest == r.ConfigurationDigest &&
			view.GenesisUID == s.Status.GenesisRef.UID &&
			view.IndexBlockID != "" &&
			view.StacksHeight > minimum &&
			!view.ObservedAt.IsZero() &&
			!view.ObservedAt.After(s.At) &&
			s.At.Sub(view.ObservedAt.Time) <= freshness
		return p, ready
	}
	return participantEvidence{}, false
}

// actorClaim reads the exact mounted claim; it never infers storage from an ordinal or account name.
func (h *harness) actorClaim(ctx context.Context, r *api.ParticipantRuntimeStatus) (actorStorage, error) {
	var storage actorStorage
	if r == nil || r.PodRef == nil {
		return storage, fmt.Errorf("actor Pod binding unavailable")
	}
	var pod corev1.Pod
	if err := h.c.Get(ctx, client.ObjectKey{
		Namespace: h.config.namespace,
		Name:      r.PodRef.Name,
	}, &pod); err != nil {
		return storage, err
	}
	if pod.UID != r.PodRef.UID || pod.DeletionTimestamp != nil {
		return storage, fmt.Errorf("actor Pod identity changed")
	}
	epoch, err := podEpoch(&pod)
	if err != nil {
		return storage, err
	}
	matchingProcess := false
	for _, id := range epoch.Containers {
		if id == r.ContainerID && id != "" {
			matchingProcess = true
		}
	}
	if !matchingProcess {
		return storage, fmt.Errorf("actor process changed before storage observation")
	}

	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		}
		if storage.Claim.UID != "" {
			return storage, fmt.Errorf("actor has multiple persistent claims")
		}
		var claim corev1.PersistentVolumeClaim
		if err := h.c.Get(
			ctx,
			client.ObjectKey{Namespace: h.config.namespace, Name: volume.PersistentVolumeClaim.ClaimName},
			&claim,
		); err != nil {
			return storage, err
		}
		if claim.UID == "" || claim.DeletionTimestamp != nil || claim.Status.Phase != corev1.ClaimBound ||
			claim.Spec.VolumeName == "" {
			return storage, fmt.Errorf("actor claim is not currently bound")
		}
		storage = actorStorage{Claim: objectIdentity(&claim), Volume: claim.Spec.VolumeName}
	}
	if storage.Claim.UID == "" {
		return storage, fmt.Errorf("actor qualification requires persistent storage")
	}
	return storage, nil
}

// recordActor stores public process and PVC identities without configuration contents.
func (h *harness) recordActor(stage string, epoch actorEpoch) error {
	data, err := json.MarshalIndent(epoch, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(h.evidence, stage+"-actor.json"), data, 0o600)
}

// waitFollower qualifies native progress and captures the current claim/process epoch.
func (h *harness) waitFollower(
	ctx context.Context,
	stage, name string,
	minimum uint64,
	guard actorGuard,
) (snapshot, actorEpoch, error) {
	var epoch actorEpoch
	observed, err := h.wait(ctx, stage, h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		p, ready := readyFollower(s, name, minimum)
		if !ready {
			return false, nil
		}
		if err := h.checkActorGuard(ctx, guard, s); err != nil {
			return false, err
		}
		storage, err := h.actorClaim(ctx, p.Status.Runtime)
		if err != nil {
			return false, err
		}
		epoch = actorEpoch{
			Participant: p.Identity,
			Runtime:     *p.Status.Runtime.DeepCopy(),
			Storage:     storage,
			Height:      p.Status.Runtime.Protocol.StacksHeight,
		}
		return true, nil
	})
	if err == nil {
		err = h.recordActor(stage, epoch)
	}
	return observed, epoch, err
}

// retainedClaim proves the removed actor's old claim remains a distinct resource.
func (h *harness) retainedClaim(ctx context.Context, old actorStorage) error {
	var claim corev1.PersistentVolumeClaim
	if err := h.c.Get(ctx, client.ObjectKey{
		Namespace: h.config.namespace,
		Name:      old.Claim.Name,
	}, &claim); err != nil {
		return err
	}
	if claim.UID != old.Claim.UID || claim.Spec.VolumeName != old.Volume || claim.DeletionTimestamp != nil {
		return fmt.Errorf("retained actor storage identity changed")
	}
	return nil
}

// removeFollower waits for retained allocation plus actual participant/workload deletion.
func (h *harness) removeFollower(
	ctx context.Context,
	name string,
	old actorEpoch,
	guard actorGuard,
	retain bool,
) (snapshot, error) {
	if err := h.controlLateFollower(ctx, name, nil); err != nil {
		return snapshot{}, err
	}
	return h.awaitActorRemoved(ctx, name, old, guard, retain)
}

// awaitActorRemoved checks actual runtime/storage disposal after a public declaration is removed.
func (h *harness) awaitActorRemoved(
	ctx context.Context,
	name string,
	old actorEpoch,
	guard actorGuard,
	retain bool,
) (snapshot, error) {
	return h.wait(ctx, "actor-removed-"+name, h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		retained := false
		for _, id := range s.Status.Identities {
			if id.Name == name && id.UID == old.Participant.UID && id.Removing {
				retained = true
			}
		}
		if !retained {
			return false, nil
		}
		var p api.StacksNetworkParticipant
		if err := h.c.Get(
			ctx,
			client.ObjectKey{Namespace: h.config.namespace, Name: old.Participant.Name},
			&p,
		); err == nil {
			if p.UID != old.Participant.UID {
				return false, fmt.Errorf("removed participant was replaced")
			}
			return false, nil
		} else if !apierrors.IsNotFound(
			err,
		) {
			return false, nil
		}
		var pod corev1.Pod
		if err := h.c.Get(
			ctx,
			client.ObjectKey{Namespace: h.config.namespace, Name: old.Runtime.PodRef.Name},
			&pod,
		); !apierrors.IsNotFound(
			err,
		) {
			return false, nil
		}
		for _, ref := range old.Runtime.WorkloadRefs {
			if ref.Kind == "StatefulSet" {
				var workload appsv1.StatefulSet
				if err := h.c.Get(
					ctx,
					client.ObjectKey{Namespace: h.config.namespace, Name: ref.Name},
					&workload,
				); !apierrors.IsNotFound(
					err,
				) {
					return false, nil
				}
			}
		}
		if retain {
			if err := h.retainedClaim(ctx, old.Storage); err != nil {
				return false, err
			}
		} else {
			var claim corev1.PersistentVolumeClaim
			if err := h.c.Get(
				ctx,
				client.ObjectKey{Namespace: h.config.namespace, Name: old.Storage.Claim.Name},
				&claim,
			); !apierrors.IsNotFound(
				err,
			) {
				return false, nil
			}
		}
		if err := h.checkActorGuard(ctx, guard, s); err != nil {
			return false, err
		}
		return true, nil
	})
}

// newStorageEpoch rejects a replacement participant that adopted the removed actor's data identity.
func newStorageEpoch(old, next actorEpoch) error {
	if old.Participant.UID == "" || next.Participant.UID == "" || old.Participant.UID == next.Participant.UID ||
		old.Runtime.PodRef == nil ||
		next.Runtime.PodRef == nil ||
		old.Runtime.PodRef.UID == next.Runtime.PodRef.UID ||
		old.Storage.Claim.UID == "" ||
		next.Storage.Claim.UID == "" ||
		old.Storage.Claim.UID == next.Storage.Claim.UID ||
		old.Storage.Claim.Name == next.Storage.Claim.Name ||
		old.Storage.Volume == "" ||
		next.Storage.Volume == "" ||
		old.Storage.Volume == next.Storage.Volume {
		return fmt.Errorf("new participant reused old actor/storage identity")
	}
	return nil
}

// qualifyActors exercises late-actor lifecycle and an optional explicitly selected image roll.
func (h *harness) qualifyActors(ctx context.Context, before snapshot) (snapshot, error) {
	for _, p := range before.Participants {
		if p.Kind == "StacksNode" && p.Status.Admission != nil && p.Status.Admission.Configuration.StacksNode != nil {
			account := p.Status.Admission.Configuration.StacksNode.IdentityAccountRef
			if account != nil && account.Name == "traffic-recipient" {
				return before, fmt.Errorf("qualification account already supplies a peer identity")
			}
		}
	}
	for _, wanted := range []struct {
		kind, name string
		object     client.Object
	}{
		{"StacksNode", "follower-01", &stacks.StacksNode{}},
		{"StacksAccount", "traffic-recipient", &stacks.StacksAccount{}},
	} {
		var uid types.UID
		for _, o := range h.declared.reusable {
			if o.GetKind() == wanted.kind && o.GetName() == wanted.name {
				uid = o.GetUID()
			}
		}
		if uid == "" {
			return before, fmt.Errorf("reusable %s declaration unavailable", wanted.kind)
		}
		if err := h.c.Get(
			ctx,
			client.ObjectKey{Namespace: h.config.namespace, Name: wanted.name},
			wanted.object,
		); err != nil {
			return before, err
		}
		if wanted.object.GetUID() != uid || wanted.object.GetDeletionTimestamp() != nil {
			return before, fmt.Errorf("reusable declaration identity changed")
		}
	}
	guard, err := h.captureActorGuard(ctx, before)
	if err != nil {
		return before, err
	}
	minimum := uint64(0)
	for _, p := range before.Participants {
		if p.Status.Runtime != nil && p.Status.Runtime.Protocol != nil &&
			p.Status.Runtime.Protocol.StacksHeight > minimum {
			minimum = p.Status.Runtime.Protocol.StacksHeight
		}
	}
	const first = "qualification-follower-a"
	const second = "qualification-follower-b"
	var storageBefore storageInventory
	if os.Getenv("STACKS_PUBLIC_FRESH_JOIN") == "1" {
		storageBefore, err = h.captureStorageInventory(ctx)
		if err != nil {
			return before, err
		}
	}
	if err := h.addLateFollower(ctx, first, true); err != nil {
		return before, err
	}
	joined, original, err := h.waitFollower(ctx, "actor-late-joined", first, minimum, guard)
	if err != nil {
		return joined, err
	}
	if os.Getenv("STACKS_PUBLIC_FRESH_JOIN") == "1" {
		return h.qualifyFreshFollower(ctx, joined, first, original, guard, storageBefore)
	}
	if image := os.Getenv("STACKS_PUBLIC_UPGRADE_IMAGE"); image != "" {
		joined, original, err = h.qualifyActorImage(ctx, first, image, joined, original, guard)
		if err != nil {
			return joined, err
		}
	}
	if err := h.controlLateFollower(ctx, first, ptr.To(true)); err != nil {
		return joined, err
	}
	suspended, err := h.wait(ctx, "actor-suspended", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		for _, p := range s.Participants {
			if p.Name == first {
				if p.Identity.UID != original.Participant.UID {
					return false, fmt.Errorf("suspension replaced participant")
				}
				c := meta.FindStatusCondition(p.Status.Conditions, "WorkloadReady")
				r := p.Status.Runtime
				if r == nil || !r.Terminated || r.PodRef == nil || r.PodRef.UID != original.Runtime.PodRef.UID ||
					r.ContainerID != original.Runtime.ContainerID ||
					r.ObservedGeneration != p.Identity.Generation ||
					c == nil ||
					c.ObservedGeneration != p.Identity.Generation ||
					c.Reason != "Suspended" ||
					c.Status != metav1.ConditionFalse {
					return false, nil
				}
				if err := h.retainedClaim(ctx, original.Storage); err != nil {
					return false, err
				}
				if err := h.checkActorGuard(ctx, guard, s); err != nil {
					return false, err
				}
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return suspended, err
	}
	if err := h.controlLateFollower(ctx, first, ptr.To(false)); err != nil {
		return suspended, err
	}
	resumed, current, err := h.waitFollower(ctx, "actor-resumed", first, original.Height, guard)
	if err != nil {
		return resumed, err
	}
	if current.Participant.UID != original.Participant.UID ||
		current.Runtime.PodRef.UID == original.Runtime.PodRef.UID ||
		current.Storage.Claim.UID != original.Storage.Claim.UID ||
		current.Storage.Volume != original.Storage.Volume ||
		!reflect.DeepEqual(current.Runtime.ConfigRef, original.Runtime.ConfigRef) {
		return resumed, fmt.Errorf("suspend/resume changed participant, config or storage identity, or reused old Pod")
	}
	removed, err := h.removeFollower(ctx, first, current, guard, true)
	if err != nil {
		return removed, err
	}
	if err := h.addLateFollower(ctx, second, false); err != nil {
		return removed, err
	}
	fresh, next, err := h.waitFollower(ctx, "actor-fresh-replacement", second, current.Height, guard)
	if err != nil {
		return fresh, err
	}
	if err := newStorageEpoch(current, next); err != nil {
		return fresh, err
	}
	if err := h.retainedClaim(ctx, current.Storage); err != nil {
		return fresh, err
	}
	removed, err = h.removeFollower(ctx, second, next, guard, false)
	if err != nil {
		return removed, err
	}
	settled, err := h.wait(
		ctx,
		"actors-disposal-ready",
		h.config.progressTimeout,
		true,
		func(s snapshot) (bool, error) {
			_, ready := progress(s)
			return ready && condition(s, "Operational", metav1.ConditionTrue), nil
		},
	)
	if err != nil {
		return removed, err
	}
	final, err := h.awaitProgress(ctx, "actors-final-native-progress", settled)
	if err != nil {
		return removed, err
	}
	if err := h.checkActorGuard(ctx, guard, final); err != nil {
		return final, err
	}
	return final, nil
}
