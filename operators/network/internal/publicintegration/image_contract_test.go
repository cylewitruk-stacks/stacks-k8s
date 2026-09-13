//go:build live

package publicintegration

import (
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
)

// TestActorQualificationImageRollRequiresSameDataAndDistinctImage guards the mixed-image evidence predicate.
func TestActorQualificationImageRollRequiresSameDataAndDistinctImage(t *testing.T) {
	old := actorEpoch{
		Participant: identity{UID: "actor"},
		Runtime:     api.ParticipantRuntimeStatus{PodRef: &common.Binding{UID: "old-pod"}, ImageID: "old-image"},
		Storage:     actorStorage{Claim: identity{UID: "claim"}, Volume: "volume"},
	}
	fresh := func() actorEpoch {
		return actorEpoch{
			Participant: old.Participant,
			Runtime:     api.ParticipantRuntimeStatus{PodRef: &common.Binding{UID: "new-pod"}, ImageID: "new-image"},
			Storage:     old.Storage,
		}
	}
	if err := sameActorStorageAcrossImageRoll(old, fresh()); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*actorEpoch){
		"participant":   func(e *actorEpoch) { e.Participant.UID = "replacement" },
		"same pod":      func(e *actorEpoch) { e.Runtime.PodRef.UID = "old-pod" },
		"same image":    func(e *actorEpoch) { e.Runtime.ImageID = "old-image" },
		"missing image": func(e *actorEpoch) { e.Runtime.ImageID = "" },
		"new claim":     func(e *actorEpoch) { e.Storage.Claim.UID = "new-claim" },
		"new data":      func(e *actorEpoch) { e.Storage.Volume = "new-volume" },
	} {
		t.Run(name, func(t *testing.T) {
			e := fresh()
			change(&e)
			if sameActorStorageAcrossImageRoll(old, e) == nil {
				t.Fatal("invalid mixed-image evidence accepted")
			}
		})
	}
}
