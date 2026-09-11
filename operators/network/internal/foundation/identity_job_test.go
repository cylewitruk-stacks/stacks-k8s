package foundation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKeyJobRetriesPreserveKeyAndPublicReport(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			scheme := runtime.NewScheme()
			corev1.AddToScheme(scheme)
			owner := []metav1.OwnerReference{{APIVersion: "stacks.stacks.org/v1alpha2", Kind: "StacksAccount", Name: "account", UID: "account-uid", Controller: ptr.To(true)}}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "key", Namespace: "test", UID: "key-uid", OwnerReferences: owner}}
			report := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "report", Namespace: "test", UID: "report-uid", OwnerReferences: owner}}
			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, report).Build()
			c := &lostSecretPatch{Client: base, commit: committed}
			input := KeyJobInput{Namespace: "test", SourceUID: "account-uid", InputDigest: "input", CredentialsRef: common.SecretKeyRef{Name: "key", Key: "privateKey"}, CredentialsUID: "key-uid", Generate: true, ReportName: "report", ReportUID: "report-uid"}
			ctx := context.Background()
			if err := RunKeyJob(ctx, c, input); err == nil {
				t.Fatal("expected lost write acknowledgement")
			}
			if err := base.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
				t.Fatal(err)
			}
			key := string(secret.Data["privateKey"])
			if err := RunKeyJob(ctx, c, input); err != nil {
				t.Fatal(err)
			}
			base.Get(ctx, client.ObjectKeyFromObject(secret), secret)
			if committed && string(secret.Data["privateKey"]) != key {
				t.Fatal("regenerated committed key")
			}
			if !ptr.Deref(secret.Immutable, false) {
				t.Fatal("generated key not immutable")
			}
			base.Get(ctx, client.ObjectKeyFromObject(report), report)
			before := report.Data["report.json"]
			if strings.Contains(before, string(secret.Data["privateKey"])) {
				t.Fatal("private key escaped in report")
			}
			var public KeyReport
			if err := json.Unmarshal([]byte(before), &public); err != nil || public.SourceUID != input.SourceUID || public.Public.Address == "" {
				t.Fatalf("invalid public report: %v", err)
			}
			if err := RunKeyJob(ctx, c, input); err != nil {
				t.Fatal(err)
			}
			base.Get(ctx, client.ObjectKeyFromObject(report), report)
			if report.Data["report.json"] != before {
				t.Fatal("idempotent retry changed report")
			}
			input.CredentialsUID = types.UID("replacement")
			if err := RunKeyJob(ctx, c, input); err == nil {
				t.Fatal("accepted replacement credentials")
			}
		})
	}
}

type lostSecretPatch struct {
	client.Client
	commit, failed bool
}

func (c *lostSecretPatch) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if _, ok := obj.(*corev1.Secret); ok && !c.failed {
		c.failed = true
		if c.commit {
			if err := c.Client.Patch(ctx, obj, patch, opts...); err != nil {
				return err
			}
		}
		return fmt.Errorf("acknowledgement lost")
	}
	return c.Client.Patch(ctx, obj, patch, opts...)
}
func TestPublicIdentityResolverDoesNotReadSecrets(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	stacks.AddToScheme(scheme)
	// A known compressed generator public key; no private key enters the controller call.
	a := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "public", Namespace: "test", UID: "public-uid"}, Spec: stacks.StacksAccountSpec{Key: &common.KeySource{PublicIdentity: &common.PublicIdentity{PublicKey: "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798", Address: "ST1THWXQ8368SDN2MJGE4BMDKMCHZ2GSVTSQDA7QF"}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a).Build()
	guard := &noSecretReads{Client: c}
	r := IdentityReconciler{Client: guard, Reader: guard, Scheme: scheme}
	if _, err := r.resolve(context.Background(), a, Digest(a.Spec)); err != nil {
		t.Fatal(err)
	}
}

type noSecretReads struct{ client.Client }

func (c *noSecretReads) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		return fmt.Errorf("controller attempted Secret data read")
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

// TestReportReplacementReinspectsExistingKey models Job execution without a kubelet.
func TestReportReplacementReinspectsExistingKey(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	batchv1.AddToScheme(scheme)
	rbacv1.AddToScheme(scheme)
	stacks.AddToScheme(scheme)
	owner := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "account", Namespace: "test", UID: "account-uid"}}
	refs := []metav1.OwnerReference{{APIVersion: "stacks.stacks.org/v1alpha2", Kind: "StacksAccount", Name: owner.Name, UID: owner.UID, Controller: ptr.To(true)}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "key", Namespace: "test", UID: "key-uid", OwnerReferences: refs}}
	report := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "report", Namespace: "test", UID: "report-uid", OwnerReferences: refs}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, secret, report).WithStatusSubresource(&batchv1.Job{}).Build()
	in := KeyJobInput{Namespace: "test", SourceUID: owner.UID, InputDigest: "input", CredentialsRef: common.SecretKeyRef{Name: "key", Key: "privateKey"}, CredentialsUID: secret.UID, Generate: true, ReportName: report.Name, ReportUID: report.UID}
	if err := provisionKeyJob(ctx, c, c, scheme, owner, "resolver:test", in); err != nil {
		t.Fatal(err)
	}
	if err := RunKeyJob(ctx, c, in); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		t.Fatal(err)
	}
	key := string(secret.Data["privateKey"])
	if err := c.Get(ctx, client.ObjectKeyFromObject(report), report); err != nil {
		t.Fatal(err)
	}
	public := report.Data["report.json"]
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs); err != nil || len(jobs.Items) != 1 {
		t.Fatalf("jobs: %v %+v", err, jobs.Items)
	}
	old := jobs.Items[0].DeepCopy()
	old.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if err := c.Status().Update(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, report); err != nil {
		t.Fatal(err)
	}
	report = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: in.ReportName, Namespace: "test", UID: "new-report-uid", OwnerReferences: refs}}
	if err := c.Create(ctx, report); err != nil {
		t.Fatal(err)
	}
	if err := RunKeyJob(ctx, c, in); err == nil {
		t.Fatal("old job accepted replacement report")
	}
	in.ReportUID = report.UID
	for i := 0; i < 2; i++ {
		if err := provisionKeyJob(ctx, c, c, scheme, owner, "resolver:test", in); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.List(ctx, &jobs); err != nil || len(jobs.Items) != 2 {
		t.Fatalf("replacement job not created once: %v %d", err, len(jobs.Items))
	}
	var current batchv1.Job
	for _, job := range jobs.Items {
		if job.Name != old.Name {
			current = job
		}
	}
	var actual KeyJobInput
	if err := json.Unmarshal([]byte(strings.TrimPrefix(current.Spec.Template.Spec.Containers[0].Args[1], "--input=")), &actual); err != nil {
		t.Fatal(err)
	}
	if actual.ReportUID != in.ReportUID || actual.CredentialsUID != secret.UID {
		t.Fatal("replacement lost input/output binding")
	}
	if err := RunKeyJob(ctx, c, actual); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["privateKey"]) != key || !ptr.Deref(secret.Immutable, false) {
		t.Fatal("reinspection changed immutable key")
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(report), report); err != nil {
		t.Fatal(err)
	}
	if report.Data["report.json"] != public {
		t.Fatal("reinspection changed public identity")
	}
	// Owned but altered inputs must not be accepted under the deterministic name.
	current.Spec.Template.Spec.Containers[0].Args[1] = "--input={}"
	if err := c.Update(ctx, &current); err != nil {
		t.Fatal(err)
	}
	if err := provisionKeyJob(ctx, c, c, scheme, owner, "resolver:test", in); err == nil {
		t.Fatal("altered resolver binding accepted")
	}
}
