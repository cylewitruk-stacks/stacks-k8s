//go:build live

package publicintegration

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestFreshStorageRejectsReuseAndRestoredData(t *testing.T) {
	for _, mode := range []string{
		"fresh",
		"old-claim",
		"old-volume",
		"snapshot",
		"clone",
		"foreign-binding",
		"unbound",
	} {
		t.Run(mode, func(t *testing.T) {
			before := storageInventory{claims: map[types.UID]bool{}, volumes: map[types.UID]bool{}}
			claim := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: "lab", UID: "claim"},
				Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "volume"},
				Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
			}
			volume := &corev1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{Name: "volume", UID: "volume"},
				Spec: corev1.PersistentVolumeSpec{
					ClaimRef: &corev1.ObjectReference{Name: "claim", Namespace: "lab", UID: "claim"},
				},
				Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
			}
			switch mode {
			case "old-claim":
				before.claims[claim.UID] = true
			case "old-volume":
				before.volumes[volume.UID] = true
			case "snapshot":
				claim.Spec.DataSource = &corev1.TypedLocalObjectReference{Kind: "VolumeSnapshot", Name: "snapshot"}
			case "clone":
				claim.Spec.DataSourceRef = &corev1.TypedObjectReference{Kind: "PersistentVolumeClaim", Name: "other"}
			case "foreign-binding":
				volume.Spec.ClaimRef.UID = "other"
			case "unbound":
				claim.Status.Phase = corev1.ClaimPending
			}
			if freshStorage(before, claim, volume) != (mode == "fresh") {
				t.Fatalf("incorrect fresh-storage outcome for %s", mode)
			}
		})
	}
}

func TestFreshFollowerRequiresCurrentPoX5CanonicalPeerAndNewHeight(t *testing.T) {
	for _, mode := range []string{
		"matching",
		"other-fork",
		"stale-peer",
		"same-pod",
		"no-peer",
		"no-progress",
		"pox4",
	} {
		t.Run(mode, func(t *testing.T) {
			s := actorSnapshot()
			s.Participants[0].Status.Runtime.Protocol.PoXContract = "ST000000000000000000002AMW42H.pox-5"
			peer := s.Participants[0]
			peer.Name = "cohort"
			peer.Identity.UID = "cohort-participant"
			peer.Status = *peer.Status.DeepCopy()
			peer.Status.Runtime.PodRef.UID = "cohort-pod"
			peer.Status.Runtime.Protocol.PodUID = "cohort-pod"
			s.Participants = append(s.Participants, peer)
			switch mode {
			case "other-fork":
				s.Participants[1].Status.Runtime.Protocol.IndexBlockID = "other"
			case "stale-peer":
				s.Participants[1].Status.Runtime.Protocol.ObservedAt = metav1.NewTime(s.At.Add(-time.Hour))
			case "same-pod":
				s.Participants[1].Status.Runtime.PodRef.UID = s.Participants[0].Status.Runtime.PodRef.UID
				s.Participants[1].Status.Runtime.Protocol.PodUID = s.Participants[0].Status.Runtime.Protocol.PodUID
			case "no-peer":
				s.Participants = s.Participants[:1]
			case "no-progress":
				s.Participants[0].Status.Runtime.Protocol.StacksHeight = 5
			case "pox4":
				s.Participants[0].Status.Runtime.Protocol.PoXContract = "ST000000000000000000002AMW42H.pox-4"
			}
			if followerOnCohortTip(s, "late", 5) != (mode == "matching") {
				t.Fatalf("incorrect canonical catch-up outcome for %s", mode)
			}
		})
	}
}
