package rbac

import (
	"bytes"
	"encoding/json"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestValidateInstallationScopesAndRejectsAddedPrivilege(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(map[bool]string{false: "cluster", true: "namespace"}[scoped], func(t *testing.T) {
			kind, ns := "ClusterRole", ""
			bindingKind := "ClusterRoleBinding"
			if scoped {
				kind, ns, bindingKind = "Role", "experiments", "RoleBinding"
			}
			role := rbac.Role{TypeMeta: metav1.TypeMeta{APIVersion: rbac.SchemeGroupVersion.String(), Kind: kind}, ObjectMeta: metav1.ObjectMeta{Name: "operator", Namespace: ns, Labels: map[string]string{"app.kubernetes.io/component": "operator"}}, Rules: expectedRules()}
			leader := rbac.Role{TypeMeta: metav1.TypeMeta{APIVersion: rbac.SchemeGroupVersion.String(), Kind: "Role"}, ObjectMeta: metav1.ObjectMeta{Name: "leader", Namespace: "system", Labels: map[string]string{"app.kubernetes.io/component": "leader"}}, Rules: leaderRules()}
			binding := rbac.RoleBinding{TypeMeta: metav1.TypeMeta{APIVersion: rbac.SchemeGroupVersion.String(), Kind: bindingKind}, ObjectMeta: metav1.ObjectMeta{Name: "operator", Namespace: ns}, Subjects: []rbac.Subject{{Kind: "ServiceAccount", Name: "operator", Namespace: "system"}}, RoleRef: rbac.RoleRef{APIGroup: rbac.GroupName, Kind: kind, Name: "operator"}}
			election := *binding.DeepCopy()
			election.Kind = "RoleBinding"
			election.Name = "leader"
			election.Namespace = "system"
			election.RoleRef.Kind = "Role"
			election.RoleRef.Name = "leader"
			encode := func() *bytes.Reader {
				var b bytes.Buffer
				for _, v := range []any{role, leader, binding, election} {
					data, _ := json.Marshal(v)
					b.Write(data)
					b.WriteString("\n---\n")
				}
				return bytes.NewReader(b.Bytes())
			}
			if err := Validate(encode()); err != nil {
				t.Fatal(err)
			}
			for _, resource := range []string{"secrets", "pods/exec", "*"} {
				role.Rules[0].Resources = append(role.Rules[0].Resources, resource)
				if Validate(encode()) == nil {
					t.Fatalf("extra privilege %s accepted", resource)
				}
				role.Rules = expectedRules()
			}
			binding.Subjects[0].Namespace = "foreign"
			if Validate(encode()) == nil {
				t.Fatal("different installation identity accepted")
			}
			binding.Subjects[0].Namespace = "system"
			leader.Rules = expectedRules()
			if Validate(encode()) == nil {
				t.Fatal("leader scope expansion accepted")
			}
		})
	}
}
