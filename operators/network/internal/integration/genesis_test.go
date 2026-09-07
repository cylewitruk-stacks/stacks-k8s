//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"

	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyGenesisAdmission checks immutable recipe/snapshot behavior against the real API server.
func verifyGenesisAdmission(t *testing.T, ctx context.Context, c client.Client) {
	verifyGenesisShapeAdmission(t, ctx, c)
	profile := &network.StacksGenesisProfile{ObjectMeta: meta.ObjectMeta{Name: "genesis-recipe", Namespace: testNamespace}, Spec: profiles.DefaultGenesis()}
	profile.Spec.Balances = []network.GenesisBalance{{Name: "funded", Address: "STTEST", Amount: 123456}}
	must(t, c.Create(ctx, profile))
	changed := profile.DeepCopy()
	changed.Spec.Balances[0].Amount++
	if err := c.Update(ctx, changed); !apierrors.IsInvalid(err) {
		t.Fatalf("profile spec mutation accepted: %v", err)
	}
	parent := bitcoinNetwork("genesis-snapshot")
	parent.Spec.Genesis = profile.Spec.DeepCopy()
	must(t, c.Create(ctx, parent))
	for _, mutate := range []func(*network.StacksNetwork){
		func(n *network.StacksNetwork) { n.Spec.Genesis.Balances[0].Amount++ },
		func(n *network.StacksNetwork) { n.Spec.Genesis.Epochs[13].StartHeight++ },
		func(n *network.StacksNetwork) { n.Spec.Genesis.PoX.PrepareLength++ },
		func(n *network.StacksNetwork) { enabled := false; n.Spec.Genesis.UseTestGenesisChainstate = &enabled },
		func(n *network.StacksNetwork) { n.Spec.Genesis = nil },
	} {
		must(t, c.Get(ctx, client.ObjectKeyFromObject(parent), parent))
		candidate := parent.DeepCopy()
		mutate(candidate)
		if err := c.Patch(ctx, candidate, client.MergeFrom(parent)); !apierrors.IsInvalid(err) {
			t.Fatalf("network genesis mutation accepted: %v", err)
		}
	}
	// Mutable operation remains available on the same network.
	must(t, c.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	base := parent.DeepCopy()
	parent.Spec.Suspended = true
	must(t, c.Patch(ctx, parent, client.MergeFrom(base)))
	// Deleting/recreating the public recipe does not alter the copied network snapshot.
	must(t, c.Delete(ctx, profile))
	profile = &network.StacksGenesisProfile{ObjectMeta: meta.ObjectMeta{Name: "genesis-recipe", Namespace: testNamespace}, Spec: profiles.DefaultGenesis()}
	must(t, c.Create(ctx, profile))
	must(t, c.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	if len(parent.Spec.Genesis.Balances) != 1 || parent.Spec.Genesis.Balances[0].Amount != 123456 {
		t.Fatal("network depended on mutable profile lookup")
	}
	absent := bitcoinNetwork("genesis-absent")
	must(t, c.Create(ctx, absent))
	absentBase := absent.DeepCopy()
	absent.Spec.Genesis = profile.Spec.DeepCopy()
	if err := c.Patch(ctx, absent, client.MergeFrom(absentBase)); !apierrors.IsInvalid(err) {
		t.Fatalf("late genesis addition accepted: %v", err)
	}
	// Ordinary deletion is not obstructed by the immutability rules.
	must(t, c.Delete(ctx, parent))
	must(t, c.Delete(ctx, profile))
	must(t, c.Delete(ctx, absent))
}

// verifyGenesisShapeAdmission checks compiler parity on both consumers of shared markers.
func verifyGenesisShapeAdmission(t *testing.T, ctx context.Context, c client.Client) {
	cases := []struct {
		name   string
		valid  bool
		mutate func(*network.GenesisSpec)
	}{
		{"complete", true, func(g *network.GenesisSpec) {}},
		{"omitted", true, func(g *network.GenesisSpec) { *g = network.GenesisSpec{} }},
		{"empty", true, func(g *network.GenesisSpec) { g.Epochs = []network.GenesisEpoch{} }},
		{"equal-heights", true, func(g *network.GenesisSpec) {
			for i := range g.Epochs {
				g.Epochs[i].StartHeight = 0
			}
		}},
		{"anonymous", true, func(g *network.GenesisSpec) {
			g.Balances = []network.GenesisBalance{{Address: "a", Amount: 1}, {Address: "b", Amount: 2}}
		}},
		{"named", true, func(g *network.GenesisSpec) {
			g.Balances = []network.GenesisBalance{{Name: "a", Address: "a", Amount: 1}, {Name: "b", Address: "b", Amount: 2}}
		}},
		{"partial", false, func(g *network.GenesisSpec) { g.Epochs = g.Epochs[:3] }},
		{"one-epoch", false, func(g *network.GenesisSpec) { g.Epochs = g.Epochs[:1] }},
		{"duplicate-epoch", false, func(g *network.GenesisSpec) { g.Epochs[2].Name = "2.0" }},
		{"unordered", false, func(g *network.GenesisSpec) { g.Epochs[2].Name, g.Epochs[3].Name = g.Epochs[3].Name, g.Epochs[2].Name }},
		{"decreasing", false, func(g *network.GenesisSpec) { g.Epochs[3].StartHeight = 1 }},
		{"first-height", false, func(g *network.GenesisSpec) {
			for i := range g.Epochs {
				g.Epochs[i].StartHeight++
			}
		}},
		{"second-height", false, func(g *network.GenesisSpec) { g.Epochs[1].StartHeight = 1 }},
		{"duplicate-name", false, func(g *network.GenesisSpec) {
			g.Balances = []network.GenesisBalance{{Name: "stacker", Address: "a", Amount: 1}, {Name: "stacker", Address: "b", Amount: 2}}
		}},
		{"duplicate-address", false, func(g *network.GenesisSpec) {
			g.Balances = []network.GenesisBalance{{Address: "a", Amount: 1}, {Address: "a", Amount: 2}}
		}},
		{"max-balances", true, func(g *network.GenesisSpec) {
			for i := range 1000 {
				name := fmt.Sprintf("%s%03d", strings.Repeat("a", 60), i)
				g.Balances = append(g.Balances, network.GenesisBalance{Name: name, Address: name, Amount: 1})
			}
		}},
	}
	maximum := cases[len(cases)-1]
	duplicate := maximum
	duplicate.name, duplicate.valid = "max-duplicate-name", false
	duplicate.mutate = func(g *network.GenesisSpec) {
		maximum.mutate(g)
		g.Balances[999].Name = g.Balances[0].Name
	}
	cases = append(cases, duplicate)
	for _, tc := range cases {
		for _, kind := range []string{"profile", "network"} {
			t.Run("genesis-"+kind+"-"+tc.name, func(t *testing.T) {
				genesis := profiles.DefaultGenesis()
				tc.mutate(&genesis)
				name := "shape-" + kind + "-" + tc.name
				var object client.Object
				if kind == "profile" {
					object = &network.StacksGenesisProfile{ObjectMeta: meta.ObjectMeta{Name: name, Namespace: testNamespace}, Spec: genesis}
				} else {
					parent := bitcoinNetwork(name)
					parent.Spec.Suspended = true
					parent.Spec.Genesis = &genesis
					object = parent
				}
				// Preserve explicit empty arrays/names that typed omitempty would remove.
				wire, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
				must(t, err)
				wire["apiVersion"] = "network.stacks.org/v1alpha1"
				wire["kind"] = "StacksGenesisProfile"
				genesisWire := wire["spec"].(map[string]any)
				if kind == "network" {
					wire["kind"] = "StacksNetwork"
					genesisWire = genesisWire["genesis"].(map[string]any)
				}
				if tc.name == "empty" {
					genesisWire["epochs"] = []any{}
				}
				if tc.name == "anonymous" {
					genesisWire["balances"].([]any)[0].(map[string]any)["name"] = ""
				}
				err = c.Create(ctx, &unstructured.Unstructured{Object: wire})
				if !tc.valid {
					if !apierrors.IsInvalid(err) {
						t.Fatalf("invalid shape: %v", err)
					}
					return
				}
				must(t, err)
				// Decode the admitted shape, not just the local input, before compiling it.
				must(t, c.Get(ctx, client.ObjectKeyFromObject(object), object))
				if profile, ok := object.(*network.StacksGenesisProfile); ok {
					genesis = profile.Spec
				} else {
					genesis = *object.(*network.StacksNetwork).Spec.Genesis
				}
				if _, err = profiles.ResolveGenesis(&genesis); err != nil {
					t.Fatalf("admitted genesis rejected by compiler: %v", err)
				}
				must(t, c.Delete(ctx, object))
			})
		}
	}
}
