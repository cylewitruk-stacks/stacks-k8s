package networkruntime

import (
	"strings"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func TestPreparedSetRequiresExactCurrentCohortAndNativeAgreement(t *testing.T) {
	for _, mode := range []string{
		"valid",
		"missing-node",
		"missing-signer",
		"old-pod",
		"stale",
		"unavailable",
		"different-weights",
		"extra-key",
		"wrong-cycle",
		"compatible-roll",
		"mining-changed",
	} {
		t.Run(mode, func(t *testing.T) {
			root, g := gateFixture()
			now := time.Now()
			pub := "02" + strings.Repeat("a", 64)
			participants := []api.StacksNetworkParticipant{}
			for _, name := range []string{"one", "two", "signer"} {
				p := participantFixture(root)
				p.Name = name
				p.UID = types.UID(name)
				p.Spec.ParticipantName = name
				p.Spec.Kind = "StacksNode"
				if name == "signer" {
					p.Spec.Kind = "StacksSigner"
				}
				p.Status.Admission = &api.Admission{PolicyDigest: name}
				p.Status.Runtime = &api.ParticipantRuntimeStatus{
					ObservedGeneration:  p.Generation,
					PolicyDigest:        name,
					PodRef:              &common.Binding{UID: types.UID("pod-" + name)},
					ContainerID:         "process",
					ConfigurationDigest: "config",
				}
				p.Status.Conditions = []metav1.Condition{
					{Type: "WorkloadReady", Status: metav1.ConditionTrue, ObservedGeneration: p.Generation},
					{Type: "ConfigVerified", Status: metav1.ConditionTrue, ObservedGeneration: p.Generation},
				}
				req := api.BootstrapRequirement{
					Kind:         p.Spec.Kind,
					Participant:  common.Binding{UID: p.UID},
					PolicyDigest: name,
				}
				if name == "signer" {
					p.Status.Admission.Configuration.StacksSigner = &stacks.StacksSignerSpec{
						AccountRef: &common.NameRef{Name: "consensus"},
					}
					req.Accounts = []api.PublicAccount{
						{Binding: common.Binding{Name: "consensus"}, Identity: common.PublicIdentity{PublicKey: pub}},
					}
				} else {
					p.Status.Admission.Configuration.StacksNode = &stacks.StacksNodeSpec{
						Mining: &stacks.Mining{Enabled: ptr.To(true)},
					}
					req.MiningEnabled = ptr.To(true)
					p.Status.Runtime.Protocol = &api.StacksProtocolObservation{
						Available:           true,
						FullySynced:         true,
						NetworkID:           0x80000000,
						GenesisUID:          g.UID,
						PodUID:              p.Status.Runtime.PodRef.UID,
						ContainerID:         "process",
						ConfigurationDigest: "config",
						ObservedAt:          metav1.NewTime(now),
						PreparedSet: &api.PreparedSignerSetObservation{
							Cycle:      12,
							Available:  true,
							Version:    1,
							Threshold:  "100",
							ObservedAt: metav1.NewTime(now),
							Signers: []api.PreparedSignerObservation{
								{PublicKey: pub, Weight: 2, StackedAmount: "200"},
							},
						},
					}
				}
				g.Spec.Bootstrap.Requirements = append(g.Spec.Bootstrap.Requirements, req)
				root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: name, Kind: p.Spec.Kind})
				participants = append(participants, *p)
			}
			observation := participants[0].Status.Runtime.Protocol
			switch mode {
			case "missing-node":
				participants = participants[1:]
			case "missing-signer":
				participants = participants[:2]
			case "old-pod":
				observation.PodUID = "old"
			case "stale":
				observation.ObservedAt = metav1.NewTime(now.Add(-17 * time.Second))
			case "unavailable":
				observation.Available = false
			case "different-weights":
				observation.PreparedSet.Signers[0].Weight = 3
			case "extra-key":
				observation.PreparedSet.Signers = append(
					observation.PreparedSet.Signers,
					api.PreparedSignerObservation{
						PublicKey:     "03" + strings.Repeat("b", 64),
						Weight:        1,
						StackedAmount: "100",
					},
				)
			case "compatible-roll":
				participants[0].Status.Admission.Configuration.StacksNode.Image = ptr.To("updated-image")
				participants[0].Status.Admission.PolicyDigest = "updated"
				participants[0].Status.Runtime.PolicyDigest = "updated"
			case "mining-changed":
				participants[0].Status.Admission.Configuration.StacksNode.Mining.Enabled = ptr.To(false)
			case "wrong-cycle":
				observation.PreparedSet.Cycle = 11
			}
			gate := api.Gate{Name: "PrepareNakamoto", TargetCycle: g.Spec.Bootstrap.Gates[1].TargetCycle}
			if got := preparedCohortSatisfied(
				root,
				g,
				participants,
				gate,
				now,
			); got != (mode == "valid" || mode == "compatible-roll") {
				t.Fatalf("prepared accepted=%v", got)
			}
		})
	}
}
