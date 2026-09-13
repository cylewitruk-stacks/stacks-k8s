package networkruntime

import (
	"strings"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestContractGateRequiresEveryFrozenSourceAndRegistryIdentity(t *testing.T) {
	for _, mode := range []string{
		"valid",
		"worker",
		"missing",
		"stale",
		"incomplete",
		"bundle",
		"source",
		"deployer",
		"keys",
		"aggregate",
		"threshold",
		"principal",
		"account",
		"policy",
	} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			root, g, participants := cohortFixture(now)
			p := &participants[0]
			p.Spec.Kind = "StacksContractSet"
			root.Spec.Participants[0].Kind = p.Spec.Kind
			p.Status.Admission.Configuration = api.Configuration{StacksContractSet: &stacks.StacksContractSetSpec{}}
			keys := []string{"02" + strings.Repeat("a", 64), "03" + strings.Repeat("b", 64)}
			aggregate := "02" + strings.Repeat("c", 64)
			requirement := g.Spec.Bootstrap.Requirements[0]
			requirement.Kind = p.Spec.Kind
			requirement.RegistryInitialization = &stacks.RegistryInitialization{
				Mode:                   "ExplicitTestRegistry",
				SignerAccountRefs:      []common.NameRef{{Name: "a"}, {Name: "b"}},
				AggregateKeyAccountRef: common.NameRef{Name: "aggregate"},
				Threshold:              2,
			}
			p.Status.Admission.Configuration.StacksContractSet.Initialization = requirement.RegistryInitialization.DeepCopy()
			p.Status.Admission.Configuration.StacksContractSet.Initialization = requirement.RegistryInitialization.DeepCopy()
			requirement.Accounts = []api.PublicAccount{
				{Binding: common.Binding{Name: "a"}, Identity: common.PublicIdentity{PublicKey: keys[0]}},
				{Binding: common.Binding{Name: "b"}, Identity: common.PublicIdentity{PublicKey: keys[1]}},
				{Binding: common.Binding{Name: "aggregate"}, Identity: common.PublicIdentity{PublicKey: aggregate}},
			}
			g.Spec.Bootstrap.Requirements = []api.BootstrapRequirement{requirement}
			g.Spec.Chain.Contracts.Deployer = "ST000000000000000000002AMW42H"
			g.Spec.Chain.Contracts.Bundle = "sbtc-regtest-v1"
			g.Spec.Chain.Contracts.SourceHashes = map[string]string{"sbtc-registry": strings.Repeat("d", 64)}
			principal, err := identity.EncodeAddress(21, [20]byte{1})
			if err != nil {
				t.Fatal(err)
			}
			o := &api.ContractSetObservation{
				Deployer:           g.Spec.Chain.Contracts.Deployer,
				Bundle:             g.Spec.Chain.Contracts.Bundle,
				SourceDigest:       foundation.Digest(g.Spec.Chain.Contracts.SourceHashes),
				SignerPublicKeys:   keys,
				AggregatePublicKey: aggregate,
				Threshold:          2,
				SignerPrincipal:    principal,
				Complete:           true,
				ObservedAt:         metav1.NewTime(now),
			}
			p.Status.Execution.Contracts = o
			switch mode {
			case "worker":
				p.Status.Execution.PodUID = "replacement"
			case "missing":
				participants = nil
			case "stale":
				o.ObservedAt = metav1.NewTime(now.Add(-17 * time.Second))
			case "incomplete":
				o.Complete = false
			case "bundle":
				o.Bundle = "other"
			case "source":
				o.SourceDigest = "different"
			case "deployer":
				o.Deployer = "other"
			case "keys":
				o.SignerPublicKeys = []string{keys[1], keys[0]}
			case "aggregate":
				o.AggregatePublicKey = keys[0]
			case "threshold":
				o.Threshold = 1
			case "principal":
				o.SignerPrincipal = "invalid"
			case "account":
				g.Spec.Bootstrap.Requirements[0].Accounts = nil
			case "policy":
				p.Status.Admission.PolicyDigest = "new"
			}
			if got := contractCohortSatisfied(root, g, participants, now); got != (mode == "valid") {
				t.Fatalf("accepted=%v", got)
			}
		})
	}
}
