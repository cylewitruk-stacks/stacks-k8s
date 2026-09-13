//go:build live

package publicintegration

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const defaultFixture = "../../../../docs/design/public-api/examples/30-actors.yaml"

// fixtureOptions changes only test declaration inputs, never generated runtime resources.
type fixtureOptions struct {
	path, namespace, variant               string
	bitcoinImage, stacksImage, signerImage string
	cadence                                time.Duration
	nodeMap                                map[string]string
}

// declarations retains user-owned reusable objects separately from the network instance.
type declarations struct {
	root     *unstructured.Unstructured
	reusable []*unstructured.Unstructured
}

// loadFixture decodes the documented YAML or an explicitly supplied Kubernetes List.
func loadFixture(options fixtureOptions) (declarations, error) {
	var result declarations
	data, err := os.ReadFile(options.path)
	if err != nil {
		return result, err
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var objects []*unstructured.Unstructured
	for {
		object := &unstructured.Unstructured{}
		if err := decoder.Decode(object); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return result, err
		}
		if object.GetKind() == "List" {
			items, _, err := unstructured.NestedSlice(object.Object, "items")
			if err != nil {
				return result, err
			}
			for _, item := range items {
				value, ok := item.(map[string]any)
				if !ok {
					return result, fmt.Errorf("invalid list item")
				}
				objects = append(objects, &unstructured.Unstructured{Object: value})
			}
		} else if object.GetKind() != "" {
			objects = append(objects, object)
		}
	}
	for _, o := range objects {
		if o.GetKind() == "Namespace" {
			continue
		}
		group := o.GroupVersionKind().Group
		if !strings.HasSuffix(group, ".stacks.org") || o.GroupVersionKind().Version != "v1alpha2" ||
			o.GetName() == "" ||
			strings.HasSuffix(o.GetKind(), "Request") ||
			o.GetKind() == "StacksNetworkParticipant" ||
			o.GetKind() == "StacksGenesis" {
			return result, fmt.Errorf("fixture contains unsupported non-declaration %s", o.GetKind())
		}
		if len(o.GetOwnerReferences()) != 0 || len(o.GetFinalizers()) != 0 {
			return result, fmt.Errorf("fixture declarations must be independently owned")
		}
		o.SetNamespace(options.namespace)
		o.SetUID("")
		o.SetResourceVersion("")
		o.SetManagedFields(nil)
		unstructured.RemoveNestedField(o.Object, "status")
		replacePlacement(o.Object, options.nodeMap)
		if o.GetKind() == "StacksNetwork" {
			if result.root != nil || o.GetName() != "network" {
				return result, fmt.Errorf("exactly one network declaration named network is required")
			}
			result.root = o
		} else {
			result.reusable = append(result.reusable, o)
		}
	}
	if result.root == nil {
		return result, fmt.Errorf("fixture has no network")
	}
	root := result.root
	for key, value := range map[string]string{
		"bitcoin":      options.bitcoinImage,
		"stacksNode":   options.stacksImage,
		"stacksSigner": options.signerImage,
	} {
		if value == "" || strings.HasSuffix(value, ":latest") {
			return result, fmt.Errorf("explicit non-latest %s image required", key)
		}
		_ = unstructured.SetNestedField(root.Object, value, "spec", "defaults", "images", key)
	}
	_ = unstructured.SetNestedField(root.Object, "Running", "spec", "operation")
	_ = unstructured.SetNestedField(root.Object, false, "spec", "defaults", "storage", "retainOnDelete")
	participants, _, err := unstructured.NestedSlice(root.Object, "spec", "participants")
	if err != nil {
		return result, err
	}
	switch options.variant {
	case "minimal14":
		selected := map[string]bool{}
		for _, name := range []string{
			"btc-01",
			"btc-02",
			"btc-07",
			"miner-01",
			"signer-node-01",
			"signer-node-02",
			"signer-01",
			"signer-02",
			"stacker-01",
			"stacker-02",
			"blocks",
			"traffic",
			"sbtc",
			"faucet",
		} {
			selected[name] = true
		}
		reduced := []any{}
		for _, raw := range participants {
			p, ok := raw.(map[string]any)
			if !ok {
				return result, fmt.Errorf("invalid participant")
			}
			name, _ := p["name"].(string)
			if selected[name] {
				reduced = append(reduced, p)
				delete(selected, name)
			}
		}
		if len(selected) != 0 {
			return result, fmt.Errorf("minimal fixture lacks required declarations: %v", selected)
		}
		_ = unstructured.SetNestedSlice(root.Object, reduced, "spec", "participants")
		for _, o := range result.reusable {
			if o.GetKind() == "BitcoinBlockProduction" && o.GetName() == "blocks" {
				_ = unstructured.SetNestedSlice(
					o.Object,
					[]any{
						map[string]any{"nodeRef": map[string]any{"name": "btc-01"}, "weight": int64(1)},
						map[string]any{"nodeRef": map[string]any{"name": "btc-02"}, "weight": int64(1)},
					},
					"spec",
					"targets",
				)
				_ = unstructured.SetNestedSlice(
					o.Object,
					[]any{map[string]any{"name": "miner-wallet-01"}},
					"spec",
					"initialization",
					"minerWalletRefs",
				)
			}
		}
	case "full30":
		actors := 0
		for _, raw := range participants {
			p, _ := raw.(map[string]any)
			switch p["kind"] {
			case "BitcoinNode", "StacksNode", "StacksSigner":
				actors++
			}
		}
		if actors != 30 {
			return result, fmt.Errorf("full30 requires exactly 30 actors, found %d", actors)
		}
	default:
		return result, fmt.Errorf("variant must be minimal14 or full30")
	}
	if options.cadence < time.Second || options.cadence > time.Hour {
		return result, fmt.Errorf("cadence must be 1s..1h")
	}
	for _, o := range result.reusable {
		if o.GetKind() == "BitcoinBlockSchedule" && o.GetName() == "steady-blocks" {
			_ = unstructured.SetNestedField(o.Object, "Fixed", "spec", "cadence", "mode")
			_ = unstructured.SetNestedField(o.Object, options.cadence.String(), "spec", "cadence", "interval")
		}
	}
	return result, nil
}

// replacePlacement translates explicit fixture hostnames without changing scheduling policy.
func replacePlacement(value any, nodeMap map[string]string) {
	switch object := value.(type) {
	case map[string]any:
		for key, child := range object {
			if key == "kubernetes.io/hostname" {
				if old, ok := child.(string); ok && nodeMap[old] != "" {
					object[key] = nodeMap[old]
				}
			} else {
				replacePlacement(child, nodeMap)
			}
		}
	case []any:
		for _, child := range object {
			replacePlacement(child, nodeMap)
		}
	}
}

func TestFixtureVariantsPreservePublicDeclarations(t *testing.T) {
	for _, variant := range []string{"minimal14", "full30"} {
		t.Run(variant, func(t *testing.T) {
			d, err := loadFixture(
				fixtureOptions{
					path:         defaultFixture,
					namespace:    "fresh-test",
					variant:      variant,
					bitcoinImage: "bitcoin:test",
					stacksImage:  "stacks:test",
					signerImage:  "signer:test",
					cadence:      5 * time.Second,
					nodeMap:      map[string]string{"stacks-k8s-worker": "selected-node"},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if d.root.GetNamespace() != "fresh-test" || len(d.reusable) < 50 {
				t.Fatal("reusable declarations were dropped")
			}
			p, _, _ := unstructured.NestedSlice(d.root.Object, "spec", "participants")
			if variant == "minimal14" && len(p) != 14 {
				t.Fatal("minimal cohort differs")
			}
			for _, o := range d.reusable {
				if o.GetNamespace() != "fresh-test" || o.GetUID() != "" || o.GetKind() == "Secret" {
					t.Fatal("fixture gained generated/private identity")
				}
			}
		})
	}
}
