package rbac

import (
	"bytes"
	"encoding/json"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateAcceptsExactContractAndRejectsAddedPrivilege(t *testing.T) {
	role := rbacv1.Role{TypeMeta: metav1.TypeMeta{APIVersion: rbacv1.SchemeGroupVersion.String(), Kind: "Role"}, ObjectMeta: metav1.ObjectMeta{Name: "operator", Namespace: "test"}, Rules: expectedRules()}
	binding := rbacv1.RoleBinding{TypeMeta: metav1.TypeMeta{APIVersion: rbacv1.SchemeGroupVersion.String(), Kind: "RoleBinding"}, ObjectMeta: metav1.ObjectMeta{Name: "operator", Namespace: "test"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "operator", Namespace: "test"}}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "operator"}}
	encode := func() *bytes.Reader {
		left, _ := json.Marshal(role)
		right, _ := json.Marshal(binding)
		return bytes.NewReader(append(append(left, []byte("\n---\n")...), right...))
	}
	if err := Validate(encode()); err != nil {
		t.Fatal(err)
	}
	role.Rules[0].Resources = append(role.Rules[0].Resources, "secrets")
	if err := Validate(encode()); err == nil {
		t.Fatal("added secret privilege was accepted")
	}
}
