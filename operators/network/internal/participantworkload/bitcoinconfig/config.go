// Package bitcoinconfig validates and renders bounded native Bitcoin configuration.
package bitcoinconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/util/validation"
)

// MaximumBytes bounds private documents and public overrides.
const MaximumBytes = 900 * 1024

// document retains section scope and the order of repeated option values.
type document map[string]map[string][]string

var optionName = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// baseName identifies negated options without allowing them to bypass protection.
func baseName(name string) string {
	for strings.HasPrefix(name, "no") {
		name = strings.TrimPrefix(name, "no")
	}
	return name
}

// contained rejects options which escape the single mounted document or managed process.
func contained(name string) bool {
	switch baseName(name) {
	case "conf",
		"includeconf",
		"datadir",
		"blocksdir",
		"walletdir",
		"wallet",
		"settings",
		"pid",
		"daemon",
		"daemonwait",
		"startupnotify",
		"blocknotify",
		"walletnotify",
		"alertnotify",
		"shutdownnotify",
		"loadblock",
		"debuglogfile",
		"stopatheight",
		"stopafterblockimport":
		return false
	}
	return true
}

// protected identifies managed identities, ingress, chain parameters and peer seeds.
func protected(name string) bool {
	name = baseName(name)
	if strings.HasPrefix(name, "rpc") || strings.HasPrefix(name, "zmq") {
		return true
	}
	switch name {
	case "regtest",
		"chain",
		"testnet",
		"testnet4",
		"signet",
		"signetchallenge",
		"signetseednode",
		"vbparams",
		"testactivationheight",
		"mocktime",
		"server",
		"listen",
		"bind",
		"whitebind",
		"port",
		"externalip",
		"addnode",
		"connect",
		"proxy",
		"onion",
		"onlynet",
		"dnsseed",
		"fixedseeds",
		"networkactive",
		"txindex",
		"prune",
		"disablewallet":
		return true
	}
	return !contained(name)
}

// ValidateCustomization checks public structure without reading any private bytes.
func ValidateCustomization(config *common.Config) error {
	if config == nil {
		return nil
	}
	if config.SecretRef != nil && config.Overrides != nil {
		return fmt.Errorf("configuration sources are exclusive")
	}
	if config.Compatibility != nil && *config.Compatibility != common.CompatibilityManaged &&
		*config.Compatibility != common.CompatibilityUnverified {
		return fmt.Errorf("unsupported configuration compatibility")
	}
	if ref := config.SecretRef; ref != nil &&
		(len(validation.IsDNS1123Subdomain(ref.Name)) != 0 || len(validation.IsConfigMapKey(ref.Key)) != 0) {
		return fmt.Errorf("invalid configuration Secret reference")
	}
	aliases := map[string]bool{}
	for _, ref := range config.ServiceRefs {
		if len(validation.IsDNS1123Label(ref.Alias)) != 0 || aliases[ref.Alias] ||
			len(validation.IsDNS1123Label(ref.Name)) != 0 {
			return fmt.Errorf("invalid or duplicate Service alias")
		}
		supportedEndpoint := (ref.Kind == string(api.ParticipantBitcoinNode) ||
			ref.Kind == string(api.ParticipantStacksNode)) &&
			(ref.Endpoint == common.EndpointRPC ||
				ref.Endpoint == common.EndpointP2P) ||
			ref.Kind == string(api.ParticipantStacksSigner) &&
				ref.Endpoint == common.EndpointEvents
		if !supportedEndpoint {
			return fmt.Errorf("unsupported Service endpoint")
		}
		aliases[ref.Alias] = true
	}
	if len(aliases) > 0 && config.SecretRef == nil {
		return fmt.Errorf("service substitutions require a complete configuration Secret")
	}
	_, err := overrides(config)
	return err
}

// Apply reports native syntax and managed-setting agreement, not Core startup compatibility.
func Apply(generated, custom []byte, config *common.Config, services map[string]string) ([]byte, bool, error) {
	if err := ValidateCustomization(config); err != nil {
		return nil, false, err
	}
	base, err := parse(generated)
	if err != nil {
		return nil, false, err
	}
	if config == nil {
		return generated, true, nil
	}
	verified := config.Compatibility == nil || *config.Compatibility != common.CompatibilityUnverified
	if config.SecretRef != nil {
		text := string(custom)
		for _, ref := range config.ServiceRefs {
			host := services[ref.Alias]
			if len(validation.IsDNS1123Subdomain(host)) != 0 {
				return nil, false, fmt.Errorf("service alias unavailable")
			}
			text = strings.ReplaceAll(text, "${SERVICE:"+ref.Alias+"}", host)
		}
		if strings.Contains(text, "${SERVICE:") {
			return nil, false, fmt.Errorf("unmatched Service placeholder")
		}
		candidate, err := parse([]byte(text))
		if err != nil {
			return nil, false, err
		}
		if verified && !reflect.DeepEqual(managed(base), managed(candidate)) {
			return nil, false, fmt.Errorf("protected Bitcoin configuration disagrees")
		}
		base = candidate
	} else {
		values, err := overrides(config)
		if err != nil {
			return nil, false, err
		}
		for section, options := range values {
			if base[section] == nil {
				base[section] = map[string][]string{}
			}
			for key, values := range options {
				base[section][key] = values
			}
		}
	}
	data := render(base)
	if len(data) > MaximumBytes {
		return nil, false, fmt.Errorf("configuration exceeds size bound")
	}
	return data, verified, nil
}

// managed retains every protected occurrence, including section and negation spelling.
func managed(in document) document {
	out := document{}
	for section, options := range in {
		for key, values := range options {
			if protected(key) {
				if out[section] == nil {
					out[section] = map[string][]string{}
				}
				out[section][key] = values
			}
		}
	}
	return out
}

// parse accepts the explicit single-file native subset and returns redacted errors.
func parse(data []byte) (document, error) {
	if len(data) == 0 || len(data) > MaximumBytes || bytes.ContainsAny(data, "\x00") {
		return nil, fmt.Errorf("invalid Bitcoin configuration size or encoding")
	}
	out := document{"": {}}
	section := ""
	for _, raw := range strings.Split(string(data), "\n") {
		raw = strings.TrimSuffix(raw, "\r")
		if strings.Contains(raw, "\r") {
			return nil, fmt.Errorf("invalid Bitcoin configuration encoding")
		}
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if line != "[regtest]" && line != "[main]" && line != "[test]" && line != "[testnet4]" &&
				line != "[signet]" {
				return nil, fmt.Errorf("unsupported Bitcoin configuration section")
			}
			section = line[1 : len(line)-1]
			if out[section] == nil {
				out[section] = map[string][]string{}
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || !optionName.MatchString(key) || !contained(key) {
			return nil, fmt.Errorf("invalid or unsupported Bitcoin configuration option")
		}
		out[section][key] = append(out[section][key], value)
	}
	return out, nil
}

// overrides maps root scalars and one section level into native option lists.
func overrides(config *common.Config) (document, error) {
	out := document{}
	if config == nil || config.Overrides == nil {
		return out, nil
	}
	if len(config.Overrides.Raw) > MaximumBytes {
		return nil, fmt.Errorf("configuration overrides exceed size bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(config.Overrides.Raw))
	decoder.UseNumber()
	var raw map[string]any
	if decoder.Decode(&raw) != nil || raw == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("overrides must contain one object")
	}
	add := func(section, key string, value any) error {
		if !optionName.MatchString(key) {
			return fmt.Errorf("invalid Bitcoin option name")
		}
		if protected(key) {
			return fmt.Errorf("protected Bitcoin configuration path %s", strings.TrimPrefix(section+"."+key, "."))
		}
		list, ok := value.([]any)
		if !ok {
			list = []any{value}
		}
		values := []string{}
		for _, item := range list {
			var text string
			switch value := item.(type) {
			case string:
				text = value
			case bool:
				if value {
					text = "1"
				} else {
					text = "0"
				}
			case json.Number:
				text = string(value)
			default:
				return fmt.Errorf("expected scalar values for Bitcoin option")
			}
			if strings.ContainsAny(text, "\r\n\x00#") || text != strings.TrimSpace(text) {
				return fmt.Errorf("invalid Bitcoin option value")
			}
			values = append(values, text)
		}
		if out[section] == nil {
			out[section] = map[string][]string{}
		}
		out[section][key] = values
		return nil
	}
	for key, value := range raw {
		if section, ok := value.(map[string]any); ok {
			if key != "regtest" && key != "main" && key != "test" && key != "testnet4" && key != "signet" {
				return nil, fmt.Errorf("unsupported Bitcoin configuration section")
			}
			for option, item := range section {
				if err := add(key, option, item); err != nil {
					return nil, err
				}
			}
		} else if err := add("", key, value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// render sorts sections and keys while preserving each repeated option's order.
func render(in document) []byte {
	sections := make([]string, 0, len(in))
	for section := range in {
		sections = append(sections, section)
	}
	sort.Strings(sections)
	var out strings.Builder
	for _, section := range sections {
		if section != "" {
			out.WriteString("[" + section + "]\n")
		}
		keys := make([]string, 0, len(in[section]))
		for key := range in[section] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			for _, value := range in[section][key] {
				out.WriteString(key + "=" + value + "\n")
			}
		}
	}
	return []byte(out.String())
}
