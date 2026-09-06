// Command bitcoin-environment emits a fresh disposable Bitcoin-only environment with static RPC separation.
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/production"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

func main() {
	namespace := flag.String("namespace", "", "Fresh namespace dedicated to this environment (required).")
	name := flag.String("name", "bitcoin", "StacksNetwork name.")
	image := flag.String("image", "bitcoin/bitcoin:31.1@sha256:da25cedc66b1daefff9f412ee196c901a899c3fa68a33b20849c3e08b5c40d63", "Bitcoin Core image; defaults to the pinned 31.1 image index.")
	address := flag.String("address", "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn", "Regtest coinbase destination; the default is a test-only address with no provided spending key.")
	interval := flag.Int("interval-seconds", 5, "Delay between acknowledged single-block requests.")
	flag.Parse()
	if len(validation.IsDNS1123Label(*namespace)) != 0 || len(validation.IsDNS1123Label(*name)) != 0 || *interval < 1 || *interval > 86400 {
		fmt.Fprintln(os.Stderr, "namespace/name must be DNS labels and interval must be 1..86400")
		os.Exit(1)
	}
	producer, observer := token(), token()
	configuration := "regtest=1\nserver=1\nprinttoconsole=1\ntxindex=1\ndiscover=0\ndnsseed=0\nlistenonion=0\nrpcwhitelistdefault=1\n" +
		auth("producer", producer) + auth("observer", observer) +
		"rpcwhitelist=producer:getblockchaininfo,validateaddress,generatetoaddress\nrpcwhitelist=observer:getblockchaininfo,getblockcount,getbestblockhash\n" +
		"[regtest]\nrpcbind=0.0.0.0:18443\nrpcallowip=0.0.0.0/0\n"
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(configuration)))
	credentials, err := json.Marshal(production.Credentials{Username: "producer", Password: producer, ConfigDigest: digest})
	must(err)
	readCredentials, err := json.Marshal(map[string]string{"username": "observer", "password": observer})
	must(err)
	immutable := true
	objects := []any{
		&corev1.Namespace{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"}, ObjectMeta: metav1.ObjectMeta{Name: *namespace}},
		&corev1.ConfigMap{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}, ObjectMeta: metav1.ObjectMeta{Name: "stacks-bitcoin-rpc-config", Namespace: *namespace}, Immutable: &immutable, Data: map[string]string{"bitcoin.conf": configuration}},
		&corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Name: "stacks-bitcoin-production-rpc", Namespace: *namespace}, Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"credentials.json": credentials}},
		&corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Name: "stacks-bitcoin-observer-rpc", Namespace: *namespace}, Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"credentials.json": readCredentials}},
		&networkv1alpha1.StacksNetwork{TypeMeta: metav1.TypeMeta{APIVersion: networkv1alpha1.GroupVersion.String(), Kind: "StacksNetwork"}, ObjectMeta: metav1.ObjectMeta{Name: *name, Namespace: *namespace}, Spec: networkv1alpha1.StacksNetworkSpec{
			Defaults:               networkv1alpha1.NetworkDefaults{BitcoinImage: *image},
			BitcoinNodes:           []networkv1alpha1.BitcoinNodeTemplate{{Name: "bitcoin", Role: networkv1alpha1.BitcoinNodeRole("miner"), Config: networkv1alpha1.ConfigSource{ConfigMapRef: &networkv1alpha1.ConfigObjectRef{Name: "stacks-bitcoin-rpc-config", Key: "bitcoin.conf", ExpectedDigest: digest}}}},
			BitcoinBlockProduction: &bitcoinv1alpha1.ProductionPolicy{Target: "bitcoin", IntervalSeconds: int32(*interval), Address: *address},
		}},
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	must(encoder.Encode(map[string]any{"apiVersion": "v1", "kind": "List", "items": objects}))
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
	must(err)
	return hex.EncodeToString(value[:])
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
