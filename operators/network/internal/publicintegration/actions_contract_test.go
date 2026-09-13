//go:build live

package publicintegration

import (
	"testing"
	"time"

	actions "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// actionContractFixture supplies public facts only for rejection-oriented qualification tests.
func actionContractFixture() (snapshot, actionSelection, *actions.BitcoinReorganization) {
	now := metav1.Now()
	target := bitcoin.BitcoinTargetIdentity{
		Participant: common.Binding{
			Kind: "StacksNetworkParticipant",
			Name: "compiled-bitcoin",
			UID:  "participant",
		},
		Pod:           common.Binding{Kind: "Pod", Name: "bitcoin-0", UID: "pod"},
		ContainerID:   "container",
		Configuration: common.Binding{Kind: "ConfigMap", Name: "config", UID: "config"},
		Credentials:   common.Binding{Kind: "Secret", Name: "rpc", UID: "rpc"},
		PolicyDigest:  "policy",
	}
	p := participantEvidence{
		Identity: identity{Name: target.Participant.Name, UID: target.Participant.UID},
		Name:     "bitcoin",
		Kind:     "BitcoinNode",
		Status: api.ParticipantStatus{
			Admission: &api.Admission{PolicyDigest: "policy"},
			Runtime: &api.ParticipantRuntimeStatus{
				PodRef:       target.Pod.DeepCopy(),
				ContainerID:  target.ContainerID,
				ConfigRef:    target.Configuration.DeepCopy(),
				RPCSecretRef: target.Credentials.DeepCopy(),
				PolicyDigest: target.PolicyDigest,
			},
		},
	}
	ref := common.Binding{Kind: "BitcoinExecution", Name: "execution", UID: "execution"}
	record := executionEvidence{
		Identity:       identity{Name: ref.Name, UID: ref.UID},
		ParticipantUID: p.Identity.UID,
		Status: bitcoin.BitcoinExecutionStatus{
			Observation: &bitcoin.BitcoinObservation{
				Target:     target,
				ObservedAt: now,
				Height:     50,
				Tip:        "original",
				Wallets:    []bitcoin.BitcoinWalletObservation{{Ready: true, Address: "bcrt-public-address"}},
			},
		},
	}
	s := snapshot{
		At:        now.Time,
		Root:      identity{UID: "root"},
		Operation: "Running",
		Status: api.StacksNetworkStatus{
			Initialization:    &api.InitializationStatus{Completed: true},
			Bitcoin:           &api.BitcoinRuntimeStatus{ExecutionRefs: []common.Binding{ref}},
			ObservationPolicy: &api.ObservationPolicy{PollIntervalSeconds: 2, RPCAllowanceSeconds: 10},
		},
		Participants: []participantEvidence{p},
		Executions:   []executionEvidence{record},
	}
	selected := actionSelection{
		Participant: p,
		Execution:   record.Identity,
		Target:      target,
		Height:      50,
		Address:     "bcrt-public-address",
	}
	status := actions.BitcoinBlockGenerationStatus{
		Phase:              "Completed",
		ObservedGeneration: 1,
		StartedAt:          &now,
		FinishedAt:         &now,
		LastDispatchID:     "receipt",
		LastBlockHash:      "replacement2",
		BlocksGenerated:    2,
		AdmittedNetwork:    &actions.NetworkIdentity{UID: "root"},
		AdmittedExecution:  &ref,
		AdmittedTarget: &actions.TargetIdentity{
			UID:           string(p.Identity.UID),
			Name:          p.Identity.Name,
			PodUID:        string(target.Pod.UID),
			ContainerID:   target.ContainerID,
			Configuration: target.Configuration,
			Credentials:   target.Credentials,
			SpecDigest:    target.PolicyDigest,
		},
	}
	for _, name := range []string{"Admitted", "EffectObserved", "CleanupComplete"} {
		status.Conditions = append(
			status.Conditions,
			metav1.Condition{Type: name, Status: metav1.ConditionTrue, ObservedGeneration: 1},
		)
	}
	request := &actions.BitcoinReorganization{
		ObjectMeta: metav1.ObjectMeta{Name: "reorganization", Generation: 1},
		Status: actions.BitcoinReorganizationStatus{
			BitcoinBlockGenerationStatus: status,
			OriginalChain: &actions.BitcoinChainPoint{
				Hash:              "original",
				Height:            50,
				PreviousBlockHash: "fork",
				Chainwork:         "10",
			},
			ForkParent: &actions.BitcoinChainPoint{Hash: "fork", Height: 49, Chainwork: "0f"},
			FinalChain: &actions.BitcoinChainPoint{
				Hash:              "replacement2",
				PreviousBlockHash: "replacement1",
				Height:            51,
				Chainwork:         "11",
			},
			InvalidatedHash:          "original",
			ReplacementBlockHashes:   []string{"replacement1", "replacement2"},
			InvalidationAcknowledged: true,
			CleanupAcknowledged:      true,
		},
	}
	return s, selected, request
}

func TestActionQualificationRejectsIncompleteOrForeignEvidence(t *testing.T) {
	for _, mode := range []string{
		"valid",
		"failed",
		"network",
		"participant",
		"pod",
		"credential",
		"record",
		"receipt",
		"count",
		"cleanup",
		"fork",
		"replacement",
		"work",
		"generation",
	} {
		t.Run(mode, func(t *testing.T) {
			_, selected, request := actionContractFixture()
			switch mode {
			case "failed":
				request.Status.Phase = "Inconclusive"
			case "network":
				request.Status.AdmittedNetwork.UID = "replacement"
			case "participant":
				request.Status.AdmittedTarget.UID = "replacement"
			case "pod":
				request.Status.AdmittedTarget.PodUID = "replacement"
			case "credential":
				request.Status.AdmittedTarget.Credentials.UID = "replacement"
			case "record":
				request.Status.AdmittedExecution.UID = "replacement"
			case "receipt":
				request.Status.LastDispatchID = ""
			case "count":
				request.Status.BlocksGenerated = 3
			case "cleanup":
				request.Status.CleanupAcknowledged = false
			case "fork":
				request.Status.ForkParent.Height--
			case "replacement":
				request.Status.ReplacementBlockHashes[0] = request.Status.ReplacementBlockHashes[1]
			case "work":
				request.Status.FinalChain.Chainwork = "10"
			case "generation":
				request.Status.Conditions[2].ObservedGeneration = 0
			}
			err := validateActionResult(request, selected, "root")
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s error=%v", mode, err)
			}
		})
	}
}

func TestActionQualificationRequiresFreshCurrentNativeIdentity(t *testing.T) {
	for _, mode := range []string{
		"valid",
		"stale",
		"future",
		"pod",
		"policy",
		"unbound",
		"foreign-record",
		"wallet",
		"armed",
		"incomplete",
	} {
		t.Run(mode, func(t *testing.T) {
			s, _, _ := actionContractFixture()
			switch mode {
			case "stale":
				s.At = s.At.Add(17 * time.Second)
			case "future":
				s.At = s.At.Add(-time.Second)
			case "pod":
				s.Participants[0].Status.Runtime.PodRef.UID = "replacement"
			case "policy":
				s.Participants[0].Status.Admission.PolicyDigest = "replacement"
			case "unbound":
				s.Status.Bitcoin.ExecutionRefs = nil
			case "foreign-record":
				s.Executions[0].ParticipantUID = "replacement"
			case "wallet":
				s.Executions[0].Status.Observation.Wallets[0].Ready = false
			case "armed":
				s.Executions[0].Status.Armed = &bitcoin.BitcoinArmedRPC{ID: "busy"}
			case "incomplete":
				s.Status.Initialization.Completed = false
			}
			_, err := selectActionTarget(s)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s error=%v", mode, err)
			}
		})
	}
}

// TestBaselineMutationDrainDoesNotTreatExpiredUnsentOffersAsReceipts verifies the qualification gate.
func TestBaselineMutationDrainDoesNotTreatExpiredUnsentOffersAsReceipts(t *testing.T) {
	now := time.Now()
	r := &bitcoin.BitcoinExecution{}
	r.Spec.Offer = &bitcoin.BitcoinBlockOffer{Number: 2, ExpiresAt: metav1.NewTime(now.Add(time.Second))}
	if !baselineMutationPending(r, now) {
		t.Fatal("live opportunity ignored")
	}
	r.Spec.Offer.ExpiresAt = metav1.NewTime(now)
	if baselineMutationPending(r, now) {
		t.Fatal("expired unsent offer requires a nonexistent receipt")
	}
	r.Status.Armed = &bitcoin.BitcoinArmedRPC{ID: "pending"}
	if !baselineMutationPending(r, now) {
		t.Fatal("expiry hid in-flight work")
	}
}

// TestActionQualificationWaitsForChosenProducer rejects fallback to another healthy actor.
func TestActionQualificationWaitsForChosenProducer(t *testing.T) {
	s, want, _ := actionContractFixture()
	if got, err := selectActionTargetUID(
		s,
		want.Participant.Identity.UID,
	); err != nil ||
		got.Participant.Identity.UID != want.Participant.Identity.UID {
		t.Fatalf("chosen producer unavailable: %v", err)
	}
	if _, err := selectActionTargetUID(s, "different-producer"); err == nil {
		t.Fatal("substituted a healthy peer for the chosen producer")
	}
	s.Executions[0].Status.Armed = &bitcoin.BitcoinArmedRPC{ID: "pending"}
	if _, err := selectActionTargetUID(s, want.Participant.Identity.UID); err == nil {
		t.Fatal("selected an occupied producer")
	}
}
