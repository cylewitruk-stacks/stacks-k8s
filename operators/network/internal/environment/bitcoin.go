// Package environment provisions disposable networks outside the controllers.
package environment

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DefaultBitcoinImage pins the qualified official Core 31.1 image index.
const DefaultBitcoinImage = "bitcoin/bitcoin:31.1@sha256:da25cedc66b1daefff9f412ee196c901a899c3fa68a33b20849c3e08b5c40d63"

// BitcoinOptions selects the Bitcoin topology and RPC capabilities to provision.
type BitcoinOptions struct {
	// Namespace, Name, Image and Address select the environment and coinbase destination.
	Namespace, Name, Image, Address string
	// Reorganization grants the optional bounded reorganization method profile.
	Reorganization bool
	// Interval is the total baseline opportunity interval in seconds.
	Interval int
	// Weights assigns a relative share to each generated Bitcoin node.
	Weights []int32
	// RPC adds explicitly scoped external actor/bootstrap identities.
	RPC []RPCIdentity
}

// RPCIdentity supplies one external client's static method-scoped Bitcoin authentication.
type RPCIdentity struct{ Username, Password, Methods string }

// BitcoinEnvironment exposes typed composition points without inspecting list positions.
type BitcoinEnvironment struct {
	// Resources is the complete Kubernetes resource list.
	Resources []any
	// Network is the same parent object included in Resources.
	Network *networkv1alpha1.StacksNetwork
}

// Bitcoin returns namespaced resources with separated static RPC credentials.
func Bitcoin(options BitcoinOptions) (BitcoinEnvironment, error) {
	producer, observer := token(), token()
	producerMethods := "getblockchaininfo,validateaddress,generatetoaddress"
	observerMethods := "getblockchaininfo,getblockcount,getbestblockhash"
	if options.Reorganization {
		producerMethods += ",getblockheader,getblockhash,getchaintips,invalidateblock,reconsiderblock"
		observerMethods += ",getblockheader,getblockhash,getchaintips"
	}
	extra := ""
	seen := map[string]bool{"producer": true, "observer": true}
	for _, identity := range options.RPC {
		if seen[identity.Username] || identity.Username == "" || identity.Password == "" || identity.Methods == "" || strings.ContainsAny(identity.Username+identity.Methods, "\r\n:") {
			return BitcoinEnvironment{}, fmt.Errorf("invalid or duplicate RPC identity")
		}
		seen[identity.Username] = true
		extra += auth(identity.Username, identity.Password)
	}
	for _, identity := range options.RPC {
		extra += "rpcwhitelist=" + identity.Username + ":" + identity.Methods + "\n"
	}
	configuration := "regtest=1\nserver=1\nprinttoconsole=1\ntxindex=1\ndiscover=0\ndnsseed=0\nlistenonion=0\nrpcwhitelistdefault=1\n" +
		auth("producer", producer) + auth("observer", observer) +
		"rpcwhitelist=producer:" + producerMethods + "\nrpcwhitelist=observer:" + observerMethods + "\n" +
		extra + "[regtest]\nrpcbind=0.0.0.0:18443\nrpcallowip=0.0.0.0/0\n"
	for i := range options.Weights {
		actor := "bitcoin"
		if i > 0 {
			actor = fmt.Sprintf("bitcoin-%d", i+1)
		}
		configuration += fmt.Sprintf("addnode=%s.%s.svc.cluster.local:18444\n", naming.Child(options.Name, actor), options.Namespace)
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(configuration)))
	credentials, err := json.Marshal(map[string]string{"username": "producer", "password": producer, "configDigest": digest})
	if err != nil {
		return BitcoinEnvironment{}, err
	}
	readCredentials, err := json.Marshal(map[string]string{"username": "observer", "password": observer})
	if err != nil {
		return BitcoinEnvironment{}, err
	}
	immutable := true
	nodes := make([]networkv1alpha1.BitcoinNodeTemplate, 0, len(options.Weights))
	targets := make([]bitcoinv1alpha1.ProductionTarget, 0, len(options.Weights))
	for i, weight := range options.Weights {
		actor := "bitcoin"
		if i > 0 {
			actor = fmt.Sprintf("bitcoin-%d", i+1)
		}
		nodes = append(nodes, networkv1alpha1.BitcoinNodeTemplate{Name: actor, Config: networkv1alpha1.ConfigSource{ConfigMapRef: &networkv1alpha1.ConfigObjectRef{Name: "stacks-bitcoin-rpc-config", Key: "bitcoin.conf", ExpectedDigest: digest}}})
		targets = append(targets, bitcoinv1alpha1.ProductionTarget{Name: actor, Weight: weight, Address: options.Address})
	}
	parent := &networkv1alpha1.StacksNetwork{TypeMeta: metav1.TypeMeta{APIVersion: networkv1alpha1.GroupVersion.String(), Kind: "StacksNetwork"}, ObjectMeta: metav1.ObjectMeta{Name: options.Name, Namespace: options.Namespace}, Spec: networkv1alpha1.StacksNetworkSpec{
		Defaults:               networkv1alpha1.NetworkDefaults{BitcoinImage: options.Image},
		BitcoinNodes:           nodes,
		BitcoinBlockProduction: &bitcoinv1alpha1.ProductionPolicy{Targets: targets, IntervalSeconds: int32(options.Interval)},
	}}
	objects := []any{
		&corev1.Namespace{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"}, ObjectMeta: metav1.ObjectMeta{Name: options.Namespace}},
		&corev1.ConfigMap{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}, ObjectMeta: metav1.ObjectMeta{Name: "stacks-bitcoin-rpc-config", Namespace: options.Namespace}, Immutable: &immutable, Data: map[string]string{"bitcoin.conf": configuration}},
		&corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Name: "stacks-bitcoin-production-rpc", Namespace: options.Namespace}, Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"credentials.json": credentials}},
		&corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Name: "stacks-bitcoin-observer-rpc", Namespace: options.Namespace}, Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"credentials.json": readCredentials}},
		parent,
	}
	return BitcoinEnvironment{Resources: objects, Network: parent}, nil
}

// auth returns Bitcoin Core's salted HMAC verifier, without the plaintext password.
func auth(username, password string) string {
	salt := token()
	mac := hmac.New(sha256.New, []byte(salt))
	_, _ = mac.Write([]byte(password))
	return fmt.Sprintf("rpcauth=%s:%s$%x\n", username, salt, mac.Sum(nil))
}

// token creates a fresh static credential or verifier salt.
func token() string {
	var value [32]byte
	_, err := rand.Read(value[:])
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}
