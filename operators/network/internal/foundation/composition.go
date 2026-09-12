// Package foundation resolves composable declarations and captures immutable genesis.
// It deliberately creates no actor or protocol mutation workloads.
package foundation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DefinitionObject returns the typed declaration for a supported kind.
func DefinitionObject(kind api.ParticipantKind) client.Object {
	switch kind {
	case "BitcoinNode":
		return &bitcoin.BitcoinNode{}
	case "StacksNode":
		return &stacks.StacksNode{}
	case "StacksSigner":
		return &stacks.StacksSigner{}
	case "StacksStacker":
		return &stacks.StacksStacker{}
	case "StacksFaucet":
		return &stacks.StacksFaucet{}
	case "StacksContractSet":
		return &stacks.StacksContractSet{}
	case "StacksTransactionProduction":
		return &stacks.StacksTransactionProduction{}
	case "BitcoinBlockProduction":
		return &bitcoin.BitcoinBlockProduction{}
	default:
		return nil
	}
}

func branch(kind api.ParticipantKind) string {
	s := string(kind)
	if s == "" {
		return ""
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// Digest hashes deterministic JSON for public policy identity.
func Digest(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// ParticipantName produces the pre-UID allocation name defined by the public contract.
func ParticipantName(networkUID, name string) string {
	token := sha256.Sum256([]byte(networkUID))
	prefix := "n-" + hex.EncodeToString(token[:])[:12] + "-participant-" + name
	if len(prefix) > 39 {
		prefix = prefix[:39]
	}
	prefix = strings.TrimRight(prefix, "-")
	suffix := Digest([]string{networkUID, name, "participant"})
	return prefix + "-" + strings.TrimPrefix(suffix, "sha256:")[:12]
}

// RuntimeName isolates runtime roots and reserves suffix space for workload children.
func RuntimeName(networkUID, participantUID, kind, name, purpose string) string {
	return naming.RuntimeName(networkUID, participantUID, kind, name, purpose)
}

func objectMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	err = d.Decode(&out)
	return out, err
}
func decodeMap(m map[string]any, out any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func merge(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for _, pair := range [][2]string{{"schedule", "scheduleRef"}, {"overrides", "secretRef"}, {"nodeRefs", "discovery"}} {
		if _, ok := src[pair[0]]; ok {
			delete(dst, pair[1])
		}
		if _, ok := src[pair[1]]; ok {
			delete(dst, pair[0])
		}
	}
	if src["ephemeral"] == true {
		delete(dst, "size")
		delete(dst, "class")
		delete(dst, "retainOnDelete")
	}
	for _, key := range []string{"size", "class", "retainOnDelete"} {
		if _, ok := src[key]; ok {
			delete(dst, "ephemeral")
		}
	}
	for k, v := range src {
		if child, ok := v.(map[string]any); ok {
			prior, _ := dst[k].(map[string]any)
			dst[k] = merge(prior, child)
		} else {
			dst[k] = v
		}
	}
	return dst
}
func fill(dst, defaults map[string]any) {
	for k, v := range defaults {
		prior, ok := dst[k]
		if !ok {
			dst[k] = v
			continue
		}
		a, aok := prior.(map[string]any)
		b, bok := v.(map[string]any)
		if aok && bok {
			fill(a, b)
		}
	}
}

// Compose applies explicit root, definition and entry values before profile fallbacks.
func Compose(root *api.StacksNetwork, entry api.Participant, source any) (api.Configuration, error) {
	key := branch(entry.Kind)
	if DefinitionObject(entry.Kind) == nil {
		return api.Configuration{}, fmt.Errorf("unsupported participant kind")
	}
	input, err := objectMap(source)
	if err != nil {
		return api.Configuration{}, err
	}
	out := map[string]any{}
	if root.Spec.Defaults != nil {
		defaults, _ := objectMap(root.Spec.Defaults)
		if entry.Kind == "BitcoinNode" || entry.Kind == "StacksNode" || entry.Kind == "StacksSigner" {
			if v, ok := defaults["storage"]; ok {
				out["storage"] = v
			}
			imageKey := key
			if entry.Kind == "BitcoinNode" {
				imageKey = "bitcoin"
			}
			if images, ok := defaults["images"].(map[string]any); ok {
				if v, ok := images[imageKey]; ok {
					out["image"] = v
				}
			}
			if resources, ok := defaults["actorResources"].(map[string]any); ok {
				if v, ok := resources[imageKey]; ok {
					out["resources"] = v
				}
			}
		}
		if entry.Kind == "BitcoinNode" || strings.HasPrefix(key, "stacks") && entry.Kind != "StacksNode" && entry.Kind != "StacksSigner" {
			if v, ok := defaults["workerPlacement"]; ok {
				out["workerPlacement"] = v
			}
		}
	}
	out = merge(out, input)
	if entry.Overrides != nil {
		overrides, _ := objectMap(entry.Overrides)
		if len(overrides) != 1 || overrides[key] == nil {
			return api.Configuration{}, fmt.Errorf("override branch does not match kind")
		}
		out = merge(out, overrides[key].(map[string]any))
	}
	switch entry.Kind {
	case "BitcoinNode", "StacksNode", "StacksSigner":
		image := "stacks-core:4.0.1-pox5"
		if entry.Kind == "BitcoinNode" {
			image = "bitcoin/bitcoin:31.1"
		}
		fill(out, map[string]any{"image": image, "imagePullPolicy": "IfNotPresent"})
		storage, _ := out["storage"].(map[string]any)
		if storage == nil {
			storage = map[string]any{}
			out["storage"] = storage
		}
		if storage["ephemeral"] != true {
			fill(storage, map[string]any{"size": "2Gi", "retainOnDelete": false})
		}
		if entry.Kind != "StacksSigner" && out["peers"] == nil {
			fill(out, map[string]any{"peers": map[string]any{"discovery": "Network"}})
		}
	case "StacksStacker":
		fill(out, map[string]any{"lockCycles": json.Number("6"), "renewWhenRemainingCycles": json.Number("3")})
	case "StacksFaucet":
		fill(out, map[string]any{"genesisBalanceMicroSTX": "1000000000000", "maxRequestMicroSTX": "1000000000000"})
	case "StacksTransactionProduction":
		fill(out, map[string]any{"amountMicroSTX": "1", "feeMicroSTX": "1000", "interval": "10s"})
	case "StacksContractSet":
		fill(out, map[string]any{"bundle": "sbtc-regtest-v1"})
	case "BitcoinBlockProduction":
		if out["schedule"] == nil && out["scheduleRef"] == nil {
			out["schedule"] = map[string]any{"cadence": map[string]any{"mode": "Fixed", "interval": "5s"}}
		}
	}
	var result api.Configuration
	if err := decodeMap(map[string]any{key: out}, &result); err != nil {
		return result, fmt.Errorf("invalid typed participant configuration: %w", err)
	}
	return result, nil
}

func sourceSpec(obj client.Object) (any, error) {
	m, err := objectMap(obj)
	if err != nil {
		return nil, err
	}
	return m["spec"], nil
}
func inlineSpec(entry api.Participant) (any, error) {
	m, err := objectMap(entry.Definition.Inline)
	if err != nil {
		return nil, err
	}
	if len(m) != 1 || m[branch(entry.Kind)] == nil {
		return nil, fmt.Errorf("inline branch does not match kind")
	}
	return m[branch(entry.Kind)], nil
}

func duration(value common.Duration) (time.Duration, error) {
	v, err := time.ParseDuration(string(value))
	if err != nil || v < time.Second || v > time.Hour {
		return 0, fmt.Errorf("duration must be between 1s and 1h")
	}
	return v, nil
}
func validateSchedule(s bitcoin.BitcoinBlockScheduleSpec) error {
	c := s.Cadence
	switch c.Mode {
	case "Fixed":
		if c.Interval == nil || c.MinimumInterval != nil || c.MaximumInterval != nil {
			return fmt.Errorf("invalid fixed cadence")
		}
		_, err := duration(*c.Interval)
		return err
	case "Uniform":
		if c.Interval != nil || c.MinimumInterval == nil || c.MaximumInterval == nil {
			return fmt.Errorf("invalid uniform cadence")
		}
		low, err := duration(*c.MinimumInterval)
		if err != nil {
			return err
		}
		high, err := duration(*c.MaximumInterval)
		if err != nil {
			return err
		}
		if high < low {
			return fmt.Errorf("uniform maximum is below minimum")
		}
		return nil
	default:
		return fmt.Errorf("unsupported cadence")
	}
}

func equal(a, b any) bool { return reflect.DeepEqual(a, b) }
