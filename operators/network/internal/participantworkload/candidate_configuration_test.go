package participantworkload

import (
	"context"
	"encoding/json"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCandidateResolverCannotBeUsedAsLiveGenesisAuthority(t *testing.T) {
	ctx := context.Background()
	c, in := stacksResolverFixture(t)
	if err := RunStacksCandidateConfigResolver(ctx, c, in); err == nil {
		t.Fatal("candidate mode accepted published genesis authority")
	}
	in.CandidateDigest = "sha256:candidate"
	in.Genesis = common.Binding{Fingerprint: in.Genesis.Fingerprint}
	if err := RunStacksConfigResolver(ctx, c, in); err == nil {
		t.Fatal("live mode accepted candidate genesis")
	}
	if err := RunStacksCandidateConfigResolver(ctx, c, in); err != nil {
		t.Fatal(err)
	}
	var report corev1.ConfigMap
	if err := c.Get(ctx, client.ObjectKey{Namespace: in.Namespace, Name: in.Report.Name}, &report); err != nil {
		t.Fatal(err)
	}
	var result StacksConfigReport
	if err := json.Unmarshal([]byte(report.Data["report.json"]), &result); err != nil {
		t.Fatal(err)
	}
	if result.InputDigest != digest(in) || result.GenesisDigest != in.Genesis.Fingerprint || !result.Verified {
		t.Fatal("candidate report does not bind complete public rendering inputs")
	}
	in.CandidateDigest = "sha256:changed"
	if err := RunStacksCandidateConfigResolver(ctx, c, in); err == nil {
		t.Fatal("same report accepted a changed candidate")
	}
}

func TestCandidatePreparationPreservesActiveRuntimeConfiguration(t *testing.T) {
	p := stacksParticipantFixture("StacksNode")
	p.Status.Runtime = nativeRuntimeFixture()
	p.Status.Runtime.PolicyDigest = "sha256:active-policy"
	c := testClient(t, p)
	prepared := p.Status.Runtime.DeepCopy()
	prepared.ConfigRef = &common.Binding{Kind: "Secret", Name: "candidate", UID: "candidate", Fingerprint: "sha256:candidate"}
	prepared.ConfigurationDigest = "sha256:candidate"
	prepared.PolicyDigest = "sha256:candidate-policy"
	r := &Reconciler{Client: c, Reader: c}
	if err := r.publishCandidateBindings(context.Background(), p, prepared); err != nil {
		t.Fatal(err)
	}
	var actual api.StacksNetworkParticipant
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(p), &actual); err != nil {
		t.Fatal(err)
	}
	if actual.Status.Runtime.ConfigRef.Name != "config" || actual.Status.Runtime.PolicyDigest != "sha256:active-policy" {
		t.Fatal("candidate support replaced admitted runtime configuration")
	}
}

func TestCandidateNativeValidationIsScopedAndRequiresExactInputs(t *testing.T) {
	for _, kind := range []api.ParticipantKind{"StacksNode", "StacksSigner"} {
		t.Run(string(kind), func(t *testing.T) {
			ctx := context.Background()
			p := stacksParticipantFixture(kind)
			p.Status.Runtime = nativeRuntimeFixture()
			p.Status.Runtime.ConfigRef.Fingerprint = "sha256:rendered"
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: p.Namespace, UID: "config-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID, Controller: ptr.To(true)}}}, Immutable: ptr.To(true)}
			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, batchv1.AddToScheme, api.AddToScheme} {
				if err := add(scheme); err != nil {
					t.Fatal(err)
				}
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&batchv1.Job{}).WithObjects(p, secret).Build()
			r := &Reconciler{Client: c, Reader: c, candidateConfiguration: &foundation.CandidateConfiguration{Root: &api.StacksNetwork{Spec: api.StacksNetworkSpec{Defaults: &api.Defaults{WorkerPlacement: &common.Placement{NodeSelector: map[string]string{"pool": "workers"}}}}}}}
			if ready, err := r.validateCandidateImage(ctx, p, "sha256:candidate"); err != nil || ready {
				t.Fatalf("pending validation: ready=%v err=%v", ready, err)
			}
			var jobs batchv1.JobList
			if err := c.List(ctx, &jobs); err != nil || len(jobs.Items) != 1 {
				t.Fatalf("Jobs: %v %+v", err, jobs.Items)
			}
			job := &jobs.Items[0]
			pod := job.Spec.Template.Spec
			if ptr.Deref(pod.AutomountServiceAccountToken, true) || len(pod.Volumes) != 1 || pod.Volumes[0].Secret.SecretName != "config" || len(pod.Containers) != 1 || len(pod.Containers[0].Env) != 0 || pod.NodeSelector["pool"] != "workers" {
				t.Fatal("native validation received excess authority or incorrect placement")
			}
			if pod.Containers[0].Command[2] != `exec "$1" check-config --config /config/config.toml >/dev/null 2>&1` {
				t.Fatal("private native validation output was not suppressed")
			}
			var workloads appsv1.StatefulSetList
			if err := c.List(ctx, &workloads); err != nil || len(workloads.Items) != 0 {
				t.Fatal("candidate native validation activated an actor")
			}
			job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
			if err := c.Status().Update(ctx, job); err != nil {
				t.Fatal(err)
			}
			if ready, err := r.validateCandidateImage(ctx, p, "sha256:candidate"); err != nil || !ready {
				t.Fatalf("completed validation: ready=%v err=%v", ready, err)
			}
			if ready, err := r.validateCandidateImage(ctx, p, "sha256:new-candidate"); err != nil || ready {
				t.Fatalf("stale candidate completion reused: ready=%v err=%v", ready, err)
			}
			if err := c.Delete(ctx, secret); err != nil {
				t.Fatal(err)
			}
			secret.ResourceVersion = ""
			secret.UID = "replacement"
			if err := c.Create(ctx, secret); err != nil {
				t.Fatal(err)
			}
			if ready, err := r.validateCandidateImage(ctx, p, "sha256:candidate"); err == nil || ready {
				t.Fatal("same-name config replacement reused prior native completion")
			}
		})
	}
}
