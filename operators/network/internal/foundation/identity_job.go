package foundation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// KeyJobInput limits an identity resolver to one key and one public report.
type KeyJobInput struct {
	// Namespace contains the source and output objects.
	Namespace string `json:"namespace"`
	// SourceUID pins the account/wallet owning the report.
	SourceUID types.UID `json:"sourceUID"`
	// InputDigest identifies the source specification.
	InputDigest string `json:"inputDigest"`
	// CredentialsRef identifies exact input material.
	CredentialsRef common.SecretKeyRef `json:"credentialsRef"`
	// CredentialsUID rejects a replacement Secret.
	CredentialsUID types.UID `json:"credentialsUID"`
	// Generate permits filling a controller-created empty Secret once.
	Generate bool `json:"generate"`
	// Descriptor selects public wallet descriptor parsing.
	Descriptor bool `json:"descriptor"`
	// ReportName identifies the sole public output ConfigMap.
	ReportName string `json:"reportName"`
	// ReportUID rejects output replacement.
	ReportUID types.UID `json:"reportUID"`
}

// KeyReport contains only public encodings and exact input/output provenance.
type KeyReport struct {
	// SourceUID binds the reusable identity object.
	SourceUID types.UID `json:"sourceUID"`
	// InputDigest identifies evaluated inputs.
	InputDigest string `json:"inputDigest"`
	// CredentialsUID binds the private or descriptor input.
	CredentialsUID types.UID `json:"credentialsUID"`
	// Public contains independently validated public encodings.
	Public identity.Public `json:"public"`
	// Descriptor carries the public wallet descriptor.
	Descriptor string `json:"descriptor,omitempty"`
	// BitcoinAddress is the descriptor's actual address encoding.
	BitcoinAddress string `json:"bitcoinAddress,omitempty"`
}

// RunKeyJob executes inside the scoped resolver process, never the controller.
func RunKeyJob(ctx context.Context, c client.Client, in KeyJobInput) error {
	if in.Namespace == "" || in.SourceUID == "" || in.CredentialsUID == "" || in.ReportUID == "" {
		return fmt.Errorf("incomplete identity job binding")
	}
	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: in.Namespace, Name: in.CredentialsRef.Name}, &secret); err != nil {
		return err
	}
	if secret.UID != in.CredentialsUID {
		return fmt.Errorf("credential identity changed")
	}
	if in.Generate && len(secret.Data) == 0 && !ptr.Deref(secret.Immutable, false) {
		if !ownedUID(&secret, in.SourceUID) {
			return fmt.Errorf("generated credential has foreign ownership")
		}
		scalar, err := identity.Generate()
		if err != nil {
			return err
		}
		base := secret.DeepCopy()
		secret.Data = map[string][]byte{in.CredentialsRef.Key: []byte(scalar)}
		secret.Immutable = ptr.To(true)
		if err := c.Patch(ctx, &secret, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	if !ptr.Deref(secret.Immutable, false) {
		return fmt.Errorf("identity input must be immutable")
	}
	material := strings.TrimSpace(string(secret.Data[in.CredentialsRef.Key]))
	report := KeyReport{SourceUID: in.SourceUID, InputDigest: in.InputDigest, CredentialsUID: secret.UID}
	var err error
	if in.Descriptor {
		report.Public, report.BitcoinAddress, err = identity.FromDescriptor(material)
		report.Descriptor = material
	} else {
		report.Public, err = identity.FromPrivate(material)
		report.Descriptor = "pkh(" + report.Public.PublicKey + ")"
		report.BitcoinAddress = report.Public.BitcoinAddress
	}
	if err != nil {
		return fmt.Errorf("identity material is invalid for the selected format")
	}
	var output corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Namespace: in.Namespace, Name: in.ReportName}, &output); err != nil {
		return err
	}
	if output.UID != in.ReportUID || !ownedUID(&output, in.SourceUID) {
		return fmt.Errorf("identity report binding changed")
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if existing := output.Data["report.json"]; existing != "" {
		if existing != string(data) {
			return fmt.Errorf("identity report cannot be overwritten with different values")
		}
		return nil
	}
	base := output.DeepCopy()
	output.Data = map[string]string{"report.json": string(data)}
	return c.Patch(ctx, &output, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

func ownedUID(obj metav1.Object, uid types.UID) bool {
	ref := metav1.GetControllerOf(obj)
	return ref != nil && ref.UID == uid
}

func createOwned(ctx context.Context, c client.Client, s *runtime.Scheme, owner client.Object, obj client.Object) error {
	if err := controllerutil.SetControllerReference(owner, obj, s, controllerutil.WithBlockOwnerDeletion(false)); err != nil {
		return err
	}
	err := c.Create(ctx, obj)
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	current := obj.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
		return err
	}
	if !ownedUID(current, owner.GetUID()) || current.GetDeletionTimestamp() != nil {
		return fmt.Errorf("owned output unavailable or foreign: %s", obj.GetName())
	}
	return nil
}

// KeyJobRules grants the resolver only its exact input and report names.
func KeyJobRules(in KeyJobInput) []rbacv1.PolicyRule {
	verbs := []string{"get"}
	if in.Generate {
		verbs = append(verbs, "patch")
	}
	return []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{in.CredentialsRef.Name}, Verbs: verbs}, {APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{in.ReportName}, Verbs: []string{"get", "patch"}}}
}

var errResolverFailed = errors.New("scoped identity resolver Job failed; inspect its status and replace the identity declaration to retry")

func provisionKeyJob(ctx context.Context, c client.Client, reader client.Reader, s *runtime.Scheme, owner client.Object, image string, in KeyJobInput) error {
	if image == "" {
		return fmt.Errorf("resolver image is required")
	}
	gvk, err := apiutil.GVKForObject(owner, s)
	if err != nil {
		return err
	}
	name := RuntimeName("", string(owner.GetUID()), gvk.Kind, owner.GetName(), "resolve-"+strings.TrimPrefix(Digest(in), "sha256:"))
	account := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: owner.GetNamespace()}}
	if err := createOwned(ctx, c, s, owner, account); err != nil {
		return err
	}
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: owner.GetNamespace()}, Rules: KeyJobRules(in)}
	if err := createOwned(ctx, c, s, owner, role); err != nil {
		return err
	}
	binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: owner.GetNamespace()}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: common.KindRole, Name: name}, Subjects: []rbacv1.Subject{{Kind: common.KindServiceAccount, Name: name, Namespace: owner.GetNamespace()}}}
	if err := createOwned(ctx, c, s, owner, binding); err != nil {
		return err
	}
	data, _ := json.Marshal(in)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: owner.GetNamespace(), Labels: map[string]string{managedByLabel: foundationManager, api.LabelRole: api.RoleSupport, api.LabelSourceUID: string(owner.GetUID()), api.LabelSourceKind: gvk.Kind}, Annotations: map[string]string{api.AnnotationSourceName: owner.GetName()}},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To[int32](3), ActiveDeadlineSeconds: ptr.To[int64](120),
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{managedByLabel: foundationManager, api.LabelRole: api.RoleSupport, api.LabelSourceUID: string(owner.GetUID()), api.LabelSourceKind: gvk.Kind}}, Spec: corev1.PodSpec{
				ServiceAccountName: name, RestartPolicy: corev1.RestartPolicyNever,
				SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](65532), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
				Containers: []corev1.Container{{
					Name: "resolver", Image: image, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")}}, Command: []string{"/foundation"}, Args: []string{"--mode=" + ModeResolveKey, "--input=" + string(data)},
					SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
				}},
			}},
		},
	}
	if err := createOwned(ctx, c, s, owner, job); err != nil {
		return err
	}
	if err := reader.Get(ctx, client.ObjectKeyFromObject(job), job); err != nil {
		return err
	}
	if !ownedUID(job, owner.GetUID()) || job.DeletionTimestamp != nil {
		return fmt.Errorf("identity resolver Job unavailable or foreign")
	}
	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 1 || len(containers[0].Args) != 2 || containers[0].Args[1] != "--input="+string(data) {
		return fmt.Errorf("identity resolver Job input binding changed")
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return errResolverFailed
		}
	}
	return nil
}
