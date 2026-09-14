// Package rbac verifies the chart's exact observation permissions.
package rbac

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Validate requires exactly the topology-read, status, and leader-election privileges used by the controller.
func Validate(reader io.Reader) error {
	decoder := yaml.NewYAMLOrJSONDecoder(reader, 4096)
	var watched map[string]bool
	var roles []rbacv1.Role
	var bindings []rbacv1.RoleBinding
	for {
		object := &unstructured.Unstructured{}
		if err := decoder.Decode(object); err != nil {
			if errors.Is(err, io.EOF) {
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
		case "Deployment":
			var deployment appsv1.Deployment
			if err := json.Unmarshal(encoded, &deployment); err != nil {
				return err
			}
			if watched != nil {
				return fmt.Errorf("multiple manager Deployments")
			}
			watched = map[string]bool{}
			for _, container := range deployment.Spec.Template.Spec.Containers {
				for _, arg := range container.Args {
					if names, ok := strings.CutPrefix(arg, "--watch-namespace="); ok {
						for _, name := range strings.Split(names, ",") {
							watched[name] = true
						}
					}
				}
			}
			if len(watched) == 0 {
				return fmt.Errorf("missing explicit namespace enrollment")
			}
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
	if len(roles) == 0 || len(roles) != len(bindings) {
		return fmt.Errorf("expected matched namespace Roles and bindings")
	}
	matched := map[string]bool{}
	for _, role := range roles {
		expected := expectedRules()
		if watched != nil && !watched[role.Namespace] {
			expected = installationRules()
		}
		for _, binding := range bindings {
			if binding.Namespace == role.Namespace && binding.RoleRef.Name == role.Name && len(binding.Subjects) == 1 &&
				binding.Subjects[0].Namespace == role.Namespace {
				expected = append(expected, leaderRules()...)
			}
		}
		if !reflect.DeepEqual(normalize(role.Rules), normalize(expected)) {
			return fmt.Errorf("controller Role differs from exact enrolled-namespace contract")
		}
		key := role.Namespace + "/" + role.Name
		if matched[key] {
			return fmt.Errorf("duplicate Role")
		}
		matched[key] = true
	}
	installationIdentity := ""
	for _, binding := range bindings {
		key := binding.Namespace + "/" + binding.RoleRef.Name
		if binding.RoleRef.APIGroup != rbacv1.GroupName || binding.RoleRef.Kind != "Role" || !matched[key] {
			return fmt.Errorf("RoleBinding does not reference a unique rendered Role")
		}
		delete(matched, key)
		if len(binding.Subjects) != 1 || binding.Subjects[0].Kind != "ServiceAccount" ||
			binding.Subjects[0].Name == "" || binding.Subjects[0].Namespace == "" {
			return fmt.Errorf("invalid operator identity")
		}
		identity := binding.Subjects[0].Namespace + "/" + binding.Subjects[0].Name
		if installationIdentity != "" && installationIdentity != identity {
			return fmt.Errorf("bindings disagree on operator identity")
		}
		installationIdentity = identity
	}

	return nil
}

func expectedRules() []rbacv1.PolicyRule {
	read := []string{"get", "list", "watch"}
	manage := []string{"get", "list", "watch", "create", "patch", "update", "delete"}
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"observation.stacks.org"},
			Resources: []string{"networkobservations", "networktelemetries"},
			Verbs:     read,
		},
		{
			APIGroups: []string{"observation.stacks.org"},
			Resources: []string{"networkobservations/status"},
			Verbs:     []string{"get", "update", "patch"},
		},
		{
			APIGroups: []string{"observation.stacks.org"},
			Resources: []string{"networktelemetries/status"},
			Verbs:     []string{"patch"},
		},
		{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworks"}, Verbs: read},
		{
			APIGroups: []string{"network.stacks.org"},
			Resources: []string{"stacksnetworkparticipants", "stacksgeneses"},
			Verbs:     read,
		},
		{APIGroups: []string{""}, Resources: []string{"pods", "services", "events"}, Verbs: read},
		{APIGroups: []string{""}, Resources: []string{"configmaps", "serviceaccounts"}, Verbs: manage},
		{APIGroups: []string{"apps"}, Resources: []string{"statefulsets"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "daemonsets"}, Verbs: manage},
		{APIGroups: []string{"rbac.authorization.k8s.io"}, Resources: []string{"roles", "rolebindings"}, Verbs: manage},
		{APIGroups: []string{"bitcoin.stacks.org"}, Resources: []string{"bitcoinexecutions"}, Verbs: read},
		{
			APIGroups: []string{"actions.stacks.org"},
			Resources: []string{"bitcoinblockgenerations", "bitcoinreorganizations"},
			Verbs:     read,
		},
		{APIGroups: []string{"chaos-mesh.org"}, Resources: []string{"networkchaos"}, Verbs: read},
		{
			APIGroups: []string{"observation.stacks.org"},
			Resources: []string{"networktelemetries/finalizers"},
			Verbs:     []string{"update"},
		},
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

// leaderRules confines election state and its recorder Events to the installation namespace.
func leaderRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
		{
			APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"},
			Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"},
		},
	}
}

// installationRules supports readiness when the installation namespace is not enrolled.
func installationRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}}}
}
