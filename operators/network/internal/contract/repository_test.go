// Package contract verifies repository-level product boundaries and examples.
package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/network"
)

func TestExamplesCompile(t *testing.T) {
	root := repositoryRoot(t)
	entries, err := filepath.Glob(filepath.Join(root, "examples", "network", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("example count = %d", len(entries))
	}
	for _, path := range entries {
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			value := &networkv1alpha1.StacksNetwork{}
			if err := yaml.UnmarshalStrict(content, value); err != nil {
				t.Fatal(err)
			}
			if value.APIVersion != networkv1alpha1.GroupVersion.String() || value.Kind != "StacksNetwork" {
				t.Fatalf("unexpected type %s %s", value.APIVersion, value.Kind)
			}
			if _, err := network.Compile(value); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMarkdownLinksResolve(t *testing.T) {
	root := repositoryRoot(t)
	link := regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range link.FindAllStringSubmatch(string(content), -1) {
			target := strings.Split(match[1], "#")[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), filepath.FromSlash(target))); err != nil {
				t.Errorf("%s: unresolved link %q", path, match[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLeafCRDSchemasDoNotDeclareDefaults(t *testing.T) {
	root := repositoryRoot(t)
	files := []string{
		"network.stacks.org_bitcoinnodes.yaml",
		"network.stacks.org_stacksnodes.yaml",
		"network.stacks.org_stackssigners.yaml",
	}
	checkedSchemas := 0
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(root, "charts", "stacks-network-operator", "crds", name))
			if err != nil {
				t.Fatal(err)
			}
			crd := &apiextensionsv1.CustomResourceDefinition{}
			if err := yaml.UnmarshalStrict(content, crd); err != nil {
				t.Fatal(err)
			}
			if crd.Kind != "CustomResourceDefinition" || len(crd.Spec.Versions) == 0 {
				t.Fatalf("%s is not a versioned CustomResourceDefinition", name)
			}
			for _, version := range crd.Spec.Versions {
				if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
					t.Fatalf("%s/%s has no structural schema", name, version.Name)
				}
				checkedSchemas++
				encoded, err := json.Marshal(version.Schema.OpenAPIV3Schema)
				if err != nil {
					t.Fatal(err)
				}
				var schema any
				if err := json.Unmarshal(encoded, &schema); err != nil {
					t.Fatal(err)
				}
				assertNoSchemaDefault(t, schema, "$")
			}
		})
	}
	if checkedSchemas != len(files) {
		t.Fatalf("checked %d schemas, want %d", checkedSchemas, len(files))
	}
}

func assertNoSchemaDefault(t *testing.T, value any, path string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "default" {
				t.Fatalf("leaf CRD schema declares an API-server default at %s.default", path)
			}
			assertNoSchemaDefault(t, child, path+"."+key)
		}
	case []any:
		for index, child := range typed {
			assertNoSchemaDefault(t, child, path+"["+strconv.Itoa(index)+"]")
		}
	}
}

func TestRepositoryLayout(t *testing.T) {
	root := repositoryRoot(t)
	required := []string{
		"apis/network/go.mod",
		"apis/network/tools/go.mod",
		"charts/stacks-network-operator/Chart.yaml",
		"charts/stacks-observability-operator/Chart.yaml",
		"contracts/actor-ports-v1.json",
		"contracts/image-id-v1.json",
		"contracts/inventory-v1.json",
		"contracts/leaf-spec-v1.json",
		"operators/network/go.mod",
		"operators/observability/go.mod",
	}
	for _, name := range required {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Errorf("required repository path %s: %v", name, err)
		}
	}
	retired := []string{
		"operators/network/api",
		"operators/network/tools",
		"stacks-network-operator",
		"stacks-observability-operator",
	}
	for _, name := range retired {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("retired root-level component path still exists: %s", name)
		}
	}
}

func TestDockerContextIsDefaultDeny(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	rules := make([]string, 0)
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rules = append(rules, line)
	}
	if len(rules) == 0 || rules[0] != "**" {
		t.Fatal("root Docker context must begin with a default-deny ** rule")
	}
	required := []string{
		"!apis/network/**",
		"!operators/network/**",
		"!operators/observability/**",
		"**/.claude",
		"**/.env",
		"**/.env.*",
	}
	lastNegation := -1
	positions := make(map[string]int, len(rules))
	for index, rule := range rules {
		positions[rule] = index
		if strings.HasPrefix(rule, "!") {
			lastNegation = index
		}
	}
	for _, expected := range required {
		if _, found := positions[expected]; !found {
			t.Errorf("root .dockerignore is missing required rule %q", expected)
		}
	}
	for _, sensitive := range []string{"**/.claude", "**/.env", "**/.env.*"} {
		if position, found := positions[sensitive]; found && position <= lastNegation {
			t.Errorf("sensitive exclusion %q must follow every allow rule", sensitive)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate contract test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
}
