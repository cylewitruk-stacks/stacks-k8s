// Package rbac verifies the chart's exact observation permissions.
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
	if !reflect.DeepEqual(normalize(roles[0].Rules), normalize(expectedRules())) {
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

func expectedRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"observation.stacks.org"}, Resources: []string{"networkobservations"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"observation.stacks.org"}, Resources: []string{"networkobservations/status"}, Verbs: []string{"get", "update", "patch"}},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworks"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworkparticipants"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{""}, Resources: []string{"pods", "services", "configmaps"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"apps"}, Resources: []string{"statefulsets"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
	}
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
