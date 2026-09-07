// Package chaosprofile verifies the optional native-fault chart and pinned upstream schema.
package chaosprofile

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Version and SchemaSHA256 pin the unmodified upstream NetworkChaos wire contract.
const Version = "2.8.4"
const SchemaSHA256 = "d151d3d38cfb4e906df457a2927a8bad8872241655b0267e08e3315b1b615974"
const schemaURL = "https://raw.githubusercontent.com/chaos-mesh/chaos-mesh/v2.8.4/helm/chaos-mesh/crds/chaos-mesh.org_networkchaos.yaml"

// Decode reads rendered Kubernetes objects without altering their wire representation.
func Decode(reader io.Reader) ([]*unstructured.Unstructured, error) {
	decoder := yaml.NewYAMLOrJSONDecoder(reader, 4096)
	var result []*unstructured.Unstructured
	for {
		object := &unstructured.Unstructured{}
		if err := decoder.Decode(object); err != nil {
			if err == io.EOF {
				return result, nil
			}
			return nil, err
		}
		if object.GetKind() != "" {
			result = append(result, object)
		}
	}
}

// Validate requires exactly the native-delay policy, bounded quota and namespaced agent permissions.
func Validate(objects []*unstructured.Unstructured) error {
	if len(objects) != 6 {
		return fmt.Errorf("expected six profile resources")
	}
	kinds := map[string]*unstructured.Unstructured{}
	for _, o := range objects {
		if kinds[o.GetKind()] != nil {
			return fmt.Errorf("duplicate kind %s", o.GetKind())
		}
		kinds[o.GetKind()] = o
	}
	for _, kind := range []string{"ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding", "ServiceAccount", "Role", "RoleBinding", "ResourceQuota"} {
		if kinds[kind] == nil {
			return fmt.Errorf("missing %s", kind)
		}
	}
	role := &rbacv1.Role{}
	binding := &rbacv1.RoleBinding{}
	account := &corev1.ServiceAccount{}
	quota := &corev1.ResourceQuota{}
	for _, pair := range []struct {
		o  *unstructured.Unstructured
		to any
	}{{kinds["Role"], role}, {kinds["RoleBinding"], binding}, {kinds["ServiceAccount"], account}, {kinds["ResourceQuota"], quota}} {
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(pair.o.Object, pair.to); err != nil {
			return err
		}
	}
	expected := []rbacv1.PolicyRule{{APIGroups: []string{"chaos-mesh.org"}, Resources: []string{"networkchaos"}, Verbs: []string{"get", "list", "watch", "create", "delete"}}}
	if !reflect.DeepEqual(role.Rules, expected) {
		return fmt.Errorf("agent Role differs from exact native-delay permissions")
	}
	if role.Namespace == "" || binding.Namespace != role.Namespace || account.Namespace != role.Namespace || quota.Namespace != role.Namespace {
		return fmt.Errorf("namespace mismatch")
	}
	if binding.RoleRef != (rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name}) || !reflect.DeepEqual(binding.Subjects, []rbacv1.Subject{{Kind: "ServiceAccount", Name: account.Name, Namespace: role.Namespace}}) {
		return fmt.Errorf("agent binding mismatch")
	}
	if account.AutomountServiceAccountToken == nil || *account.AutomountServiceAccountToken {
		return fmt.Errorf("agent tokens must not automount")
	}
	quantity, ok := quota.Spec.Hard["count/networkchaos.chaos-mesh.org"]
	if !ok || len(quota.Spec.Hard) != 1 || quantity.Value() != 1 {
		return fmt.Errorf("expected one native fault object quota")
	}
	policy, bound := kinds["ValidatingAdmissionPolicy"], kinds["ValidatingAdmissionPolicyBinding"]
	typedPolicy := &admissionv1.ValidatingAdmissionPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(policy.Object, typedPolicy); err != nil {
		return err
	}
	scope := admissionv1.NamespacedScope
	expectedMatch := &admissionv1.MatchResources{ResourceRules: []admissionv1.NamedRuleWithOperations{{RuleWithOperations: admissionv1.RuleWithOperations{
		Operations: []admissionv1.OperationType{admissionv1.Create, admissionv1.Update},
		Rule:       admissionv1.Rule{APIGroups: []string{"chaos-mesh.org"}, APIVersions: []string{"v1alpha1"}, Resources: []string{"networkchaos"}, Scope: &scope},
	}}}}
	if !reflect.DeepEqual(typedPolicy.Spec.MatchConstraints, expectedMatch) {
		return fmt.Errorf("policy match constraints differ from exact namespaced native create/update coverage")
	}
	failure, _, _ := unstructured.NestedString(policy.Object, "spec", "failurePolicy")
	name, _, _ := unstructured.NestedString(bound.Object, "spec", "policyName")
	ns, _, _ := unstructured.NestedString(bound.Object, "spec", "matchResources", "namespaceSelector", "matchLabels", "kubernetes.io/metadata.name")
	actions, _, _ := unstructured.NestedStringSlice(bound.Object, "spec", "validationActions")
	if failure != "Fail" || name != policy.GetName() || ns != role.Namespace || !reflect.DeepEqual(actions, []string{"Deny"}) {
		return fmt.Errorf("admission must fail closed in the release namespace")
	}
	return nil
}

// Schema returns a checksum-verified upstream CRD file, cached outside the repository.
// STACKS_CHAOS_CRD_FILE optionally supplies that exact file for offline verification.
func Schema(ctx context.Context) (string, error) {
	path := os.Getenv("STACKS_CHAOS_CRD_FILE")
	explicit := path != ""
	if !explicit {
		path = filepath.Join(os.TempDir(), "stacks-chaos-networkchaos-"+Version+".yaml")
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if fmt.Sprintf("%x", sha256.Sum256(data)) != SchemaSHA256 {
			return "", fmt.Errorf("upstream schema checksum mismatch: %s; remove the cached file or supply the verified upstream CRD", path)
		}
		return path, nil
	}
	if explicit || !os.IsNotExist(err) {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, schemaURL, nil)
	if err != nil {
		return "", err
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("schema download: HTTP %d", response.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != SchemaSHA256 {
		return "", fmt.Errorf("downloaded schema checksum mismatch")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "chaos-crd-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}
