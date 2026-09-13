package stacksworker

import (
	"context"
	"testing"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// sourceClient isolates source availability and failures after a fresh participant read.
type sourceClient struct {
	client.Client
	sourceErr, podErr, allErr error
}

func (c *sourceClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	object client.Object,
	opts ...client.GetOption,
) error {
	if c.allErr != nil {
		return c.allErr
	}
	if _, ok := object.(*stacks.StacksTransactionProduction); ok && c.sourceErr != nil {
		return c.sourceErr
	}
	if _, ok := object.(*corev1.Pod); ok && c.podErr != nil {
		return c.podErr
	}
	return c.Client.Get(ctx, key, object, opts...)
}

// sourceBaseline supplies a real bound worker and named retained source without private reads.
func sourceBaseline(
	t *testing.T,
	activate bool,
) (*Runtime, *sourceClient, *baselineRole, *stacks.StacksTransactionProduction) {
	t.Helper()
	root, p, pod, profile := fixture(t)
	source := &stacks.StacksTransactionProduction{
		ObjectMeta: metav1.ObjectMeta{Name: "definition", Namespace: p.Namespace, UID: "source-uid", Generation: 1},
	}
	p.Spec.Source = api.Source{Name: source.Name, UID: source.UID, Generation: 1, Digest: "original"}
	p.Status.Admission.Source = p.Spec.Source
	ProjectSession(root, p, pod, nil, time.Now())
	c := &sourceClient{Client: &statusClient{Client: fakeClient(t, root, p, pod, source)}}
	role := &baselineRole{}
	r := &Runtime{
		Client:          c,
		Namespace:       p.Namespace,
		ParticipantName: p.Name,
		NetworkUID:      root.UID,
		ParticipantUID:  p.UID,
		PodName:         pod.Name,
		PodUID:          pod.UID,
		Profile:         profile,
		Role:            role,
		Prerequisites:   func(context.Context, Snapshot) error { return nil },
	}
	if activate {
		if _, err := r.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
		if role.sends != 1 || r.appliedSnapshot == nil {
			t.Fatal("baseline was not activated")
		}
	}
	return r, c, role, source
}

func TestSourceLossBlocksSendsAndLaterOutageFallback(t *testing.T) {
	for _, mode := range []string{
		"missing",
		"replaced",
		"deleting",
		"ineligible-condition",
		"ineligible-with-source-outage",
		"source-loss-before-pod-error",
	} {
		t.Run(mode, func(t *testing.T) {
			r, c, role, source := sourceBaseline(t, true)
			if mode == "ineligible-condition" || mode == "ineligible-with-source-outage" {
				p := &api.StacksNetworkParticipant{}
				if err := c.Client.Get(
					t.Context(),
					client.ObjectKey{Namespace: r.Namespace, Name: r.ParticipantName},
					p,
				); err != nil {
					t.Fatal(err)
				}
				for i := range p.Status.Conditions {
					if p.Status.Conditions[i].Type == "AdmissionReady" {
						p.Status.Conditions[i].Status = metav1.ConditionFalse
						p.Status.Conditions[i].Reason = "DefinitionUnavailable"
					}
				}
				if err := c.Client.Status().Update(t.Context(), p); err != nil {
					t.Fatal(err)
				}
				c.podErr = apierrors.NewServiceUnavailable("later Pod read failed")
				if mode == "ineligible-with-source-outage" {
					c.sourceErr = apierrors.NewServiceUnavailable(
						"source read failed after observed admission withdrawal",
					)
				}
			} else {
				if mode == "deleting" {
					source.Finalizers = []string{"test"}
					if err := c.Update(t.Context(), source); err != nil {
						t.Fatal(err)
					}
				}
				if err := c.Delete(t.Context(), source); err != nil {
					t.Fatal(err)
				}
				if mode == "replaced" {
					replacement := source.DeepCopy()
					replacement.UID = "new-source"
					replacement.ResourceVersion = ""
					if err := c.Create(t.Context(), replacement); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "source-loss-before-pod-error" {
					c.podErr = apierrors.NewServiceUnavailable("later Pod read failed")
				}
			}
			_, _ = r.Reconcile(t.Context())
			if role.sends != 1 || !r.cacheHeld {
				t.Fatalf("source loss authorized sends=%d held=%v", role.sends, r.cacheHeld)
			}
			c.allErr = apierrors.NewServiceUnavailable("whole API outage after observed loss")
			_, _ = r.Reconcile(t.Context())
			if role.sends != 1 {
				t.Fatal("API outage revived withdrawn source authority")
			}
		})
	}
}

func TestRetainedSourceAllowsNewRejectedGenerationAndKnownBaselineOutage(t *testing.T) {
	r, c, role, source := sourceBaseline(t, true)
	source.Generation++
	if err := c.Update(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	p := &api.StacksNetworkParticipant{}
	if err := c.Client.Get(
		t.Context(),
		client.ObjectKey{Namespace: r.Namespace, Name: r.ParticipantName},
		p,
	); err != nil {
		t.Fatal(err)
	}
	p.Spec.Source = api.Source{Name: "different-candidate", UID: "candidate", Generation: 9}
	if err := c.Update(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if role.sends != 2 || r.appliedSnapshot.Participant.Status.Admission.Source.Generation != 1 {
		t.Fatal("rejected candidate revoked or replaced retained source")
	}
	c.sourceErr = apierrors.NewServiceUnavailable("source API temporarily offline")
	if _, err := r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if role.sends != 3 {
		t.Fatal("previously validated baseline did not survive source API outage")
	}
}

func TestFirstSourceObservationCannotCreateCachedSendAuthority(t *testing.T) {
	r, c, role, _ := sourceBaseline(t, false)
	c.sourceErr = apierrors.NewServiceUnavailable("source has never been observed")
	_, _ = r.Reconcile(t.Context())
	if role.sends != 0 || r.appliedSnapshot != nil {
		t.Fatal("unverified first source became cached authority")
	}
}

func TestWorkerReadsGrantOnlyNamedRetainedSource(t *testing.T) {
	for kind, resource := range map[api.ParticipantKind]string{
		"StacksStacker":               "stacksstackers",
		"StacksFaucet":                "stacksfaucets",
		"StacksContractSet":           "stackscontractsets",
		"StacksTransactionProduction": "stackstransactionproductions",
	} {
		t.Run(string(kind), func(t *testing.T) {
			root, p, _, _ := fixture(t)
			p.Spec.Kind = kind
			p.Status.Admission.Configuration = api.Configuration{}
			p.Status.Admission.Source = api.Source{Name: "retained", UID: "source", Generation: 1}
			p.Spec.Source = api.Source{Name: "candidate", UID: "other", Generation: 2}
			reads, err := (Profiles{}).Reads(t.Context(), root, p)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, ref := range reads {
				if ref.Resource == resource {
					found = ref.Name == "retained" && ref.APIVersion == stacks.GroupVersion.String() &&
						publicResource(stacks.GroupVersion.Group, stacks.GroupVersion.Version, ref.Resource)
				}
				if ref.Name == "candidate" || ref.Resource == "secrets" {
					t.Fatalf("unexpected public grant: %+v", ref)
				}
			}
			if !found {
				t.Fatal("retained source read missing")
			}
		})
	}
}
