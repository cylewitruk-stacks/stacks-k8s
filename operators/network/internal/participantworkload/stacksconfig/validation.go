package stacksconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/pelletier/go-toml/v2"
	"k8s.io/apimachinery/pkg/util/validation"
)

// MaximumBytes bounds both custom TOML and generated configuration artifacts.
const MaximumBytes = 900 * 1024

var nodeProtected = []string{"node.name", "node.seed", "node.local_peer_seed", "node.miner", "node.stacker", "node.rpc_bind", "node.p2p_bind", "node.p2p_address", "node.data_url", "node.working_dir", "node.use_test_genesis_chainstate", "node.pox_5_sbtc_contract", "node.pox_5_sbtc_registry_contract", "node.pox_5_bond_admin", "node.pox_5_pause_admin", "connection_options.auth_token", "connection_options.public_ip_address", "burnchain.chain", "burnchain.mode", "burnchain.magic_bytes", "burnchain.pox_prepare_length", "burnchain.pox_reward_length", "burnchain.peer_host", "burnchain.peer_port", "burnchain.rpc_port", "burnchain.rpc_ssl", "burnchain.username", "burnchain.password", "burnchain.wallet_name", "burnchain.local_mining_public_key", "burnchain.epochs", "ustx_balance", "events_observer"}
var signerProtected = []string{"stacks_private_key", "node_host", "endpoint", "network", "auth_password", "db_path"}

// ValidateCustomization checks public structure and protected paths before private rendering.
func ValidateCustomization(kind api.ParticipantKind, config *common.Config) error {
	if kind != api.ParticipantStacksNode && kind != api.ParticipantStacksSigner {
		return fmt.Errorf("unsupported configuration kind")
	}
	if config == nil {
		return nil
	}
	if config.SecretRef != nil && config.Overrides != nil {
		return fmt.Errorf("configuration sources are exclusive")
	}
	if config.Compatibility != nil && *config.Compatibility != common.CompatibilityManaged && *config.Compatibility != common.CompatibilityUnverified {
		return fmt.Errorf("unsupported configuration compatibility")
	}
	if config.SecretRef != nil && (len(validation.IsDNS1123Subdomain(config.SecretRef.Name)) != 0 || len(validation.IsConfigMapKey(config.SecretRef.Key)) != 0) {
		return fmt.Errorf("invalid configuration Secret reference")
	}
	aliases := map[string]bool{}
	for _, ref := range config.ServiceRefs {
		if ref.Alias == "" || len(ref.Alias) > 63 || aliases[ref.Alias] || len(validation.IsDNS1123Label(ref.Name)) != 0 {
			return fmt.Errorf("invalid or duplicate Service alias %s", ref.Alias)
		}
		if !((ref.Kind == string(api.ParticipantBitcoinNode) || ref.Kind == string(api.ParticipantStacksNode)) && (ref.Endpoint == common.EndpointRPC || ref.Endpoint == common.EndpointP2P) || ref.Kind == string(api.ParticipantStacksSigner) && ref.Endpoint == common.EndpointEvents) {
			return fmt.Errorf("unsupported Service endpoint for alias %s", ref.Alias)
		}
		aliases[ref.Alias] = true
	}
	if len(config.ServiceRefs) > 0 && config.SecretRef == nil {
		return fmt.Errorf("Service substitutions require a complete configuration Secret")
	}
	_, err := customOverrides(kind, config)
	return err
}

// customOverrides decodes only supported TOML values and rejects protected writes.
func customOverrides(kind api.ParticipantKind, config *common.Config) (map[string]any, error) {
	if config == nil || config.Overrides == nil {
		return nil, nil
	}
	if len(config.Overrides.Raw) > MaximumBytes {
		return nil, fmt.Errorf("configuration overrides exceed size bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(config.Overrides.Raw))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("invalid structured overrides")
	}
	if raw == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("overrides must contain one table")
	}
	converted, err := normalize(raw, 0)
	if err != nil {
		return nil, err
	}
	protected := nodeProtected
	if kind == api.ParticipantStacksSigner {
		protected = signerProtected
	}
	overrides := converted.(map[string]any)
	if err := rejectProtected(overrides, "", protected); err != nil {
		return nil, err
	}
	return overrides, nil
}

// Apply validates customization and preserves protected managed identities and genesis.
// The returned bytes may contain private values and must stay in the scoped resolver.
func Apply(kind api.ParticipantKind, generated, custom []byte, config *common.Config, services map[string]string) ([]byte, bool, error) {
	if err := ValidateCustomization(kind, config); err != nil {
		return nil, false, err
	}
	protected := nodeProtected
	if kind == api.ParticipantStacksSigner {
		protected = signerProtected
	} else if kind != api.ParticipantStacksNode {
		return nil, false, fmt.Errorf("unsupported configuration kind")
	}
	base, err := document(generated)
	if err != nil {
		return nil, false, err
	}
	if config == nil {
		return generated, true, nil
	}
	if config.SecretRef != nil && config.Overrides != nil {
		return nil, false, fmt.Errorf("configuration sources are exclusive")
	}
	if config.SecretRef != nil {
		text := string(custom)
		for _, ref := range config.ServiceRefs {
			host, ok := services[ref.Alias]
			if !ok || host == "" {
				return nil, false, fmt.Errorf("unknown Service alias %s", ref.Alias)
			}
			text = strings.ReplaceAll(text, "${SERVICE:"+ref.Alias+"}", host)
		}
		if strings.Contains(text, "${SERVICE:") {
			return nil, false, fmt.Errorf("unmatched Service placeholder")
		}
		candidate, err := document([]byte(text))
		if err != nil {
			return nil, false, err
		}
		unverified := config.Compatibility != nil && *config.Compatibility == common.CompatibilityUnverified
		if !unverified {
			for _, path := range protected {
				expected, exists := at(base, path)
				actual, present := at(candidate, path)
				if exists != present || !reflect.DeepEqual(expected, actual) {
					return nil, false, fmt.Errorf("protected configuration disagrees at %s", path)
				}
			}
		}
		return []byte(text), !unverified, nil
	}
	if config.Overrides != nil {
		overrides, err := customOverrides(kind, config)
		if err != nil {
			return nil, false, err
		}
		merge(base, overrides)
	}
	data, err := toml.Marshal(base)
	if err != nil {
		return nil, false, fmt.Errorf("unsupported TOML override value")
	}
	if _, err := document(data); err != nil {
		return nil, false, err
	}
	return data, config.Compatibility == nil || *config.Compatibility != common.CompatibilityUnverified, nil
}

// document validates bounded TOML without exposing source text through parser errors.
func document(data []byte) (map[string]any, error) {
	if len(data) == 0 || len(data) > MaximumBytes {
		return nil, fmt.Errorf("configuration size is invalid")
	}
	var result map[string]any
	if err := toml.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid native TOML configuration")
	}
	if _, err := normalize(result, 0); err != nil {
		return nil, err
	}
	return result, nil
}

// normalize checks structural bounds and converts JSON numbers into native TOML values.
func normalize(value any, depth int) (any, error) {
	if depth > 32 {
		return nil, fmt.Errorf("configuration nesting exceeds bound")
	}
	switch v := value.(type) {
	case map[string]any:
		if len(v) > 16384 {
			return nil, fmt.Errorf("configuration table exceeds bound")
		}
		out := make(map[string]any, len(v))
		for key, item := range v {
			converted, err := normalize(item, depth+1)
			if err != nil {
				return nil, err
			}
			out[key] = converted
		}
		return out, nil
	case []any:
		if len(v) > 16384 {
			return nil, fmt.Errorf("configuration list exceeds bound")
		}
		out := make([]any, len(v))
		for i, item := range v {
			converted, err := normalize(item, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = converted
		}
		return out, nil
	case json.Number:
		if i, err := strconv.ParseInt(string(v), 10, 64); err == nil {
			return i, nil
		}
		if !strings.ContainsAny(string(v), ".eE") {
			return nil, fmt.Errorf("integer override exceeds signed TOML range")
		}
		if f, err := strconv.ParseFloat(string(v), 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
			return f, nil
		}
		return nil, fmt.Errorf("unsupported numeric override")
	case string, bool, int64, uint64:
		return value, nil
	case float64:
		if !math.IsInf(v, 0) && !math.IsNaN(v) {
			return v, nil
		}
	}
	return nil, fmt.Errorf("unsupported TOML scalar or null")
}

// at selects a protected table path without treating literal dotted keys as nesting.
func at(root map[string]any, path string) (any, bool) {
	var value any = root
	for _, part := range strings.Split(path, ".") {
		table, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = table[part]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

// rejectProtected rejects attempts to write protected paths, including whole ancestors.
func rejectProtected(values map[string]any, prefix string, protected []string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		for _, fixed := range protected {
			if path == fixed || strings.HasPrefix(path, fixed+".") {
				return fmt.Errorf("protected configuration override at %s", path)
			}
		}
		if nested, ok := values[key].(map[string]any); ok {
			if err := rejectProtected(nested, path, protected); err != nil {
				return err
			}
		} else {
			for _, fixed := range protected {
				if strings.HasPrefix(fixed, path+".") {
					return fmt.Errorf("protected configuration override at %s", path)
				}
			}
		}
	}
	return nil
}

// merge recursively combines tables while replacing arrays and scalar leaves atomically.
func merge(base, overrides map[string]any) {
	for key, value := range overrides {
		if incoming, ok := value.(map[string]any); ok {
			if current, ok := base[key].(map[string]any); ok {
				merge(current, incoming)
				continue
			}
		}
		base[key] = value
	}
}
