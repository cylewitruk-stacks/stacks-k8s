// Package rbac verifies the chart's exact controller permissions.
package rbac

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Validate requires exactly the namespaced privileges used by the controllers.
func Validate(reader io.Reader) error {
	decoder := yaml.NewYAMLOrJSONDecoder(reader, 4096)
	var roles []rbacv1.Role
	var bindings []rbacv1.RoleBinding
	for {
		object := &unstructured.Unstructured{}
		if err := decoder.Decode(object); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("decode rendered chart: %w", err)
		}
		if object.GetKind() == "" {
			continue
		}
		encoded, err := json.Marshal(object.Object)
		if err != nil {
			return err
		}
		switch object.GetKind() {
		case "Role":
			var role rbacv1.Role
			if err := json.Unmarshal(encoded, &role); err != nil {
				return err
			}
			roles = append(roles, role)
		case "RoleBinding":
			var binding rbacv1.RoleBinding
			if err := json.Unmarshal(encoded, &binding); err != nil {
				return err
			}
			bindings = append(bindings, binding)
		case "ClusterRole", "ClusterRoleBinding":
			return fmt.Errorf("chart must not render cluster-scoped RBAC kind %s", object.GetKind())
		}
	}
	if (len(roles) < 1 || len(roles) > 3) || len(bindings) != len(roles) {
		return fmt.Errorf("expected topology and optional production Role/RoleBinding pairs, got %d and %d", len(roles), len(bindings))
	}
	seen := map[string]bool{}
	serviceAccounts := map[string]bool{}
	for _, role := range roles {
		component := role.Labels["app.kubernetes.io/component"]
		if seen[component] {
			return fmt.Errorf("duplicate controller Role")
		}
		seen[component] = true
		rules := expectedRules()
		if component == "bitcoin-production" {
			rules = productionRules()
		} else if component == "stacks-transactions" {
			rules = transactionRules()
		} else if component != "" {
			return fmt.Errorf("unknown controller Role")
		}
		actual, expected := normalize(role.Rules), normalize(rules)
		if !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("controller Role differs from exact permission contract\nactual: %#v\nexpected: %#v", actual, expected)
		}
		var binding rbacv1.RoleBinding
		count := 0
		for _, candidate := range bindings {
			if candidate.RoleRef.Name == role.Name {
				binding = candidate
				count++
			}
		}
		if count != 1 || binding.Namespace != role.Namespace || binding.RoleRef.APIGroup != rbacv1.GroupName || binding.RoleRef.Kind != "Role" {
			return fmt.Errorf("RoleBinding does not reference the rendered Role")
		}
		if len(binding.Subjects) != 1 || binding.Subjects[0].Kind != "ServiceAccount" || binding.Subjects[0].Name == "" || binding.Subjects[0].Namespace != binding.Namespace {
			return fmt.Errorf("RoleBinding must select one same-namespace ServiceAccount")
		}
		if serviceAccounts[binding.Subjects[0].Name] {
			return fmt.Errorf("topology and production must use separate ServiceAccounts")
		}
		serviceAccounts[binding.Subjects[0].Name] = true
	}
	if !seen[""] {
		return fmt.Errorf("topology Role is missing")
	}
	return nil
}

func expectedRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"stacks.stacks.org"}, Resources: []string{"stackstransactionproductions"}, Verbs: []string{"get", "list", "watch", "create", "patch"}},
		{APIGroups: []string{"bitcoin.stacks.org"}, Resources: []string{"bitcoinblockproductions"}, Verbs: []string{"get", "list", "watch", "create", "patch"}},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworks"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"bitcoinnodes", "stacksnodes", "stackssigners"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworks/status", "bitcoinnodes/status", "stacksnodes/status", "stackssigners/status"}, Verbs: []string{"get", "update", "patch"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps", "services"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"statefulsets"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
	}
}

// productionRules excludes workload mutation and Secret API access.
func productionRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"bitcoin.stacks.org"}, Resources: []string{"bitcoinblockproductions"}, Verbs: []string{"get", "list", "watch", "patch"}},
		{APIGroups: []string{"bitcoin.stacks.org"}, Resources: []string{"bitcoinblockproductions/status"}, Verbs: []string{"get", "patch"}},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworks"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"bitcoinnodes"}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"apps"}, Resources: []string{"statefulsets"}, Verbs: []string{"get"}},
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
	}
}

func normalize(rules []rbacv1.PolicyRule) []rbacv1.PolicyRule {
	result := make([]rbacv1.PolicyRule, len(rules))
	copy(result, rules)
	for index := range result {
		sort.Strings(result[index].APIGroups)
		sort.Strings(result[index].Resources)
		sort.Strings(result[index].Verbs)
		sort.Strings(result[index].ResourceNames)
		sort.Strings(result[index].NonResourceURLs)
	}
	sort.Slice(result, func(i, j int) bool {
		left, _ := json.Marshal(result[i])
		right, _ := json.Marshal(result[j])
		return string(left) < string(right)
	})
	return result
}

// transactionRules preserves the separate worker's read-only actor and no-Secret-API boundary.
func transactionRules() []rbacv1.PolicyRule {
	rules := productionRules()
	for i := range rules {
		if rules[i].APIGroups[0] == "bitcoin.stacks.org" {
			rules[i].APIGroups = []string{"stacks.stacks.org"}
			for j, value := range rules[i].Resources {
				if value == "bitcoinblockproductions" {
					rules[i].Resources[j] = "stackstransactionproductions"
				} else {
					rules[i].Resources[j] = "stackstransactionproductions/status"
				}
			}
		}
		if rules[i].Resources[0] == "configmaps" {
			rules[i].Verbs = []string{"list"}
		}
		if rules[i].Resources[0] == "bitcoinnodes" {
			rules[i].Resources = []string{"stacksnodes"}
		}
	}
	return rules
}
