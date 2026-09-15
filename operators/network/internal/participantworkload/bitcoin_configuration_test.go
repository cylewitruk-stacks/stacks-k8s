package participantworkload

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// bitcoinCustomFixture supplies exact private resolver inputs with stable credential values.
func bitcoinCustomFixture(t *testing.T) (client.Client, BitcoinConfigInput, *corev1.Secret) {
	t.Helper()
	p := participantFixture()
	config := &corev1.Secret{ObjectMeta: objectMeta(p, "config", "support")}
	config.UID = "config-uid"
	control := &corev1.Secret{
		ObjectMeta: objectMeta(p, "control", "support"),
		Immutable:  ptr.To(true),
		Data:       map[string][]byte{"username": []byte("control"), "password": []byte(strings.Repeat("a", 64))},
	}
	control.UID = "control-uid"
	actor := &corev1.Secret{
		ObjectMeta: objectMeta(p, "rpc", "support"),
		Immutable:  ptr.To(true),
		Data:       map[string][]byte{"username": []byte("actor"), "password": []byte(strings.Repeat("b", 64))},
	}
	actor.UID = "actor-uid"
	report := &corev1.ConfigMap{ObjectMeta: objectMeta(p, "report", "support")}
	report.UID = "report-uid"
	rendered, err := renderConfig(nil, actor, control, nil)
	if err != nil {
		t.Fatal(err)
	}
	custom := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "custom", Namespace: p.Namespace, UID: "custom-uid"},
		Immutable:  ptr.To(true),
		Data:       map[string][]byte{"bitcoin.conf": []byte(rendered + "debug=private-canary\n")},
	}
	in := BitcoinConfigInput{
		Namespace:          p.Namespace,
		ParticipantUID:     p.UID,
		PolicyDigest:       "policy",
		Config:             *binding("Secret", config),
		ControlCredentials: *binding("Secret", control),
		ActorCredentials:   *binding("Secret", actor),
		Report:             *binding("ConfigMap", report),
		Customization:      &common.Config{SecretRef: &common.SecretKeyRef{Name: custom.Name, Key: "bitcoin.conf"}},
		Custom:             &PrivateInput{Binding: *binding("Secret", custom), Key: "bitcoin.conf"},
	}
	return testClient(t, config, control, actor, report, custom), in, custom
}

func TestBitcoinPrivateConfigurationExactSourceAndReport(t *testing.T) {
	for _, mode := range []string{
		"valid",
		"replacement",
		"mutable",
		"key-missing",
		"mismatch",
		"divergent",
		"unverified",
	} {
		t.Run(mode, func(t *testing.T) {
			c, in, custom := bitcoinCustomFixture(t)
			switch mode {
			case "replacement":
				in.Custom.Binding.UID = "old-uid"
			case "mutable":
				custom.Immutable = ptr.To(false)
			case "key-missing":
				delete(custom.Data, "bitcoin.conf")
			case "mismatch":
				in.Custom.Key = "other"
			case "divergent", "unverified":
				custom.Data["bitcoin.conf"] = []byte("regtest=1\nserver=1\n")
				if mode == "unverified" {
					in.Customization.Compatibility = ptr.To("Unverified")
				}
			}
			if err := c.Update(context.Background(), custom); err != nil {
				t.Fatal(err)
			}
			err := RunBitcoinConfigResolver(context.Background(), c, in)
			if mode != "valid" && mode != "unverified" {
				if err == nil || strings.Contains(err.Error(), "private-canary") {
					t.Fatalf("invalid source: %v", err)
				}
				var report corev1.ConfigMap
				if getErr := c.Get(
					context.Background(),
					client.ObjectKey{Namespace: in.Namespace, Name: in.Report.Name},
					&report,
				); getErr != nil ||
					report.Data["report.json"] != "" {
					t.Fatalf("invalid source published agreement: %v", getErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var report corev1.ConfigMap
			if err := c.Get(
				context.Background(),
				client.ObjectKey{Namespace: in.Namespace, Name: in.Report.Name},
				&report,
			); err != nil {
				t.Fatal(err)
			}
			var result BitcoinConfigReport
			if err := json.Unmarshal([]byte(report.Data["report.json"]), &result); err != nil {
				t.Fatal(err)
			}
			if result.Verified != (mode == "valid") || result.InputDigest != digest(in) ||
				strings.Contains(report.Data["report.json"], "private-canary") {
				t.Fatal("incorrect public agreement report")
			}
			if err := RunBitcoinConfigResolver(context.Background(), c, in); err != nil {
				t.Fatal(err)
			}
			if err := c.Delete(context.Background(), custom); err != nil {
				t.Fatal(err)
			}
			custom.ResourceVersion = ""
			custom.UID = "replacement-uid"
			if err := c.Create(context.Background(), custom); err != nil {
				t.Fatal(err)
			}
			if err := RunBitcoinConfigResolver(context.Background(), c, in); err == nil {
				t.Fatal("completed report bypassed current source identity")
			}
		})
	}
}

func TestBitcoinCustomResolverPermissions(t *testing.T) {
	_, in, _ := bitcoinCustomFixture(t)
	reads := 0
	for _, rule := range BitcoinConfigRules(in) {
		for _, name := range rule.ResourceNames {
			if name == "custom" {
				reads++
			}
			if name == "custom" && (len(rule.Verbs) != 1 || rule.Verbs[0] != "get") {
				t.Fatal("custom source is writable")
			}
		}
	}
	if reads != 1 {
		t.Fatal("custom source must have exactly one named read grant")
	}
}

// metadataOnlyReader makes an operator-side private data read fail the test.
type metadataOnlyReader struct{ client.Reader }

func (r metadataOnlyReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if _, ok := obj.(*corev1.Secret); ok {
		return fmt.Errorf("operator attempted private Secret read")
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func TestBitcoinCustomSourceMetadataRejectsReplacement(t *testing.T) {
	c, in, custom := bitcoinCustomFixture(t)
	r := Reconciler{Client: c, Reader: metadataOnlyReader{c}}
	if err := r.privateMetadata(context.Background(), in.Namespace, in.Custom.Binding, ""); err != nil {
		t.Fatal(err)
	}
	// Metadata checks use admitted UIDs independently of Secret values.
	if err := c.Delete(context.Background(), custom); err != nil {
		t.Fatal(err)
	}
	custom.ResourceVersion = ""
	custom.UID = "other"
	if err := c.Create(context.Background(), custom); err != nil {
		t.Fatal(err)
	}
	if err := r.privateMetadata(context.Background(), in.Namespace, in.Custom.Binding, ""); err == nil {
		t.Fatal("replacement source accepted")
	}
}
