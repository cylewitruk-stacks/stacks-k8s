package networkruntime

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestPoX4GateRequiresEveryFrozenIdentityAndFreshObservation(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "replacement", "stale", "wrong-holder", "wrong-signer", "wrong-amount", "worker-replaced", "policy", "removed", "not-in-reward-set", "compatible-update"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			root, g, participants := cohortFixture(now)
			observation := participants[0].Status.Execution.PoX4
			switch mode {
			case "missing":
				participants = participants[1:]
			case "replacement":
				participants[0].UID = "different"
			case "stale":
				observation.ObservedAt = metav1.NewTime(now.Add(-17 * time.Second))
			case "wrong-holder":
				observation.Holder = "other"
			case "wrong-signer":
				observation.SignerPublicKey = "03" + strings.Repeat("c", 64)
			case "wrong-amount":
				observation.AmountMicroSTX = "101"
			case "worker-replaced":
				participants[0].Status.Execution.PodUID = "other"
			case "compatible-update":
				participants[0].Status.Admission.PolicyDigest = "new"
				participants[0].Status.Execution.AppliedPolicyDigest = "new"
			case "policy":
				participants[0].Status.Admission.PolicyDigest = "new"
			case "removed":
				root.Spec.Participants = root.Spec.Participants[1:]
			case "not-in-reward-set":
				observation.TargetCycleMatched = false
			}
			got := pox4CohortSatisfied(root, g, participants, g.Spec.Bootstrap.Gates[1], now)
			if got != (mode == "valid" || mode == "compatible-update") {
				t.Fatalf("cohort accepted=%v", got)
			}
		})
	}
}

// cohortFixture supplies a captured holder and consensus participant with one live worker.
func cohortFixture(now time.Time) (*api.StacksNetwork, *api.StacksGenesis, []api.StacksNetworkParticipant) {
	root, g := gateFixture()
	p := participantFixture(root)
	p.Spec.Kind = "StacksStacker"
	p.Spec.ParticipantName = "stacker"
	p.Name = "stacker-instance"
	signer := participantFixture(root)
	signer.Spec.Kind = "StacksSigner"
	signer.Spec.ParticipantName = "signer"
	signer.Name = "signer-instance"
	signer.UID = "signer-uid"
	holder := "ST000000000000000000002AMW42H"
	pub := "02" + strings.Repeat("a", 64)
	digest := "sha256:" + strings.Repeat("b", 64)
	root.Spec.Participants = []api.Participant{{Name: "stacker", Kind: "StacksStacker"}, {Name: "signer", Kind: "StacksSigner"}}
	root.Status.Identities = []api.InstanceIdentity{{Name: "stacker", UID: p.UID, Worker: &api.WorkerSession{Pod: api.WorkerPodBinding{Kind: "Pod", Name: "worker", UID: "worker-uid"}, ProfileDigest: digest}}}
	p.Status.Admission = &api.Admission{PolicyDigest: "stacker-policy", Configuration: api.Configuration{StacksStacker: &stacks.StacksStackerSpec{AmountMicroSTX: ptr.To(common.Amount("100")), HolderAccountRef: &common.NameRef{Name: "holder"}, SignerRef: &common.NameRef{Name: "signer"}}}}
	signer.Status.Admission = &api.Admission{PolicyDigest: "signer-policy", Configuration: api.Configuration{StacksSigner: &stacks.StacksSignerSpec{AccountRef: &common.NameRef{Name: "consensus"}}}}
	p.Status.Conditions = []metav1.Condition{{Type: "WorkloadReady", Status: metav1.ConditionTrue, ObservedGeneration: p.Generation}}
	p.Status.Execution = &api.WorkerExecutionStatus{PodUID: "worker-uid", ProcessNonce: "process", ProfileDigest: digest, AppliedPolicyDigest: "stacker-policy", ObservedGeneration: p.Generation, Phase: "Active", PoX4: &api.PoX4EnrollmentObservation{Holder: holder, SignerPublicKey: pub, AmountMicroSTX: "100", FirstCycle: 11, EndCycleExclusive: 17, TargetCycle: 12, TargetCycleMatched: true, ObservedAt: metav1.NewTime(now)}}
	g.Spec.Bootstrap.Requirements = []api.BootstrapRequirement{
		{Kind: "StacksStacker", Participant: common.Binding{Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID}, PolicyDigest: "stacker-policy", AmountMicroSTX: ptr.To(common.Amount("100")), Dependencies: []common.Binding{{Kind: "StacksNetworkParticipant", Name: signer.Name, UID: signer.UID}}, Accounts: []api.PublicAccount{{Binding: common.Binding{Name: "holder"}, Identity: common.PublicIdentity{Address: holder}}}},
		{Kind: "StacksSigner", Participant: common.Binding{Kind: "StacksNetworkParticipant", Name: signer.Name, UID: signer.UID}, PolicyDigest: "signer-policy", Accounts: []api.PublicAccount{{Binding: common.Binding{Name: "consensus"}, Identity: common.PublicIdentity{PublicKey: pub}}}},
	}
	participants := []api.StacksNetworkParticipant{*p, *signer}
	return root, g, participants
}

func TestPoX5GateRequiresExactManagerAndCapturedCohort(t *testing.T) {
	for _, mode := range []string{"valid", "stale", "manager", "source", "holder", "signer", "amount", "delegated", "membership", "coverage", "worker", "removed", "administrator"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			root, g, participants := cohortFixture(now)
			p := &participants[0]
			policy := p.Status.Admission.Configuration.StacksStacker
			policy.AdministratorAccountRef = &common.NameRef{Name: "administrator"}
			administrator := "ST000000000000000000002AMW42H"
			g.Spec.Bootstrap.Requirements[0].Accounts = append(g.Spec.Bootstrap.Requirements[0].Accounts, api.PublicAccount{Binding: common.Binding{Name: "administrator"}, Identity: common.PublicIdentity{Address: administrator}})
			old := p.Status.Execution.PoX4
			source, err := protocolcontracts.DirectManager(old.Holder)
			if err != nil {
				t.Fatal(err)
			}
			o := &api.PoX5EnrollmentObservation{Holder: old.Holder, Manager: administrator + ".direct-signer", ManagerSourceDigest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(source))), SignerPublicKey: old.SignerPublicKey, AmountMicroSTX: "100", DelegatedAmountMicroSTX: "100", FirstCycle: 14, EndCycleExclusive: 20, TargetCycle: 15, TargetCycleMatched: true, ObservedAt: metav1.NewTime(now)}
			p.Status.Execution.PoX5 = o
			switch mode {
			case "stale":
				o.ObservedAt = metav1.NewTime(now.Add(-17 * time.Second))
			case "manager":
				o.Manager = "other.direct-signer"
			case "source":
				o.ManagerSourceDigest = "sha256:" + strings.Repeat("f", 64)
			case "holder":
				o.Holder = "other"
			case "signer":
				o.SignerPublicKey = "other"
			case "amount":
				o.AmountMicroSTX = "101"
			case "delegated":
				o.DelegatedAmountMicroSTX = "101"
			case "membership":
				o.TargetCycleMatched = false
			case "coverage":
				o.EndCycleExclusive = 15
			case "worker":
				p.Status.Execution.PodUID = "replacement"
			case "removed":
				root.Spec.Participants = root.Spec.Participants[1:]
			case "administrator":
				g.Spec.Bootstrap.Requirements[0].Accounts = g.Spec.Bootstrap.Requirements[0].Accounts[:1]
			}
			if got := pox5CohortSatisfied(root, g, participants, api.Gate{TargetCycle: ptr.To(int64(15))}, now); got != (mode == "valid") {
				t.Fatalf("accepted=%v", got)
			}
		})
	}
}
