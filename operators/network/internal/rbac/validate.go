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
	if len(roles) != 1 || len(bindings) != 1 {
		return fmt.Errorf("expected one Role and RoleBinding, got %d and %d", len(roles), len(bindings))
	}
	actual, expected := normalize(roles[0].Rules), normalize(expectedRules())
	if !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("controller Role differs from exact permission contract\nactual: %#v\nexpected: %#v", actual, expected)
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

func expectedRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworks"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"bitcoinnodes", "stacksnodes", "stackssigners"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworks/status", "bitcoinnodes/status", "stacksnodes/status", "stackssigners/status"}, Verbs: []string{"get", "update", "patch"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps", "services"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"statefulsets"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
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
