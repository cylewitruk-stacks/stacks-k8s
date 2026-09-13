//go:build integration

package foundationintegration

import (
	"context"
	"fmt"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// candidateCheck supplies controlled validation outcomes without claiming native image execution.
type candidateCheck func(context.Context, foundation.CandidateConfiguration, string) (bool, error)

func (f candidateCheck) ValidateConfiguration(
	ctx context.Context,
	in foundation.CandidateConfiguration,
	name string,
) (bool, error) {
	return f(ctx, in, name)
}

// verifyCandidateConfigurationGate proves invalid, pending and stale validation cannot freeze genesis.
func verifyCandidateConfigurationGate(
	t *testing.T,
	ctx context.Context,
	c client.Client,
	r *foundation.Reconciler,
	request ctrl.Request,
	root *api.StacksNetwork,
) {
	t.Helper()
	var source stacks.StacksNode
	key := client.ObjectKey{Namespace: root.Namespace, Name: "signer-node-01"}
	if err := c.Get(ctx, key, &source); err != nil {
		t.Fatal(err)
	}
	original := source.Spec.DeepCopy()
	oldHook := r.Configurations
	defer func() { r.Configurations = oldHook }()
	noArtifact := func() {
		t.Helper()
		var list api.StacksGenesisList
		if err := c.List(ctx, &list, client.InNamespace(root.Namespace)); err != nil {
			t.Fatal(err)
		}
		if len(list.Items) != 0 {
			t.Fatal("invalid or stale candidate created irreversible genesis")
		}
	}
	root.Spec.Operation = "Running"
	updateObject(t, ctx, c, root)
	// Bitcoin uses its native public paths and the same pre-freeze report boundary.
	var bitcoinSource bitcoin.BitcoinNode
	for _, entry := range root.Spec.Participants {
		if entry.Kind == "BitcoinNode" && entry.Definition.Ref != nil {
			if err := c.Get(
				ctx,
				client.ObjectKey{Namespace: root.Namespace, Name: entry.Definition.Ref.Name},
				&bitcoinSource,
			); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if bitcoinSource.Name == "" {
		t.Fatal("fixture lacks a selected Bitcoin source")
	}
	bitcoinOriginal := bitcoinSource.Spec.DeepCopy()
	bitcoinSource.Spec.Config = &common.Config{
		Overrides: &runtime.RawExtension{Raw: []byte(`{"regtest":{"norpcport":true}}`)},
	}
	updateObject(t, ctx, c, &bitcoinSource)
	r.Configurations = candidateCheck(func(context.Context, foundation.CandidateConfiguration, string) (bool, error) {
		t.Fatal("protected Bitcoin alias reached renderer")
		return false, nil
	})
	driveRoot(t, ctx, c, r, request, root)
	noArtifact()
	bitcoinSource.Spec.Config = &common.Config{Overrides: &runtime.RawExtension{Raw: []byte(`{"dbcache":256}`)}}
	updateObject(t, ctx, c, &bitcoinSource)
	r.Configurations = candidateCheck(
		func(context.Context, foundation.CandidateConfiguration, string) (bool, error) { return false, nil },
	)
	driveRoot(t, ctx, c, r, request, root)
	noArtifact()
	bitcoinCalls, bitcoinMaxCalls := 0, 0
	r.Configurations = candidateCheck(
		func(_ context.Context, in foundation.CandidateConfiguration, name string) (bool, error) {
			if in.Root.Status.GenesisRef != nil ||
				in.Participants[name].Status.Admission.Configuration.BitcoinNode.Config == nil {
				t.Fatal("Bitcoin candidate inputs not captured before freeze")
			}
			bitcoinCalls++
			if bitcoinCalls > bitcoinMaxCalls {
				bitcoinMaxCalls = bitcoinCalls
			}
			return bitcoinCalls == 1, nil
		},
	)
	for i := 0; i < 100 && bitcoinMaxCalls < 2; i++ {
		bitcoinCalls = 0
		if _, err := r.Reconcile(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	if bitcoinMaxCalls < 2 {
		t.Fatal("Bitcoin agreement not rechecked at freeze")
	}
	noArtifact()
	if err := c.Get(ctx, client.ObjectKeyFromObject(&bitcoinSource), &bitcoinSource); err != nil {
		t.Fatal(err)
	}
	bitcoinSource.Spec = *bitcoinOriginal
	updateObject(t, ctx, c, &bitcoinSource)

	source.Spec.Config = &common.Config{Overrides: &runtime.RawExtension{Raw: []byte(`{"node":{"seed":"forbidden"}}`)}}
	updateObject(t, ctx, c, &source)
	r.Configurations = candidateCheck(func(context.Context, foundation.CandidateConfiguration, string) (bool, error) {
		t.Fatal("protected override reached private renderer")
		return false, nil
	})
	driveRoot(t, ctx, c, r, request, root)
	noArtifact()
	source.Spec.Config = &common.Config{
		Overrides: &runtime.RawExtension{Raw: []byte(`{"node":{"mine_microblocks":false}}`)},
	}
	updateObject(t, ctx, c, &source)
	r.Configurations = candidateCheck(func(context.Context, foundation.CandidateConfiguration, string) (bool, error) {
		return false, fmt.Errorf("selected image rejected candidate native configuration")
	})
	driveRoot(t, ctx, c, r, request, root)
	noArtifact()
	r.Configurations = candidateCheck(
		func(context.Context, foundation.CandidateConfiguration, string) (bool, error) { return false, nil },
	)
	driveRoot(t, ctx, c, r, request, root)
	noArtifact()
	// Let preliminary validation succeed, then withdraw its exact result at the fresh boundary.
	calls, maxCalls := 0, 0
	r.Configurations = candidateCheck(
		func(_ context.Context, in foundation.CandidateConfiguration, name string) (bool, error) {
			if in.Root.Status.GenesisRef != nil ||
				in.Participants[name].Status.Admission.Configuration.StacksNode.Config == nil ||
				len(in.Genesis.Chain.Epochs) != 14 {
				t.Fatal("candidate validation did not receive complete public pre-freeze inputs")
			}
			calls++
			if calls > maxCalls {
				maxCalls = calls
			}
			return calls == 1, nil
		},
	)
	for i := 0; i < 100 && maxCalls < 2; i++ {
		calls = 0
		if _, err := r.Reconcile(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	if maxCalls < 2 {
		t.Fatal("configuration report was not rechecked at freeze")
	}
	noArtifact()
	if err := c.Get(ctx, request.NamespacedName, root); err != nil {
		t.Fatal(err)
	}
	root.Spec.Operation = "Paused"
	updateObject(t, ctx, c, root)
	if err := c.Get(ctx, key, &source); err != nil {
		t.Fatal(err)
	}
	source.Spec = *original
	updateObject(t, ctx, c, &source)
	r.Configurations = oldHook
	driveRoot(t, ctx, c, r, request, root)
	noArtifact()
}
