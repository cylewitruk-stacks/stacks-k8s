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

func TestActionFoundationChartExactPermissions(t *testing.T) {
	for _, mode := range []struct {
		name                       string
		generation, reorganization bool
	}{{"generation", true, false}, {"reorganization", false, true}, {"both", true, true}} {
		t.Run(mode.name, func(t *testing.T) {
			chart := filepath.Join("..", "..", "..", "..", "charts", "stacks-action-operator")
			args := []string{
				"template",
				"action",
				chart,
				"--namespace",
				"test",
				"--kube-version",
				"1.37.0",
				"--set",
				"bitcoinGeneration.enabled=" + boolean(mode.generation),
				"--set",
				"bitcoinReorganization.enabled=" + boolean(mode.reorganization),
			}
			// #nosec G204 -- Fixed executable and separate arguments from the test harness; no shell evaluation.
			out, err := exec.CommandContext(t.Context(), "helm", args...).CombinedOutput()
			if err != nil {
				t.Fatalf("render %v %s", err, out)
			}
			got := []string{}
			decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
			for {
				var object unstructured.Unstructured
				err := decoder.Decode(&object)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if object.GetKind() == "ClusterRole" || object.GetKind() == "ClusterRoleBinding" {
					t.Fatal("action chart grants cluster authority")
				}
				if object.GetKind() != "Role" {
					continue
				}
				rules, _, _ := unstructured.NestedSlice(object.Object, "rules")
				for _, raw := range rules {
					rule := raw.(map[string]any)
					verbs := []string{}
					for _, v := range rule["verbs"].([]any) {
						verbs = append(verbs, v.(string))
					}
					sort.Strings(verbs)
					for _, group := range rule["apiGroups"].([]any) {
						for _, resource := range rule["resources"].([]any) {
							got = append(got, group.(string)+"/"+resource.(string)+":"+strings.Join(verbs, ","))
						}
					}
				}
			}
			expected := []string{
				"/events:create,patch",
				"network.stacks.org/stacksnetworks:get",
				"network.stacks.org/stacksnetworkparticipants:get",
				"bitcoin.stacks.org/bitcoinexecutions:get",
				"coordination.k8s.io/leases:create,get,list,patch,update,watch",
			}
			for resource, enabled := range map[string]bool{
				"bitcoinblockgenerations": mode.generation,
				"bitcoinreorganizations":  mode.reorganization,
			} {
				if enabled {
					expected = append(
						expected,
						"actions.stacks.org/"+resource+":get,list,patch,watch",
						"actions.stacks.org/"+resource+"/status:patch",
					)
				}
			}
			sort.Strings(got)
			sort.Strings(expected)
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("permissions differ\ngot%v\nwant%v", got, expected)
			}
		})
	}
}

func boolean(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
