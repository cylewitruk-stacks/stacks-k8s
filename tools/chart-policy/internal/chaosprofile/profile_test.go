package chaosprofile

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRenderedProfileAndExactRBAC(t *testing.T) {
	chart := filepath.Join("..", "..", "..", "..", "charts", "stacks-chaos-profile")
	render := func(args ...string) ([]*unstructured.Unstructured, error) {
		data, err := exec.Command("helm", append([]string{"template", "test", chart, "--namespace", "profile-test"}, args...)...).CombinedOutput()
		if err != nil {
			return nil, err
		}
		return Decode(bytes.NewReader(data))
	}
	disabled, err := render()
	if err != nil || len(disabled) != 0 {
		t.Fatal("default chart is not disabled", err)
	}
	if _, err := render("--set", "networkDelay.enabled=true"); err == nil {
		t.Fatal("unacknowledged dependency accepted")
	}
	if _, err := render("--set", "networkDelay.enabled=true", "--set", "chaosMesh.externalVersion=2.7.0"); err == nil {
		t.Fatal("unqualified version accepted")
	}
	objects, err := render("--set", "networkDelay.enabled=true", "--set", "chaosMesh.externalVersion="+Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(objects); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{
		{"--set", "networkPartition.enabled=true"},
		{"--set", "networkPartition.enabled=true", "--set", "networkDelay.enabled=true"},
	} {
		if _, err := render(flags...); err == nil {
			t.Fatal("partition accepted without dependency acknowledgement")
		}
		enabled, err := render(append(flags, "--set", "chaosMesh.externalVersion="+Version)...)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate(enabled); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"wildcard", "status-write", "namespace", "quota", "warn-only", "token", "binding", "create-only", "other-resource", "extra-rule", "exclude-rule", "object-selector", "cluster-scope"} {
		changed := make([]*unstructured.Unstructured, len(objects))
		for i, o := range objects {
			changed[i] = o.DeepCopy()
		}
		for _, o := range changed {
			switch {
			case o.GetKind() == "ValidatingAdmissionPolicy" && (mode == "create-only" || mode == "other-resource" || mode == "extra-rule" || mode == "cluster-scope"):
				rules, _, _ := unstructured.NestedSlice(o.Object, "spec", "matchConstraints", "resourceRules")
				rule := rules[0].(map[string]any)
				switch mode {
				case "create-only":
					rule["operations"] = []any{"CREATE"}
				case "other-resource":
					rule["resources"] = []any{"networkchaos/status"}
				case "cluster-scope":
					rule["scope"] = "Cluster"
				case "extra-rule":
					rules = append(rules, rule)
				}
				_ = unstructured.SetNestedSlice(o.Object, rules, "spec", "matchConstraints", "resourceRules")
			case o.GetKind() == "ValidatingAdmissionPolicy" && mode == "exclude-rule":
				rules, _, _ := unstructured.NestedSlice(o.Object, "spec", "matchConstraints", "resourceRules")
				_ = unstructured.SetNestedSlice(o.Object, rules, "spec", "matchConstraints", "excludeResourceRules")
			case o.GetKind() == "ValidatingAdmissionPolicy" && mode == "object-selector":
				_ = unstructured.SetNestedField(o.Object, map[string]any{"matchLabels": map[string]any{"skip": "true"}}, "spec", "matchConstraints", "objectSelector")
			case o.GetKind() == "Role" && mode == "wildcard":
				_ = unstructured.SetNestedSlice(o.Object, []any{map[string]any{"apiGroups": []any{"*"}, "resources": []any{"*"}, "verbs": []any{"*"}}}, "rules")
			case o.GetKind() == "Role" && mode == "status-write":
				rules, _, _ := unstructured.NestedSlice(o.Object, "rules")
				rules[0].(map[string]any)["verbs"] = []any{"get", "list", "watch", "create", "delete", "patch"}
				_ = unstructured.SetNestedSlice(o.Object, rules, "rules")
			case o.GetKind() == "Role" && mode == "namespace":
				o.SetNamespace("other")
			case o.GetKind() == "ResourceQuota" && mode == "quota":
				_ = unstructured.SetNestedField(o.Object, "2", "spec", "hard", "count/networkchaos.chaos-mesh.org")
			case o.GetKind() == "ValidatingAdmissionPolicyBinding" && mode == "warn-only":
				_ = unstructured.SetNestedStringSlice(o.Object, []string{"Warn"}, "spec", "validationActions")
			case o.GetKind() == "ServiceAccount" && mode == "token":
				o.Object["automountServiceAccountToken"] = true
			case o.GetKind() == "RoleBinding" && mode == "binding":
				_ = unstructured.SetNestedField(o.Object, "ClusterRole", "roleRef", "kind")
			}
		}
		if err := Validate(changed); err == nil {
			t.Fatal("unsafe render accepted:", mode)
		}
	}
}

func TestSchemaRejectsUnverifiedOfflineFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crd.yaml")
	if err := os.WriteFile(path, []byte("not the upstream schema"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STACKS_CHAOS_CRD_FILE", path)
	if _, err := Schema(context.Background()); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("expected checksum error identifying the file, got %v", err)
	}
}
