package foundation

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestFoundationOptionalActionPermissions(t *testing.T) {
	chart := filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator")
	for _, enabled := range []bool{false, true} {
		args := []string{"template", "foundation", chart, "--kube-version", "1.37.0"}
		if enabled {
			args = append(
				args,
				"--set",
				"bitcoinActions.generationEnabled=true,bitcoinActions.reorganizationEnabled=true",
			)
		}
		// #nosec G204 -- Fixed executable and separate arguments from the test harness; no shell evaluation.
		out, err := exec.CommandContext(t.Context(), "helm", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("render %v %s", err, out)
		}
		decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
		got := []string{}
		for {
			var object unstructured.Unstructured
			err := decoder.Decode(&object)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if object.GetKind() != "ClusterRole" {
				continue
			}
			rules, _, _ := unstructured.NestedSlice(object.Object, "rules")
			for _, raw := range rules {
				rule := raw.(map[string]any)
				for _, group := range rule["apiGroups"].([]any) {
					if group != "actions.stacks.org" {
						continue
					}
					verbs := []string{}
					for _, v := range rule["verbs"].([]any) {
						verbs = append(verbs, v.(string))
					}
					sort.Strings(verbs)
					for _, resource := range rule["resources"].([]any) {
						got = append(got, resource.(string)+":"+strings.Join(verbs, ","))
					}
				}
			}
		}
		expected := []string{}
		if enabled {
			expected = []string{"bitcoinblockgenerations:get,list", "bitcoinreorganizations:get,list"}
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("optional permissions differ: %v", got)
		}
	}
}
