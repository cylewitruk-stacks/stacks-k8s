package foundation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"
)

func TestFoundationChartChecksEveryExistingCRD(t *testing.T) {
	chart := filepath.Join("..", "..", "..", "..", "charts", "stacks-network-foundation")
	files, err := filepath.Glob(filepath.Join(chart, "crds", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var crd struct{ Metadata struct{ Name string } }
		if err := yaml.Unmarshal(data, &crd); err != nil {
			t.Fatal(err)
		}
		names = append(names, crd.Metadata.Name)
	}
	if len(names) != 15 {
		t.Fatalf("CRD inventory: %d", len(names))
	}
	for _, incompatible := range append([]string{""}, names...) {
		t.Run("existing-"+incompatible, func(t *testing.T) {
			var mu sync.Mutex
			seen := map[string]bool{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var result any
				switch r.URL.Path {
				case "/version":
					result = map[string]string{"major": "1", "minor": "37", "gitVersion": "v1.37.0"}
				case "/api":
					result = map[string]any{"kind": "APIVersions", "apiVersion": "v1", "versions": []string{"v1"}}
				case "/apis":
					groups := []any{}
					for _, group := range []string{"apiextensions.k8s.io", "rbac.authorization.k8s.io", "apps"} {
						version := map[string]string{"groupVersion": group + "/v1", "version": "v1"}
						groups = append(groups, map[string]any{"name": group, "versions": []any{version}, "preferredVersion": version})
					}
					result = map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": groups}
				case "/api/v1", "/apis/apps/v1", "/apis/rbac.authorization.k8s.io/v1":
					resources := map[string]string{"serviceaccounts": "ServiceAccount"}
					group := "v1"
					if strings.Contains(r.URL.Path, "rbac.") {
						resources = map[string]string{"clusterroles": "ClusterRole", "clusterrolebindings": "ClusterRoleBinding"}
						group = "rbac.authorization.k8s.io/v1"
					}
					if strings.Contains(r.URL.Path, "apps/") {
						resources = map[string]string{"deployments": "Deployment"}
						group = "apps/v1"
					}
					list := []any{}
					for name, kind := range resources {
						list = append(list, map[string]any{"name": name, "kind": kind, "namespaced": !strings.HasPrefix(name, "cluster"), "verbs": []string{"get", "list"}})
					}
					result = map[string]any{"kind": "APIResourceList", "apiVersion": "v1", "groupVersion": group, "resources": list}
				case "/apis/apiextensions.k8s.io/v1":
					result = map[string]any{"kind": "APIResourceList", "apiVersion": "v1", "groupVersion": "apiextensions.k8s.io/v1", "resources": []any{map[string]any{"name": "customresourcedefinitions", "kind": "CustomResourceDefinition", "namespaced": false, "verbs": []string{"get", "list"}}}}
				default:
					const prefix = "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/"
					if !strings.HasPrefix(r.URL.Path, prefix) {
						http.NotFound(w, r)
						return
					}
					name := strings.TrimPrefix(r.URL.Path, prefix)
					mu.Lock()
					seen[name] = true
					mu.Unlock()
					version := "v1alpha2"
					if name == incompatible {
						version = "v1alpha1"
					}
					result = map[string]any{"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition", "metadata": map[string]string{"name": name}, "spec": map[string]any{"versions": []any{map[string]string{"name": version}}}}
				}
				json.NewEncoder(w).Encode(result)
			}))
			defer server.Close()
			config := clientcmdapi.Config{Clusters: map[string]*clientcmdapi.Cluster{"test": {Server: server.URL}}, Contexts: map[string]*clientcmdapi.Context{"test": {Cluster: "test"}}, CurrentContext: "test"}
			path := filepath.Join(t.TempDir(), "kubeconfig")
			if err := clientcmd.WriteToFile(config, path); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("helm", "template", "foundation", chart, "--dry-run=server", "--disable-openapi-validation", "--kubeconfig", path, "--kube-version", "1.37.0").CombinedOutput()
			if incompatible != "" {
				if err == nil || !strings.Contains(string(out), "Foundation CRD "+incompatible+" has incompatible version v1alpha1") {
					t.Fatalf("guard failed: %v %s", err, out)
				}
			} else {
				if err != nil {
					t.Fatalf("compatible inventory: %v %s", err, out)
				}
				mu.Lock()
				defer mu.Unlock()
				for _, name := range names {
					if !seen[name] {
						t.Errorf("CRD never checked: %s", name)
					}
				}
			}
		})
	}
}
