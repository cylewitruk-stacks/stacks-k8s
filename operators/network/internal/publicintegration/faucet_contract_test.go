//go:build live

package publicintegration

import (
	"strings"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// faucetContractFixture contains synthetic observations only for assertion validation.
func faucetContractFixture() (faucetSelection, *stacks.StacksFaucetRequest) {
	s := faucetSelection{participant: identity{Name: "participant", UID: "participant-uid"}, worker: identity{Name: "worker", UID: "worker-uid"}, logicalName: "faucet", processNonce: "process", profileDigest: "sha256:" + strings.Repeat("a", 64), destination: stacks.FaucetBinding{Kind: "StacksAccount", Name: "traffic-recipient", UID: "recipient-uid", Fingerprint: "recipient-digest"}, source: stacks.FaucetBinding{Kind: "StacksAccount", Name: "source", UID: "source-uid"}, target: stacks.FaucetBinding{Kind: "StacksNetworkParticipant", Name: "target", UID: "target-uid"}, address: "ST000000000000000000002AMW42H", amount: "1", offered: 3, included: 2, completed: 2}
	request := faucetRequest("test", "network-uid", s, time.Minute)
	request.Name = "request"
	request.UID = "request-uid"
	request.CreationTimestamp = metav1.NewTime(time.Now().Add(-time.Minute))
	request.Status.Admission = &stacks.FaucetAdmission{Decision: "Admitted", NetworkUID: "network-uid", Faucet: &stacks.FaucetBinding{Kind: "StacksNetworkParticipant", Name: s.participant.Name, UID: s.participant.UID}, Worker: &stacks.FaucetBinding{Kind: "Pod", Name: s.worker.Name, UID: s.worker.UID}, ProfileDigest: s.profileDigest, SourceAccount: &s.source, Target: &s.target, DestinationAccount: &s.destination, Destination: s.address, AmountMicroSTX: s.amount}
	request.Status.Execution = &stacks.FaucetExecution{Phase: "Completed", Reason: "Included", NetworkUID: "network-uid", FaucetUID: s.participant.UID, WorkerUID: s.worker.UID, ProcessNonce: s.processNonce, Destination: s.address, AmountMicroSTX: s.amount, TxID: strings.Repeat("b", 64), InclusionBlockID: strings.Repeat("c", 64), ObservedAt: metav1.Now()}
	return s, request
}

func TestFaucetRequestRequiresNativeOutcomeAndExactIdentities(t *testing.T) {
	for _, mode := range []string{"valid", "projection-only", "submitted", "replaced-request", "other-worker", "other-source", "no-send", "no-block", "old-observation", "wrong-amount", "uncertain"} {
		t.Run(mode, func(t *testing.T) {
			s, r := faucetContractFixture()
			switch mode {
			case "projection-only":
				r.Status.Phase = "Completed"
				r.Status.Execution = nil
			case "submitted":
				r.Status.Execution.Phase = "Submitted"
				r.Status.Execution.InclusionBlockID = ""
			case "replaced-request":
				r.UID = "replacement"
			case "other-worker":
				r.Status.Execution.WorkerUID = "replacement"
			case "other-source":
				copy := *r.Status.Admission.SourceAccount
				copy.UID = "replacement"
				r.Status.Admission.SourceAccount = &copy
			case "no-send":
				r.Status.Execution.NoSend = true
			case "no-block":
				r.Status.Execution.InclusionBlockID = ""
			case "old-observation":
				r.Status.Execution.ObservedAt = metav1.NewTime(r.CreationTimestamp.Add(-time.Second))
			case "wrong-amount":
				r.Spec.AmountMicroSTX = "2"
			case "uncertain":
				r.Status.Execution.Phase = "Inconclusive"
			}
			ready, err := includedRequest(r, "request-uid", "network-uid", s)
			switch mode {
			case "valid":
				if err != nil || !ready {
					t.Fatalf("valid observation: %v", err)
				}
			case "projection-only", "submitted":
				if err != nil || ready {
					t.Fatalf("non-inclusion accepted: %v", err)
				}
			default:
				if err == nil || ready {
					t.Fatalf("invalid evidence accepted: %v", err)
				}
			}
		})
	}
}

func TestFaucetSelectionCannotFollowRemovedOrReplacementParticipants(t *testing.T) {
	root := &api.StacksNetwork{Spec: api.StacksNetworkSpec{Participants: []api.Participant{{Name: "faucet", Kind: "StacksFaucet"}}}, Status: api.StacksNetworkStatus{Identities: []api.InstanceIdentity{{Name: "faucet", UID: "participant", Worker: &api.WorkerSession{Pod: api.WorkerPodBinding{Name: "worker", UID: "worker"}}}}}}
	participants := []participantEvidence{{Identity: identity{Name: "generated", UID: "participant"}, Name: "faucet", Kind: "StacksFaucet"}}
	if _, _, err := selectedFaucet(root, participants); err != nil {
		t.Fatal(err)
	}
	replacement := append([]participantEvidence(nil), participants...)
	replacement[0].Identity.UID = "replacement"
	if _, _, err := selectedFaucet(root, replacement); err == nil {
		t.Fatal("replacement participant selected")
	}
	root.Status.Identities[0].Removing = true
	if _, _, err := selectedFaucet(root, participants); err == nil {
		t.Fatal("removed faucet selected")
	}
	root.Status.Identities[0].Removing = false
	root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: "another", Kind: "StacksFaucet"})
	if _, _, err := selectedFaucet(root, participants); err == nil {
		t.Fatal("ambiguous faucet selection")
	}
}

func TestFaucetCountersRejectReplayAndProcessReplacement(t *testing.T) {
	selection, _ := faucetContractFixture()
	execution := &api.WorkerExecutionStatus{PodUID: selection.worker.UID, ProcessNonce: selection.processNonce, ProfileDigest: selection.profileDigest, Transactions: &api.TransactionExecutionStatus{Offered: 5, Included: 4}, Faucet: &stacks.FaucetWorkerSummary{Completed: 4}}
	s := snapshot{Participants: []participantEvidence{{Identity: selection.participant, Status: api.ParticipantStatus{Execution: execution}}}}
	if _, ready, err := faucetCounters(s, selection); err != nil || !ready {
		t.Fatalf("two sends: %v", err)
	}
	execution.Transactions.Offered++
	if _, _, err := faucetCounters(s, selection); err == nil {
		t.Fatal("extra send ignored")
	}
	execution.Transactions.Offered--
	execution.ProcessNonce = "replacement"
	if _, _, err := faucetCounters(s, selection); err == nil {
		t.Fatal("new process inherited old counters")
	}
}

func TestFaucetFixtureSupportsTwoPublicRequests(t *testing.T) {
	for _, variant := range []string{"minimal14", "full30"} {
		t.Run(variant, func(t *testing.T) {
			fixture, err := loadFixture(fixtureOptions{path: defaultFixture, namespace: "test", variant: variant, bitcoinImage: "bitcoin:31.1", stacksImage: "stacks:pinned", signerImage: "stacks:pinned", cadence: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			recipients := 0
			for _, o := range fixture.reusable {
				if o.GetKind() == "StacksAccount" && o.GetName() == "traffic-recipient" {
					recipients++
				}
			}
			if recipients != 1 {
				t.Fatal("fixture lacks exact reusable public recipient")
			}
			selection, _ := faucetContractFixture()
			r := faucetRequest("test", "actual-root-uid", selection, time.Minute)
			if r.UID != "" || r.Name != "" || r.GenerateName == "" || r.Spec.FaucetRef.Name != "faucet" || r.Spec.Destination.AccountRef.Name != "traffic-recipient" || r.Spec.AmountMicroSTX != common.Amount("1") || len(r.OwnerReferences) != 0 {
				t.Fatal("request invented identity or private inputs")
			}
		})
	}
}
