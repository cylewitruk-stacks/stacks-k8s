package foundation

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRetainedAdmissionEligibilityIsIndependentOfCandidateGeneration(t *testing.T) {
	for _, mode := range []string{
		"valid",
		"newer-invalid-source",
		"different-candidate-source",
		"missing",
		"replaced",
		"deleting",
		"API-unavailable",
		"missing-account",
	} {
		t.Run(mode, func(t *testing.T) {
			root, p := unrecordedWorker()
			root.Status.Identities = []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: p.UID}}
			source := &stacks.StacksFaucet{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "original",
					Namespace:  p.Namespace,
					UID:        "source-uid",
					Generation: 4,
				},
			}
			policy := api.Configuration{StacksFaucet: &stacks.StacksFaucetSpec{}}
			p.Spec.Source = api.Source{
				Name:       source.Name,
				UID:        source.UID,
				Generation: source.Generation,
				Digest:     "public",
			}
			p.Status.Admission = &api.Admission{
				Source:        p.Spec.Source,
				Configuration: policy,
				PolicyDigest:  Digest(policy),
			}
			p.Status.Conditions = []metav1.Condition{
				{
					Type:               "Resolved",
					Status:             metav1.ConditionFalse,
					Reason:             "RequiresReplacement",
					ObservedGeneration: p.Generation,
				},
			}
			switch mode {
			case "newer-invalid-source":
				source.Generation++
				source.Spec.TargetNodeRef = &common.NameRef{Name: "invalid-new-target"}
			case "different-candidate-source":
				p.Spec.Source = api.Source{Name: "new-candidate", UID: "candidate-uid", Generation: 1}
			case "replaced":
				source.UID = "replacement"
			case "deleting":
				now := metav1.Now()
				source.DeletionTimestamp = &now
				source.Finalizers = []string{"test"}
			case "missing-account":
				p.Status.Admission.Dependencies = []common.Binding{
					{Kind: "StacksAccount", Name: "gone", UID: "old", Fingerprint: "old"},
				}
			}
			objects := []client.Object{}
			if mode != "missing" {
				objects = append(objects, source)
			}
			scheme := runtime.NewScheme()
			_ = stacks.AddToScheme(scheme)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			var reader client.Reader = c
			if mode == "API-unavailable" {
				reader = eligibilityUnavailable{Reader: c}
			}
			r := Reconciler{Reader: reader}
			status, reason, _ := r.admissionReadiness(t.Context(), root, p, newDependencyCheck(reader, root))
			want := metav1.ConditionTrue
			if mode == "missing" || mode == "replaced" || mode == "deleting" || mode == "missing-account" {
				want = metav1.ConditionFalse
			}
			if mode == "API-unavailable" {
				want = metav1.ConditionUnknown
			}
			if status != want {
				t.Fatalf("eligibility=%s/%s, want %s", status, reason, want)
			}
			if mode == "missing-account" && reason != "IdentityUnavailable" {
				t.Fatal(reason)
			}
			if (mode == "missing" || mode == "replaced" || mode == "deleting") && reason != "DefinitionUnavailable" {
				t.Fatal(reason)
			}
			if p.Status.Admission.Source.Generation != 4 || p.Status.Admission.Source.UID != "source-uid" {
				t.Fatal("retained provenance changed")
			}
		})
	}
}

// eligibilityUnavailable simulates a failed observation rather than source deletion.
type eligibilityUnavailable struct{ client.Reader }

func (r eligibilityUnavailable) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return apierrors.NewServiceUnavailable("offline")
}

// eligibilityReads counts actual uncached public/metadata reads across participant decisions.
type eligibilityReads struct {
	client.Client
	reads map[client.ObjectKey]int
}

func (r *eligibilityReads) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	r.reads[key]++
	return r.Client.Get(ctx, key, obj, opts...)
}

func TestAdmissionPassSharesChecksAndNextPassObservesLoss(t *testing.T) {
	root, original := unrecordedWorker()
	source := &stacks.StacksFaucet{
		ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: original.Namespace, UID: "source"},
	}
	account := &stacks.StacksAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "account", Namespace: original.Namespace, UID: "account"},
		Status: common.ResolutionStatus{
			Identity:   &common.PublicIdentity{Address: "public"},
			Digest:     "fingerprint",
			Conditions: []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}},
		},
	}
	scheme := runtime.NewScheme()
	_ = stacks.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(source, account).Build()
	reader := &eligibilityReads{Client: c, reads: map[client.ObjectKey]int{}}
	r := Reconciler{Reader: reader}
	root.Spec.Participants = nil
	root.Status.Identities = nil
	participants := make([]*api.StacksNetworkParticipant, 40)
	for i := range participants {
		p := original.DeepCopy()
		p.Name = fmt.Sprintf("instance-%d", i)
		p.UID = types.UID(p.Name)
		p.Spec.ParticipantName = p.Name
		config := api.Configuration{StacksFaucet: &stacks.StacksFaucetSpec{}}
		p.Status.Admission = &api.Admission{
			Source:        api.Source{Name: source.Name, UID: source.UID},
			Configuration: config,
			PolicyDigest:  Digest(config),
			Dependencies:  []common.Binding{binding("StacksAccount", account, account.Status.Digest)},
		}
		root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: p.Name, Kind: p.Spec.Kind})
		root.Status.Identities = append(root.Status.Identities, api.InstanceIdentity{Name: p.Name, UID: p.UID})
		participants[i] = p
	}
	check := newDependencyCheck(reader, root)
	for _, p := range participants {
		// Candidate and retained-readiness validation share the same pass.
		if err := check.validate(t.Context(), p.Status.Admission.Dependencies); err != nil {
			t.Fatal(err)
		}
		state, reason, _ := r.admissionReadiness(t.Context(), root, p, check)
		if state != metav1.ConditionTrue {
			t.Fatalf("readiness %s/%s", state, reason)
		}
	}
	if reader.reads[client.ObjectKeyFromObject(account)] != 1 || reader.reads[client.ObjectKeyFromObject(source)] != 1 {
		t.Fatalf("repeated per-participant dependency reads: %+v", reader.reads)
	}
	if err := c.Delete(t.Context(), account); err != nil {
		t.Fatal(err)
	}
	state, _, _ := r.admissionReadiness(t.Context(), root, participants[0], newDependencyCheck(reader, root))
	if state != metav1.ConditionFalse || reader.reads[client.ObjectKeyFromObject(account)] != 2 {
		t.Fatal("new pass reused stale identity")
	}
}
