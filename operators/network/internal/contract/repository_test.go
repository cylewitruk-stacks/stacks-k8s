// Package contract verifies repository-level product boundaries and examples.
package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
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
		"contracts/action-lifecycle-v1.json",
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

func TestActionLifecycleDesignContract(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "contracts", "action-lifecycle-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		APIGroup             string   `json:"apiGroup"`
		APIVersion           string   `json:"apiVersion"`
		ConditionTypes       []string `json:"conditionTypes"`
		Contract             string   `json:"contract"`
		CoreSpecFields       []string `json:"coreSpecFields"`
		CoreStatusFields     []string `json:"coreStatusFields"`
		CorrelationLabel     string   `json:"correlationLabel"`
		Finalizer            string   `json:"finalizer"`
		ForbiddenSpecFields  []string `json:"forbiddenSpecFields"`
		Phases               []string `json:"phases"`
		ReasonValues         []string `json:"reasonValues"`
		SpecImmutabilityRule string   `json:"specImmutabilityRule"`
		TerminalPhases       []string `json:"terminalPhases"`
		TimeoutField         struct {
			FiniteMaximumRequired bool   `json:"finiteMaximumRequired"`
			OpenAPIFormatAllowed  bool   `json:"openAPIFormatAllowed"`
			PositiveCEL           string `json:"positiveCEL"`
			WireFormat            string `json:"wireFormat"`
			WireType              string `json:"wireType"`
		} `json:"timeoutField"`
		TimestampFields []string `json:"timestampFields"`
	}
	if err := json.Unmarshal(content, &contract); err != nil {
		t.Fatal(err)
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(content, &rawFields); err != nil {
		t.Fatal(err)
	}
	fieldNames := make([]string, 0, len(rawFields))
	for name := range rawFields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	assertStringsEqual(t, "contract fields", fieldNames, []string{"apiGroup", "apiVersion", "conditionTypes", "contract", "coreSpecFields", "coreStatusFields", "correlationLabel", "finalizer", "forbiddenSpecFields", "phases", "reasonValues", "specImmutabilityRule", "terminalPhases", "timeoutField", "timestampFields"})
	assertStringsEqual(t, "phases", contract.Phases, []string{"Pending", "Admitted", "Active", "Recovering", "Completed", "Recovered", "Failed", "Inconclusive"})
	assertStringsEqual(t, "terminal phases", contract.TerminalPhases, []string{"Completed", "Recovered", "Failed", "Inconclusive"})
	assertStringsEqual(t, "condition types", contract.ConditionTypes, []string{"Admitted", "Progressing", "EffectObserved", "CleanupComplete"})
	assertStringsEqual(t, "core spec fields", contract.CoreSpecFields, []string{"networkRef", "timeout"})
	assertStringsEqual(t, "core status fields", contract.CoreStatusFields, []string{"observedGeneration", "phase", "admittedAt", "startedAt", "expiresAt", "finishedAt", "correlationID", "admittedNetwork", "admittedTarget", "admittedPolicy", "conditions"})
	assertStringsEqual(t, "forbidden spec fields", contract.ForbiddenSpecFields, []string{"actions", "dependsOn", "executionPlan", "reduction", "replay", "scenario", "schedule", "stages", "steps", "workflow"})
	assertStringsEqual(t, "reason values", contract.ReasonValues, []string{"TargetNotReady", "TargetBusy", "PolicyUnavailable", "AdmissionSucceeded", "ActionApplying", "EffectConfirmed", "RecoveryInProgress", "ActionCompleted", "ActionRecovered", "RequestInvalid", "IdentityDiverged", "DeadlineExceeded", "MechanismFailed", "EffectUncertain", "CleanupUncertain"})
	assertStringsEqual(t, "timestamp fields", contract.TimestampFields, []string{"admittedAt", "startedAt", "expiresAt", "finishedAt"})
	if contract.Contract != "actions.stacks.org/lifecycle/v1alpha1" || contract.APIGroup != "actions.stacks.org" || contract.APIVersion != "v1alpha1" {
		t.Fatalf("unexpected action API contract identity: %#v", contract)
	}
	if contract.CorrelationLabel != "actions.stacks.org/correlation-id" || contract.Finalizer != "actions.stacks.org/action-cleanup" || contract.SpecImmutabilityRule != "self == oldSelf" {
		t.Fatalf("unexpected action lifecycle convention: %#v", contract)
	}
	if contract.TimeoutField.WireType != "string" || contract.TimeoutField.WireFormat != "kubernetes-duration" || contract.TimeoutField.PositiveCEL != "duration(self) > duration('0s')" || !contract.TimeoutField.FiniteMaximumRequired || contract.TimeoutField.OpenAPIFormatAllowed {
		t.Fatalf("unexpected timeout field contract: %#v", contract.TimeoutField)
	}

	documentBytes, err := os.ReadFile(filepath.Join(root, "docs", "design", "actions.md"))
	if err != nil {
		t.Fatal(err)
	}
	document := string(documentBytes)
	documentedValues := make([]string, 0, len(contract.Phases)+len(contract.ConditionTypes)+len(contract.CoreSpecFields)+len(contract.CoreStatusFields)+len(contract.ForbiddenSpecFields)+len(contract.ReasonValues)+len(contract.TimestampFields))
	documentedValues = append(documentedValues, contract.Phases...)
	documentedValues = append(documentedValues, contract.ConditionTypes...)
	documentedValues = append(documentedValues, contract.CoreSpecFields...)
	documentedValues = append(documentedValues, contract.CoreStatusFields...)
	documentedValues = append(documentedValues, contract.ForbiddenSpecFields...)
	documentedValues = append(documentedValues, contract.ReasonValues...)
	documentedValues = append(documentedValues, contract.TimestampFields...)
	for _, value := range documentedValues {
		if !strings.Contains(document, "`"+value+"`") {
			t.Errorf("normative action document does not represent %q", value)
		}
	}
	for _, value := range []string{contract.Contract, contract.CorrelationLabel, contract.Finalizer, contract.SpecImmutabilityRule, contract.TimeoutField.WireType, contract.TimeoutField.WireFormat, contract.TimeoutField.PositiveCEL} {
		if !strings.Contains(document, value) {
			t.Errorf("normative action document does not represent %q", value)
		}
	}
	for _, convention := range []string{"metav1.Duration", "finite upper bound", "No lifecycle fact may exist only in `phase`"} {
		if !strings.Contains(document, convention) {
			t.Errorf("normative action document does not represent %q", convention)
		}
	}

	for _, name := range []string{"actions.md", "bitcoin-lifecycle.md", "protocol-actions.md"} {
		value, err := os.ReadFile(filepath.Join(root, "docs", "design", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, stale := range []string{"immutable after admission", "become immutable after", "becomes immutable after"} {
			if strings.Contains(strings.ToLower(string(value)), stale) {
				t.Errorf("%s retains superseded mutability phrase %q", name, stale)
			}
		}
	}

	bitcoinBytes, err := os.ReadFile(filepath.Join(root, "docs", "design", "bitcoin-lifecycle.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{"rpcProfileRef", "RPC RPC"} {
		if strings.Contains(string(bitcoinBytes), stale) {
			t.Errorf("bitcoin action design retains superseded text %q", stale)
		}
	}
}

func assertStringsEqual(t *testing.T, name string, actual, expected []string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("%s = %v, want %v", name, actual, expected)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("%s = %v, want %v", name, actual, expected)
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
