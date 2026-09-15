package foundation

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestFoundationChart(t *testing.T) {
	chart := filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator")
	// #nosec G204 -- Fixed executable and separate arguments from the test harness; no shell evaluation.
	out, err := exec.CommandContext(t.Context(),
		"helm",
		"template",
		"foundation",
		chart,
		"--namespace",
		"foundation-system",
		"--kube-version",
		"1.37.0",
	).
		CombinedOutput()
	if err != nil {
		t.Fatalf("render: %v %s", err, out)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
	var got []string
	deployments := 0
	for {
		var obj unstructured.Unstructured
		err := decoder.Decode(&obj)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if obj.GetKind() == "ClusterRole" {
			rules, _, _ := unstructured.NestedSlice(obj.Object, "rules")
			for _, raw := range rules {
				rule := raw.(map[string]interface{})
				groups := rule["apiGroups"].([]interface{})
				resources := rule["resources"].([]interface{})
				verbs := rule["verbs"].([]interface{})
				for _, group := range groups {
					for _, resource := range resources {
						v := []string{}
						for _, verb := range verbs {
							v = append(v, verb.(string))
						}
						sort.Strings(v)
						got = append(got, group.(string)+"/"+resource.(string)+":"+strings.Join(v, ","))
					}
				}
			}
		}
		if obj.GetKind() == "Deployment" {
			deployments++
			pod, _, _ := unstructured.NestedMap(obj.Object, "spec", "template", "spec")
			if _, ok := pod["volumes"]; ok {
				t.Fatal("controller must not mount keys")
			}
			containers := pod["containers"].([]interface{})
			if len(containers) != 1 {
				t.Fatal("unexpected controller sidecars")
			}
			container := containers[0].(map[string]interface{})
			security := container["securityContext"].(map[string]interface{})
			if security["readOnlyRootFilesystem"] != true || security["allowPrivilegeEscalation"] != false {
				t.Fatal("unsafe controller security context")
			}
		}
	}
	expected := []string{}
	add := func(group, resources, verbs string) {
		for _, resource := range strings.Split(resources, ",") {
			expected = append(expected, group+"/"+resource+":"+verbs)
		}
	}
	add("network.stacks.org", "stacksnetworks", "get,list,patch,watch")
	add("network.stacks.org", "stacksnetworkparticipants", "create,delete,get,list,patch,watch")
	add("network.stacks.org", "stacksgeneses", "create,get,list,patch,watch")
	add("network.stacks.org", "stacksepochschedules", "get,list,watch")
	add(
		"network.stacks.org",
		"stacksnetworks/status,stacksnetworkparticipants/status,stacksepochschedules/status",
		"get,patch",
	)
	add(
		"bitcoin.stacks.org",
		"bitcoinnodes,bitcoinwallets,bitcoinblockschedules,bitcoinblockproductions",
		"get,list,watch",
	)
	add(
		"bitcoin.stacks.org",
		"bitcoinnodes/status,bitcoinwallets/status,bitcoinblockschedules/status,bitcoinblockproductions/status",
		"get,patch",
	)
	add(
		"stacks.stacks.org",
		"stacksaccounts,stacksnodes,stackssigners,stacksstackers,stacksfaucets,"+
			"stackscontractsets,stackstransactionproductions",
		"get,list,watch",
	)
	add("stacks.stacks.org", "stacksaccounts", "create")
	add(
		"stacks.stacks.org",
		"stacksaccounts/status,stacksnodes/status,stackssigners/status,stacksstackers/"+
			"status,stacksfaucets/status,stackscontractsets/status,"+
			"stackstransactionproductions/status",
		"get,patch",
	)
	add("", "secrets", "create,get,list,patch,watch")
	add("", "configmaps", "create,get,list,watch")
	add("", "serviceaccounts", "create,get,list,patch,watch")
	add("", "configmaps", "patch")
	add("batch", "jobs", "create,get,list,watch")
	add("rbac.authorization.k8s.io", "roles,rolebindings", "create,get,list,patch,watch")
	add("coordination.k8s.io", "leases", "create,get,list,patch,update,watch")
	add("", "events", "create,patch")
	add("apps", "statefulsets", "create,delete,get,list,patch,watch")
	add("apps", "deployments", "create,get,list,patch,watch")
	add("apps", "replicasets", "get,list,watch")
	add("", "pods", "create,delete,get,list,patch,watch")
	add("", "services", "create,get,list,patch,watch")
	add("", "persistentvolumeclaims", "get,patch")
	add("bitcoin.stacks.org", "bitcoinexecutions", "create,get,list,patch,update,watch")
	add("bitcoin.stacks.org", "bitcoininitializations", "create,get,list,patch,watch")
	add("bitcoin.stacks.org", "bitcoinexecutions/status,bitcoininitializations/status", "update")
	add("stacks.stacks.org", "stacksfaucetrequests", "get,list,watch")
	add("stacks.stacks.org", "stacksfaucetrequests/status", "patch")
	add("bitcoin.stacks.org", "bitcoinblockscheduleoverrides", "get,list,patch,watch")
	add("bitcoin.stacks.org", "bitcoinblockscheduleoverrides/status", "patch")
	sort.Strings(expected)
	sort.Strings(got)
	if !reflect.DeepEqual(expected, got) {
		t.Fatalf("rendered permissions differ\nwant %v\ngot %v", expected, got)
	}
	if deployments != 1 {
		t.Fatalf("deployment count %d", deployments)
	}
}

func TestKeyJobRulesAreNameScoped(t *testing.T) {
	in := KeyJobInput{CredentialsRef: common.SecretKeyRef{Name: "key", Key: "privateKey"}, ReportName: "report"}
	want := []rbacv1.PolicyRule{
		{
			APIGroups:     []string{""},
			Resources:     []string{"secrets"},
			ResourceNames: []string{"key"},
			Verbs:         []string{"get"},
		},
		{
			APIGroups:     []string{""},
			Resources:     []string{"configmaps"},
			ResourceNames: []string{"report"},
			Verbs:         []string{"get", "patch"},
		},
	}
	if !reflect.DeepEqual(KeyJobRules(in), want) {
		t.Fatal("import resolver grants excess authority")
	}
	in.Generate = true
	want[0].Verbs = append(want[0].Verbs, "patch")
	if !reflect.DeepEqual(KeyJobRules(in), want) {
		t.Fatal("generation grants excess authority")
	}
}

// TestObserverImageDoesNotFollowControllerImage proves independent default and explicit pins.
func TestObserverImageDoesNotFollowControllerImage(t *testing.T) {
	for _, observer := range []string{"", "observer:custom"} {
		t.Run(observer, func(t *testing.T) {
			chart := filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator")
			args := []string{
				"template",
				"network",
				chart,
				"--kube-version",
				"1.37.0",
				"--set",
				"image.tag=controller-new",
			}
			// #nosec G304 -- Test-owned repository chart path, without external input.
			data, err := os.ReadFile(filepath.Join(chart, "values.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			var values struct {
				Observer string `json:"bitcoinObserverImage"`
			}
			if err := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096).Decode(&values); err != nil {
				t.Fatal(err)
			}
			if values.Observer == "" {
				t.Fatal("chart must pin observer image")
			}
			want := values.Observer
			if observer != "" {
				args = append(args, "--set", "bitcoinObserverImage="+observer)
				want = observer
			}
			// #nosec G204 -- Fixed local chart and test-owned flags.
			out, err := exec.CommandContext(t.Context(), "helm", args...).CombinedOutput()
			if err != nil {
				t.Fatalf("render: %v %s", err, out)
			}
			if !strings.Contains(string(out), "--resolver-image=stacks-network-operator:controller-new") ||
				!strings.Contains(string(out), "--bitcoin-observer-image="+want) {
				t.Fatalf("independent image pin lost: %s", out)
			}
		})
	}
}
