package bitcoinconfig

import (
	"bytes"
	"strings"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestNativeOverridesPreserveRepeatedOrderAndSectionScope(t *testing.T) {
	base := []byte("regtest=1\nrpcauth=actor:a$b\ndebug=old\n[regtest]\nrpcport=18443\n")
	cfg := &common.Config{
		Overrides: &runtime.RawExtension{
			Raw: []byte(`{"debug":["net","rpc"],"dbcache":256,"logips":true,"regtest":{"maxconnections":32}}`),
		},
	}
	data, verified, err := Apply(base, nil, cfg, nil)
	if err != nil || !verified {
		t.Fatalf("apply: %v %v", verified, err)
	}
	want := "dbcache=256\ndebug=net\ndebug=rpc\nlogips=1\nregtest=1\nrpcauth=actor:a$b\n[regtest]\nm" +
		"axconnections=32\nrpcport=18443\n"
	if string(data) != want {
		t.Fatalf("rendered configuration: %s", data)
	}
	again, _, err := Apply(data, nil, cfg, nil)
	if err != nil || !bytes.Equal(data, again) {
		t.Fatal("merge is not idempotent")
	}
}

func TestProtectedAliasesAndInjectionAreRejected(t *testing.T) {
	for _, raw := range []string{
		`{"rpcport":1}`,
		`{"norpcport":1}`,
		`{"nonorpcport":1}`,
		`{"regtest":{"rpcport":1}}`,
		`{"regtest.rpcport":1}`,
		`{"RPCPort":1}`,
		`{"regtest":false}`,
		`{"addnode":["other"]}`,
		`{"noincludeconf":0}`,
		`{"walletnotify":"command"}`,
		`{"datadir":"elsewhere"}`,
		`{"debug":"net\nrpcport=1"}`,
		`{"debug":"net#other"}`,
		`{"debug":null}`,
		`{"debug":[{}]}`,
		`{"regtest":{"nested":{"debug":1}}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if err := ValidateCustomization(
				&common.Config{Overrides: &runtime.RawExtension{Raw: []byte(raw)}},
			); err == nil {
				t.Fatal("invalid override accepted")
			}
		})
	}
}

func TestCompleteDocumentAgreementAndUnverifiedBoundary(t *testing.T) {
	base := []byte("regtest=1\nrpcauth=actor:a$b\n[regtest]\nrpcport=18443\n")
	cfg := &common.Config{SecretRef: &common.SecretKeyRef{Name: "custom", Key: "bitcoin.conf"}}
	valid := append(append([]byte(nil), base...), []byte("debug=net # native comment\n")...)
	if _, verified, err := Apply(base, valid, cfg, nil); err != nil || !verified {
		t.Fatalf("valid complete document: %v", err)
	}
	for _, invalid := range []string{
		"regtest=1\n",
		string(base) + "norpcport=0\n",
		string(base) + "[main]\nrpcport=1\n",
		"regtest=1\nregtest.rpcport=18443\n",
	} {
		if _, _, err := Apply(base, []byte(invalid), cfg, nil); err == nil {
			t.Fatal("managed divergence accepted")
		}
	}
	if _, verified, err := Apply(
		base,
		[]byte(strings.ReplaceAll(string(valid), "\n", "\r\n")),
		cfg,
		nil,
	); err != nil ||
		!verified {
		t.Fatalf("native CRLF source rejected: %v", err)
	}
	cfg.Compatibility = ptr.To("Unverified")
	if _, verified, err := Apply(
		base,
		[]byte("regtest=1\n[regtest]\nrpcport=19000\n"),
		cfg,
		nil,
	); err != nil ||
		verified {
		t.Fatalf("unverified mode: %v", err)
	}
	for _, invalid := range []string{
		"includeconf=secret.conf\n",
		"nodatadir=elsewhere\n",
		"[other]\nx=1\n",
		"debug=private-canary\nnoequals\n",
	} {
		if _, _, err := Apply(
			base,
			[]byte(invalid),
			cfg,
			nil,
		); err == nil ||
			strings.Contains(err.Error(), "private-canary") {
			t.Fatal("invalid source accepted or leaked")
		}
	}
	// Numeric interactions and unknown options deliberately remain Core startup concerns.
	if _, verified, err := Apply(
		base,
		nil,
		&common.Config{Overrides: &runtime.RawExtension{Raw: []byte(`{"dbcache":"not-a-number","futureoption":true}`)}},
		nil,
	); err != nil ||
		!verified {
		t.Fatalf("syntax check claimed Core semantic validation: %v", err)
	}
}

func TestServiceSubstitutionIsDNSOnlyAndBounded(t *testing.T) {
	cfg := &common.Config{
		SecretRef:   &common.SecretKeyRef{Name: "custom", Key: "bitcoin.conf"},
		ServiceRefs: []common.ServiceRef{{Alias: "peer", Kind: "BitcoinNode", Name: "btc", Endpoint: "p2p"}},
	}
	base := []byte("regtest=1\n")
	source := []byte("regtest=1\nexperimentalpeer=${SERVICE:peer}:18444\n")
	data, _, err := Apply(base, source, cfg, map[string]string{"peer": "peer.test.svc"})
	if err != nil || !strings.Contains(string(data), "experimentalpeer=peer.test.svc:18444") {
		t.Fatalf("substitution: %v", err)
	}
	for _, hosts := range []map[string]string{nil, {"peer": "http://peer"}, {"peer": "peer\nrpcport=1"}} {
		if _, _, err := Apply(base, source, cfg, hosts); err == nil {
			t.Fatal("invalid host accepted")
		}
	}
	if _, _, err := Apply(base, bytes.Repeat([]byte("x"), MaximumBytes+1), cfg, nil); err == nil {
		t.Fatal("oversize source accepted")
	}
}
