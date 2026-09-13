package participantworkload

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload/stacksconfig"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// stacksResolverFixture pins public genesis, node identity and private resolver artifacts.
func stacksResolverFixture(t *testing.T) (client.Client, StacksConfigInput) {
	t.Helper()
	p := participantFixture()
	p.Spec.Kind = "StacksNode"
	config := &corev1.Secret{ObjectMeta: objectMeta(p, "config", "support")}
	config.UID = "config-uid"
	event := &corev1.Secret{ObjectMeta: objectMeta(p, "event-auth", "support")}
	event.UID = "event-uid"
	key := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "account-key", Namespace: p.Namespace, UID: "key-uid"},
		Immutable:  ptr.To(true),
		Data:       map[string][]byte{"privateKey": []byte(strings.Repeat("0", 63) + "1")},
	}
	btc := p.DeepCopy()
	btc.UID = "bitcoin-uid"
	btc.Spec.Kind = "BitcoinNode"
	rpc := &corev1.Secret{
		ObjectMeta: objectMeta(btc, "rpc-actor", "support"),
		Immutable:  ptr.To(true),
		Data:       map[string][]byte{"username": []byte("actor"), "password": []byte("restricted-rpc-password")},
	}
	rpc.UID = "rpc-uid"
	report := &corev1.ConfigMap{ObjectMeta: objectMeta(p, "report", "support")}
	report.UID = "report-uid"
	chain := api.Chain{
		PoX:       api.PoX{RewardCycleLength: 20, PrepareLength: 5},
		Contracts: api.ContractBindings{Deployer: "ST000000000000000000002AMW42H"},
	}
	for i, name := range []string{
		"1.0",
		"2.0",
		"2.05",
		"2.1",
		"2.2",
		"2.3",
		"2.4",
		"2.5",
		"3.0",
		"3.1",
		"3.2",
		"3.3",
		"3.4",
		"4.0",
	} {
		chain.Epochs = append(chain.Epochs, api.Epoch{Name: name, StartHeight: int64(i * 10)})
	}
	public, _ := identity.FromPrivate(string(key.Data["privateKey"]))
	in := StacksConfigInput{
		Namespace:      p.Namespace,
		ParticipantUID: p.UID,
		Kind:           p.Spec.Kind,
		PolicyDigest:   "sha256:policy",
		Genesis: common.Binding{
			Kind:        "StacksGenesis",
			Name:        "genesis",
			UID:         "genesis-uid",
			Fingerprint: digest(chain),
		},
		Node: stacksconfig.NodeParameters{
			Name:        p.Spec.ParticipantName,
			Chain:       chain,
			P2PAddress:  "10.96.0.15",
			RPCHost:     "node.test.svc",
			BitcoinHost: "btc.test.svc",
		},
		Identity:  common.PublicIdentity{Address: public.Address, PublicKey: public.PublicKey},
		Key:       PrivateInput{Binding: *binding("Secret", key), Key: "privateKey"},
		ActorRPC:  &PrivateInput{Binding: *binding("Secret", rpc), OwnerUID: btc.UID},
		EventAuth: PrivateInput{Binding: *binding("Secret", event), OwnerUID: p.UID, Key: "token"},
		Config:    *binding("Secret", config),
		Report:    *binding("ConfigMap", report),
	}
	return testClient(t, config, event, key, rpc, report), in
}

func TestStacksResolverRetainsTokenAcrossRetriesAndConfigurationRolls(t *testing.T) {
	ctx := context.Background()
	for _, commit := range []bool{false, true} {
		base, in := stacksResolverFixture(t)
		if err := RunStacksConfigResolver(ctx, &lostCredentialWrite{Client: base, commit: commit}, in); err == nil {
			t.Fatal("lost event-token acknowledgement not surfaced")
		}
		var event corev1.Secret
		if err := base.Get(
			ctx,
			client.ObjectKey{Namespace: in.Namespace, Name: in.EventAuth.Binding.Name},
			&event,
		); err != nil {
			t.Fatal(err)
		}
		first := string(event.Data["token"])
		if err := RunStacksConfigResolver(ctx, base, in); err != nil {
			t.Fatal(err)
		}
		if err := base.Get(ctx, client.ObjectKeyFromObject(&event), &event); err != nil {
			t.Fatal(err)
		}
		if commit && first != string(event.Data["token"]) {
			t.Fatal("retry regenerated committed node event token")
		}
		var report corev1.ConfigMap
		if err := base.Get(ctx, client.ObjectKey{
			Namespace: in.Namespace,
			Name:      in.Report.Name,
		}, &report); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(report.Data["report.json"], string(event.Data["token"])) ||
			strings.Contains(report.Data["report.json"], "restricted-rpc-password") {
			t.Fatal("public report contains credentials")
		}
		var result StacksConfigReport
		if err := json.Unmarshal(
			[]byte(report.Data["report.json"]),
			&result,
		); err != nil || !result.Verified || result.GenesisDigest != in.Genesis.Fingerprint ||
			result.Identity != in.Identity {
			t.Fatalf("public agreement missing: %v", err)
		}
		if err := RunStacksConfigResolver(ctx, base, in); err != nil {
			t.Fatal(err)
		}
		p := participantFixture()
		p.Spec.Kind = "StacksNode"
		config := &corev1.Secret{ObjectMeta: objectMeta(p, "config-roll", "support")}
		config.UID = "config-roll-uid"
		rolledReport := &corev1.ConfigMap{ObjectMeta: objectMeta(p, "report-roll", "support")}
		rolledReport.UID = "report-roll-uid"
		if err := base.Create(ctx, config); err != nil {
			t.Fatal(err)
		}
		if err := base.Create(ctx, rolledReport); err != nil {
			t.Fatal(err)
		}
		in.Config = *binding("Secret", config)
		in.Report = *binding("ConfigMap", rolledReport)
		in.Node.SignerHost = "new-signer.test.svc"
		if err := RunStacksConfigResolver(ctx, base, in); err != nil {
			t.Fatal(err)
		}
		before := string(event.Data["token"])
		if err := base.Get(ctx, client.ObjectKeyFromObject(&event), &event); err != nil {
			t.Fatal(err)
		}
		if before != string(event.Data["token"]) {
			t.Fatal("ordinary configuration roll rotated event token")
		}
		in.EventAuth.Binding.UID = "replacement"
		if err := RunStacksConfigResolver(ctx, base, in); err == nil {
			t.Fatal("accepted same-name event replacement")
		}
	}
}

func TestSignerResolverReadsOnlyPairedCredentialsAndNoAdministration(t *testing.T) {
	ctx := context.Background()
	c, node := stacksResolverFixture(t)
	if err := RunStacksConfigResolver(ctx, c, node); err != nil {
		t.Fatal(err)
	}
	p := participantFixture()
	p.Spec.Kind = "StacksSigner"
	p.UID = "signer-uid"
	config := &corev1.Secret{ObjectMeta: objectMeta(p, "config", "support")}
	config.UID = "signer-config-uid"
	report := &corev1.ConfigMap{ObjectMeta: objectMeta(p, "report", "support")}
	report.UID = "signer-report-uid"
	if err := c.Create(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, report); err != nil {
		t.Fatal(err)
	}
	in := node
	in.Kind = "StacksSigner"
	in.ParticipantUID = p.UID
	in.Config = *binding("Secret", config)
	in.Report = *binding("ConfigMap", report)
	in.ActorRPC = nil
	in.NodeHost = "node.test.svc"
	if err := RunStacksConfigResolver(ctx, c, in); err != nil {
		t.Fatal(err)
	}
	for _, rule := range StacksConfigRules(in) {
		for _, verb := range rule.Verbs {
			if verb != "get" {
				for _, name := range rule.ResourceNames {
					if name == in.Key.Binding.Name || name == in.EventAuth.Binding.Name {
						t.Fatal("signer resolver can alter key or node authentication")
					}
				}
			}
		}
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(config), config); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config.Data["config.toml"]), "restricted-rpc-password") ||
		strings.Contains(string(config.Data["config.toml"]), "pox") {
		t.Fatal("consensus signer received administration or Bitcoin control configuration")
	}
	in.Key.Binding.UID = "replacement"
	if err := RunStacksConfigResolver(ctx, c, in); err == nil {
		t.Fatal("same-name signer key replacement accepted")
	}
}
