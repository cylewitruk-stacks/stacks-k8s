//go:build integration

package foundationintegration

import (
	"context"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyRegistryAdmissionGate rejects native-invalid quorum declarations before irreversible freeze.
func verifyRegistryAdmissionGate(t *testing.T, ctx context.Context, c client.Client, root *api.StacksNetwork) {
	t.Helper()
	var source stacks.StacksContractSet
	if err := c.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: "sbtc"}, &source); err != nil {
		t.Fatal(err)
	}
	for _, single := range []bool{true, false} {
		invalid := source.DeepCopy()
		invalid.Spec.Initialization.Threshold = 1
		if single {
			invalid.Spec.Initialization.SignerAccountRefs = invalid.Spec.Initialization.SignerAccountRefs[:1]
		}
		if err := c.Update(ctx, invalid); !apierrors.IsInvalid(err) {
			t.Fatalf("native-invalid registry source accepted: %v", err)
		}
		candidate := root.DeepCopy()
		for i := range candidate.Spec.Participants {
			p := &candidate.Spec.Participants[i]
			if p.Kind == "StacksContractSet" {
				p.Definition = api.Definition{Inline: &api.Configuration{StacksContractSet: invalid.Spec.DeepCopy()}}
				break
			}
		}
		if err := c.Update(ctx, candidate); !apierrors.IsInvalid(err) {
			t.Fatalf("native-invalid inline registry accepted: %v", err)
		}
	}
	// Three declared keys with a strict 2-of-3 majority remain supported by the native wrapper.
	valid := source.DeepCopy()
	valid.Name = "registry-majority-validation"
	valid.ResourceVersion = ""
	valid.UID = ""
	valid.Spec.Initialization.SignerAccountRefs = append(valid.Spec.Initialization.SignerAccountRefs, common.NameRef{Name: "third-public-key"})
	if err := c.Create(ctx, valid); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, valid); err != nil {
		t.Fatal(err)
	}
	var artifacts api.StacksGenesisList
	if err := c.List(ctx, &artifacts, client.InNamespace(root.Namespace)); err != nil || len(artifacts.Items) != 0 {
		t.Fatalf("registry admission created genesis: %v", err)
	}
}
