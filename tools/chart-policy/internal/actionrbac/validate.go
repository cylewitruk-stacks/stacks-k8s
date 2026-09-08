// Package actionrbac verifies the action chart's exact permissions.
package actionrbac

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

// Validate requires exactly the topology-read, status, and leader-election privileges used by the controller.
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
	if len(roles) != 1 || len(bindings) != 1 {
		return fmt.Errorf("expected one Role and RoleBinding, got %d and %d", len(roles), len(bindings))
	}
	generation, reorganization := false, false
	for _, rule := range roles[0].Rules {
		for _, resource := range rule.Resources {
			generation = generation || resource == "bitcoinblockgenerations"
			reorganization = reorganization || resource == "bitcoinreorganizations"
		}
	}
	if !generation && !reorganization {
		return fmt.Errorf("no action permissions")
	}
	if !reflect.DeepEqual(normalize(roles[0].Rules), normalize(expectedRules(generation, reorganization))) {
		return fmt.Errorf("controller Role differs from the exact permission contract")
	}
	binding := bindings[0]
	if binding.RoleRef.APIGroup != rbacv1.GroupName || binding.RoleRef.Kind != "Role" || binding.RoleRef.Name != roles[0].Name {
		return fmt.Errorf("RoleBinding does not reference the rendered Role")
	}
	if len(binding.Subjects) != 1 || binding.Subjects[0].Kind != "ServiceAccount" || binding.Subjects[0].Name == "" || binding.Subjects[0].Namespace != binding.Namespace {
		return fmt.Errorf("RoleBinding must select one same-namespace ServiceAccount")
	}
	return nil
}

// expectedRules keeps the lifecycle writer separate from the RPC executor.
func expectedRules(generation, reorganization bool) []rbacv1.PolicyRule {
	rules := []rbacv1.PolicyRule{
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworks"}, Verbs: []string{"get"}},
		{APIGroups: []string{"bitcoin.stacks.org"}, Resources: []string{"bitcoinblockproductions", "bitcoinproductiontargets"}, Verbs: []string{"get"}},
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
	}
	for resource, enabled := range map[string]bool{"bitcoinblockgenerations": generation, "bitcoinreorganizations": reorganization} {
		if enabled {
			rules = append(rules,
				rbacv1.PolicyRule{APIGroups: []string{"actions.stacks.org"}, Resources: []string{resource}, Verbs: []string{"get", "list", "watch", "patch"}},
				rbacv1.PolicyRule{APIGroups: []string{"actions.stacks.org"}, Resources: []string{resource + "/status"}, Verbs: []string{"get", "patch"}})
		}
	}
	return rules
}

func normalize(rules []rbacv1.PolicyRule) []rbacv1.PolicyRule {
	result := append([]rbacv1.PolicyRule(nil), rules...)
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
