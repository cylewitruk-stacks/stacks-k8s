// Package profiles renders typed, deterministic regtest actor configurations.
package profiles

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"unicode/utf8"

	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/pelletier/go-toml/v2"
)

//go:embed templates/*.tmpl
var templates embed.FS

// StacksContext supplies public topology and optional externally provisioned credentials.
type StacksContext struct {
	// Network and Actor identify the node within its environment.
	Network, Actor string
	// Role selects miner, follower or signer ingress behavior.
	Role network.StacksNodeRole
	// BitcoinService and ports locate this node's burnchain source.
	BitcoinService                 string
	BitcoinRPCPort, BitcoinP2PPort int32
	// SignerService names the event destination without its port.
	SignerService string
	// Genesis supplies the common network chain parameters.
	Genesis *network.GenesisSpec
	// Generated supplies public profile overrides.
	Generated network.GeneratedConfig
	// RPCUser, RPCPassword and AuthToken are provided only to the actor that needs them.
	RPCUser, RPCPassword, AuthToken string
	// MiningPublicKey is the uncompressed Bitcoin wallet public key.
	MiningPublicKey string
}

// nodeTemplate is the fully resolved input to the common node template.
type nodeTemplate struct {
	StacksContext
	Seed, Bootstrap          string
	Miner, Stacker, Blocking bool
	Genesis                  network.GenesisSpec
}

// SignerContext supplies the signer-only key and its node's paired authentication token.
type SignerContext struct {
	PrivateKey, NodeHost, AuthToken string
}

// Bitcoin renders the public development-only Bitcoin profile.
func Bitcoin(rpcPort int32) string {
	return fmt.Sprintf("regtest=1\nprinttoconsole=1\nserver=1\ntxindex=1\ndiscover=0\ndnsseed=0\nlistenonion=0\nfallbackfee=0.00001\n\n[regtest]\nrpcbind=0.0.0.0:%d\nrpcallowip=0.0.0.0/0\nrpcuser=devnet\nrpcpassword=devnet\n", rpcPort)
}

// Stacks renders and parses a complete node TOML document from shared genesis values.
func Stacks(context StacksContext) (string, error) {
	if context.Role == network.StacksNodeMiner && context.MiningPublicKey == "" {
		return "", fmt.Errorf("miner configuration requires a mining public key")
	}
	genesis, err := ResolveGenesis(context.Genesis)
	if err != nil {
		return "", err
	}
	seed := context.Generated.Seed
	if seed == "" {
		sum := sha256.Sum256([]byte(context.Network + ":" + context.Actor))
		seed = hex.EncodeToString(sum[:])
	}
	peers := append([]string(nil), context.Generated.BootstrapPeers...)
	sort.Strings(peers)
	if context.RPCUser == "" {
		context.RPCUser = "devnet"
		context.RPCPassword = "devnet"
	}
	if context.AuthToken == "" {
		context.AuthToken = "12345"
	}
	if context.SignerService != "" {
		context.SignerService += ":30000"
	}
	return render("stacks-node.toml.tmpl", nodeTemplate{StacksContext: context, Seed: seed, Bootstrap: strings.Join(peers, ","), Miner: context.Role == network.StacksNodeMiner, Stacker: context.Role == network.StacksNodeSigner, Blocking: context.Generated.EventDispatcher == "blocking", Genesis: genesis})
}

// Signer renders and parses a complete signer TOML document.
func Signer(context SignerContext) (string, error) { return render("stacks-signer.toml.tmpl", context) }

// render rejects missing template fields and invalid TOML without exposing credential bytes.
func render(name string, value any) (string, error) {
	t, err := template.New("profiles").Option("missingkey=error").Funcs(template.FuncMap{"q": quoteTOML}).ParseFS(templates, "templates/*.tmpl")
	if err != nil {
		return "", fmt.Errorf("parse built-in configuration templates: %w", err)
	}
	var out bytes.Buffer
	if err = t.ExecuteTemplate(&out, name, value); err != nil {
		return "", fmt.Errorf("render %s: %w", name, err)
	}
	var parsed map[string]any
	if err = toml.Unmarshal(out.Bytes(), &parsed); err != nil {
		return "", fmt.Errorf("rendered %s is not valid TOML", name)
	}
	return out.String(), nil
}

// quoteTOML encodes a TOML 1.0 basic string, including Unicode escapes for controls.
func quoteTOML(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("configuration string is not valid UTF-8")
	}
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range value {
		switch {
		case r == '"' || r == '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&out, "\\u%04X", r)
		default:
			out.WriteRune(r)
		}
	}
	out.WriteByte('"')
	return out.String(), nil
}
