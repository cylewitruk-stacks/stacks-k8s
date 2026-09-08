// Package rbac verifies exact installation permissions for the operator and leader election.
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

// Validate accepts one scoped or cluster-wide operator role and an installation-local leader role.
func Validate(reader io.Reader) error {
	decoder := yaml.NewYAMLOrJSONDecoder(reader, 4096)
	roles := map[string]rbacv1.Role{}
	kinds := map[string]string{}
	bindings := map[string]rbacv1.RoleBinding{}
	for {
		object := &unstructured.Unstructured{}
		if err := decoder.Decode(object); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		data, err := json.Marshal(object.Object)
		if err != nil {
			return err
		}
		switch object.GetKind() {
		case "Role", "ClusterRole":
			var role rbacv1.Role
			if err := json.Unmarshal(data, &role); err != nil {
				return err
			}
			if _, exists := roles[role.Name]; exists {
				return fmt.Errorf("duplicate role")
			}
			roles[role.Name] = role
			kinds[role.Name] = object.GetKind()
		case "RoleBinding", "ClusterRoleBinding":
			var binding rbacv1.RoleBinding
			if err := json.Unmarshal(data, &binding); err != nil {
				return err
			}
			if _, exists := bindings[binding.RoleRef.Name]; exists {
				return fmt.Errorf("duplicate binding")
			}
			if object.GetKind() == "ClusterRoleBinding" && (binding.Namespace != "" || binding.RoleRef.Kind != "ClusterRole") {
				return fmt.Errorf("invalid cluster binding scope")
			}
			if object.GetKind() == "RoleBinding" && (binding.Namespace == "" || binding.RoleRef.Kind != "Role") {
				return fmt.Errorf("invalid namespace binding scope")
			}
			bindings[binding.RoleRef.Name] = binding
		}
	}
	if len(roles) != 2 || len(bindings) != 2 {
		return fmt.Errorf("expected operator and leader role/binding pairs")
	}
	var subject *rbacv1.Subject
	seen := map[string]bool{}
	for name, role := range roles {
		component := role.Labels["app.kubernetes.io/component"]
		if seen[component] {
			return fmt.Errorf("duplicate component role")
		}
		seen[component] = true
		var expected []rbacv1.PolicyRule
		switch component {
		case "leader":
			expected = leaderRules()
			if kinds[name] != "Role" || role.Namespace == "" {
				return fmt.Errorf("leader role must be namespaced")
			}
		case "operator":
			expected = expectedRules()
			for _, rule := range role.Rules {
				if reflect.DeepEqual(rule.APIGroups, []string{"actions.stacks.org"}) {
					for _, res := range rule.Resources {
						if res == "bitcoinblockgenerations" || res == "bitcoinreorganizations" {
							expected = append(expected, rbacv1.PolicyRule{APIGroups: []string{"actions.stacks.org"}, Resources: []string{res}, Verbs: []string{"get", "list", "watch"}})
						}
					}
				}
			}
			if (kinds[name] == "ClusterRole") != (role.Namespace == "") {
				return fmt.Errorf("operator role scope mismatch")
			}
		default:
			return fmt.Errorf("unknown role component")
		}
		if !reflect.DeepEqual(normalize(role.Rules), normalize(expected)) {
			return fmt.Errorf("%s role differs from exact permission contract", component)
		}
		binding, ok := bindings[name]
		if !ok || binding.RoleRef.APIGroup != rbacv1.GroupName || binding.RoleRef.Kind != kinds[name] || binding.Namespace != role.Namespace {
			return fmt.Errorf("role binding scope mismatch")
		}
		if len(binding.Subjects) != 1 || binding.Subjects[0].Kind != "ServiceAccount" || binding.Subjects[0].Name == "" || binding.Subjects[0].Namespace == "" {
			return fmt.Errorf("binding must select one service account")
		}
		if component == "leader" && binding.Subjects[0].Namespace != role.Namespace {
			return fmt.Errorf("leader identity must belong to installation namespace")
		}
		if subject != nil && !reflect.DeepEqual(*subject, binding.Subjects[0]) {
			return fmt.Errorf("operator and election identities differ")
		}
		s := binding.Subjects[0]
		subject = &s
	}
	if !seen["operator"] || !seen["leader"] {
		return fmt.Errorf("missing installation roles")
	}
	return nil
}

// leaderRules grants only the election client's create/get/update operations.
func leaderRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "create", "update"}}}
}

// expectedRules includes the bounded permissions delegated to workers, without Secret API access.
func expectedRules() []rbacv1.PolicyRule {
	read := []string{"get", "list", "watch"}
	owned := []string{"get", "list", "watch", "create", "update", "patch", "delete"}
	compiled := []string{"get", "list", "watch", "create", "patch"}
	status := []string{"get", "patch"}
	return append([]rbacv1.PolicyRule{
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworks"}, Verbs: read},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"bitcoinnodes", "stacksnodes", "stackssigners"}, Verbs: owned},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworks/status", "bitcoinnodes/status", "stacksnodes/status", "stackssigners/status"}, Verbs: []string{"get", "update", "patch"}},
		{APIGroups: []string{"stacks.stacks.org"}, Resources: []string{"stackstransactionproductions", "stacksaccounts", "stackscontractsets", "stacksstackingparticipants"}, Verbs: compiled},
		{APIGroups: []string{"stacks.stacks.org"}, Resources: []string{"stackstransactionproductions/status", "stacksaccounts/status", "stackscontractsets/status", "stacksstackingparticipants/status"}, Verbs: status},
		{APIGroups: []string{"bitcoin.stacks.org"}, Resources: []string{"bitcoinblockproductions", "bitcoinproductiontargets"}, Verbs: compiled},
		{APIGroups: []string{"bitcoin.stacks.org"}, Resources: []string{"bitcoinblockproductions/status", "bitcoinproductiontargets/status"}, Verbs: status},
		{APIGroups: []string{""}, Resources: []string{"configmaps", "services"}, Verbs: owned},
		{APIGroups: []string{""}, Resources: []string{"serviceaccounts"}, Verbs: compiled},
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: read},
		{APIGroups: []string{"apps"}, Resources: []string{"statefulsets"}, Verbs: owned},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: compiled},
		{APIGroups: []string{"rbac.authorization.k8s.io"}, Resources: []string{"roles", "rolebindings"}, Verbs: compiled},
	}, leaderRules()...)
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
