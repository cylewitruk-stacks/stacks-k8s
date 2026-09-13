//go:build integration

package participantworkload

import (
	"context"
	"testing"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyCandidateNativeAPI checks Job defaulting and immutable configuration binding on the API server.
func verifyCandidateNativeAPI(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	p := stacksParticipantFixture("StacksNode")
	p.Name = "candidate-native"
	p.Spec.ParticipantName = "candidate-native"
	p.UID = ""
	admission := p.Status.Admission
	p.Status = api.ParticipantStatus{}
	if err := c.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := participantstatus.Apply(ctx, c, p, api.ParticipantStatus{Admission: admission}, participantstatus.AggregateManager); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(p), p); err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{ObjectMeta: objectMeta(p, "candidate-config", "support"), Immutable: ptr.To(true), Data: map[string][]byte{"config.toml": []byte("private rendered configuration")}}
	if err := c.Create(ctx, secret); err != nil {
		t.Fatal(err)
	}
	p.Status.Runtime = &api.ParticipantRuntimeStatus{ConfigRef: binding("Secret", secret)}
	p.Status.Runtime.ConfigRef.Fingerprint = "sha256:rendered"
	r := &Reconciler{Client: c, Reader: c}
	if ready, err := r.validateCandidateImage(ctx, p, "sha256:candidate"); ready || err != nil {
		t.Fatalf("create native validation: ready=%v err=%v", ready, err)
	}
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs, client.InNamespace(p.Namespace), client.MatchingLabels{"network.stacks.org/participant-uid": string(p.UID)}); err != nil || len(jobs.Items) != 1 {
		t.Fatalf("native Jobs: count=%d err=%v", len(jobs.Items), err)
	}
	job := &jobs.Items[0]
	// Envtest has no kubelet; this asserts API observation semantics, not native binary behavior.
	job.Status.StartTime = ptr.To(metav1.Now())
	job.Status.CompletionTime = ptr.To(metav1.Now())
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue, Reason: "ValidationObserved", LastTransitionTime: metav1.Now()}, {Type: batchv1.JobComplete, Status: corev1.ConditionTrue, Reason: "ValidationObserved", LastTransitionTime: metav1.Now()}}
	if err := c.Status().Update(ctx, job); err != nil {
		t.Fatal(err)
	}
	if ready, err := r.validateCandidateImage(ctx, p, "sha256:candidate"); !ready || err != nil {
		t.Fatalf("API-defaulted native validation: ready=%v err=%v", ready, err)
	}
	if err := c.Delete(ctx, secret); err != nil {
		t.Fatal(err)
	}
	secret.ResourceVersion = ""
	secret.UID = ""
	if err := c.Create(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if ready, err := r.validateCandidateImage(ctx, p, "sha256:candidate"); ready || err == nil {
		t.Fatal("replacement private config accepted prior completion")
	}
}
