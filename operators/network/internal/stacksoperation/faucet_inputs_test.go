package stacksoperation

import (
	"context"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestFaucetPublicInputsRequireFrozenSourceTargetAndGenesis(t *testing.T) {
	for _, mode := range []string{
		"valid",
		"source-uid",
		"source-digest",
		"mounted-key",
		"target-uid",
		"genesis-source",
		"genesis-fingerprint",
	} {
		t.Run(mode, func(t *testing.T) {
			inputs, s, target := publicFixture(t)
			s.Participant.Status.Admission.Configuration = api.Configuration{
				StacksFaucet: &stacks.StacksFaucetSpec{
					AccountRef:    &common.NameRef{Name: "sender"},
					TargetNodeRef: &common.NameRef{Name: "node"},
				},
			}
			ctx := context.Background()
			c := inputs.Reader.(client.Client)
			var genesis api.StacksGenesis
			if err := c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "genesis"}, &genesis); err != nil {
				t.Fatal(err)
			}
			genesis.Spec.Source.NetworkUID = s.Network.UID
			if mode == "genesis-source" {
				genesis.Spec.Source.NetworkUID = "other"
			}
			if err := c.Update(ctx, &genesis); err != nil {
				t.Fatal(err)
			}
			s.Network.Status.GenesisRef.Fingerprint = foundation.Digest(genesis.Spec)
			a := &stacks.FaucetAdmission{
				SourceAccount: &stacks.FaucetBinding{Name: "sender", UID: "account", Fingerprint: "fingerprint"},
				Target:        &stacks.FaucetBinding{Name: target.Name, UID: target.UID},
			}
			switch mode {
			case "source-uid":
				a.SourceAccount.UID = "replacement"
			case "source-digest":
				a.SourceAccount.Fingerprint = "changed"
			case "mounted-key":
				inputs.Sender = "other"
			case "target-uid":
				a.Target.UID = "replacement"
			case "genesis-fingerprint":
				s.Network.Status.GenesisRef.Fingerprint = "changed"
			}
			result, err := inputs.Faucet(ctx, s, a)
			if mode == "valid" {
				if err != nil || result.Node == nil || result.StartHeight != 231 {
					t.Fatalf("valid public inputs rejected: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatal("changed public binding allowed")
			}
		})
	}
}
