package foundation

import (
	"context"
	"fmt"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDependencyCheckRejectsStaleCredentialStatus(t *testing.T) {
	for _, derived := range []bool{false, true} {
		for _, mode := range []string{
			"missing",
			"replaced",
			"deleting",
			"current",
			"account-replaced",
			"fingerprint-changed",
		} {
			t.Run(fmt.Sprintf("derived=%t/%s", derived, mode), func(t *testing.T) {
				scheme := runtime.NewScheme()
				if err := corev1.AddToScheme(scheme); err != nil {
					t.Fatal(err)
				}
				if err := stacks.AddToScheme(scheme); err != nil {
					t.Fatal(err)
				}
				if err := bitcoin.AddToScheme(scheme); err != nil {
					t.Fatal(err)
				}
				account := &stacks.StacksAccount{
					ObjectMeta: metav1.ObjectMeta{Name: "account", Namespace: "test", UID: "account-uid"},
					Status: common.ResolutionStatus{
						Identity:       &common.PublicIdentity{Address: "public"},
						Digest:         "fingerprint",
						CredentialsRef: &common.SecretKeyRef{Name: "key", Key: "privateKey"},
						CredentialsUID: "key-uid",
						Conditions:     []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}},
					},
				}
				pinned := binding("StacksAccount", account, account.Status.Digest)
				wallet := &bitcoin.BitcoinWallet{
					ObjectMeta: metav1.ObjectMeta{Name: "wallet", Namespace: "test", UID: "wallet-uid"},
					Spec: bitcoin.BitcoinWalletSpec{
						KeySource: &bitcoin.WalletKeySource{StacksMinerAccountRef: &common.NameRef{Name: "account"}},
					},
					Status: common.ResolutionStatus{
						Identity:     &common.PublicIdentity{Address: "public"},
						Digest:       "wallet-fingerprint",
						Dependencies: []common.Binding{pinned},
						Conditions:   []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}},
					},
				}
				if derived {
					pinned = binding("BitcoinWallet", wallet, wallet.Status.Digest)
				}
				if mode == "account-replaced" {
					account.UID = "other-account"
				}
				if mode == "fingerprint-changed" {
					account.Status.Digest = "other-fingerprint"
				}
				objects := []client.Object{account, wallet}
				if mode != "missing" {
					secret := &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{Name: "key", Namespace: "test", UID: "key-uid"},
						Immutable:  ptr.To(true),
					}
					if mode == "replaced" {
						secret.UID = "replacement"
					}
					if mode == "deleting" {
						secret.DeletionTimestamp = ptr.To(metav1.Now())
						secret.Finalizers = []string{"hold"}
					}
					objects = append(objects, secret)
				}
				base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
				reader := &metadataCounter{Client: base}
				check := newDependencyCheck(
					reader,
					&api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Namespace: "test"}},
				)
				err := check.validate(context.Background(), []common.Binding{pinned, pinned})
				if (err == nil) != (mode == "current") {
					t.Fatalf("mode %s: %v", mode, err)
				}
				if reader.secretReads > 1 {
					t.Fatalf("duplicate metadata reads: %d", reader.secretReads)
				}
				if !resolved(account) {
					t.Fatal("test must retain stale Resolved status")
				}
			})
		}
	}
}

// metadataCounter forbids Secret data reads and counts metadata checks.
type metadataCounter struct {
	client.Client
	secretReads int
}

func (r *metadataCounter) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if _, ok := obj.(*corev1.Secret); ok {
		return fmt.Errorf("private Secret read forbidden")
	}
	if m, ok := obj.(*metav1.PartialObjectMetadata); ok && m.Kind == "Secret" {
		r.secretReads++
	}
	return r.Client.Get(ctx, key, obj, opts...)
}

func TestDependencyCheckRetainedParticipantMustRemainSelected(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	p := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "instance",
			Namespace:       "test",
			UID:             "instance-uid",
			OwnerReferences: []metav1.OwnerReference{{UID: "root-uid", Controller: ptr.To(true)}},
		},
		Spec: api.StacksNetworkParticipantSpec{
			NetworkUID:      "root-uid",
			ParticipantName: "node",
			Kind:            "StacksNode",
		},
		Status: api.ParticipantStatus{Admission: &api.Admission{}},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(p).Build()
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", UID: "root-uid"},
		Spec:       api.StacksNetworkSpec{Participants: []api.Participant{{Name: "node", Kind: "StacksNode"}}},
		Status: api.StacksNetworkStatus{
			Identities: []api.InstanceIdentity{{Name: "node", UID: types.UID("instance-uid")}},
		},
	}
	for _, selected := range []bool{true, false} {
		if !selected {
			root.Spec.Participants = nil
		}
		err := newDependencyCheck(
			base,
			root,
		).validate(context.Background(), []common.Binding{binding("StacksNetworkParticipant", p, "")})
		if (err == nil) != selected {
			t.Fatalf("selected=%t: %v", selected, err)
		}
	}
}

func TestRetainedParticipantChecksSourceAndTransitiveDependencies(t *testing.T) {
	for _, mode := range []string{"valid", "source-missing", "source-replaced", "child-missing", "cycle"} {
		t.Run(mode, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := api.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := bitcoin.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := stacks.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			p := &api.StacksNetworkParticipant{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "instance",
					Namespace:       "test",
					UID:             "instance-uid",
					OwnerReferences: []metav1.OwnerReference{{UID: "root-uid", Controller: ptr.To(true)}},
				},
				Spec: api.StacksNetworkParticipantSpec{
					NetworkUID:      "root-uid",
					ParticipantName: "node",
					Kind:            "BitcoinNode",
					Source:          api.Source{Name: "definition", UID: "definition-uid"},
				},
			}
			root := &api.StacksNetwork{
				ObjectMeta: metav1.ObjectMeta{Namespace: "test", UID: "root-uid"},
				Spec:       api.StacksNetworkSpec{Participants: []api.Participant{{Name: "node", Kind: "BitcoinNode"}}},
				Status:     api.StacksNetworkStatus{Identities: []api.InstanceIdentity{{Name: "node", UID: p.UID}}},
			}
			account := &stacks.StacksAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "account", Namespace: "test", UID: "account-uid"},
				Status: common.ResolutionStatus{
					Identity:   &common.PublicIdentity{Address: "public"},
					Digest:     "public-fingerprint",
					Conditions: []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}},
				},
			}
			p.Status.Admission = &api.Admission{
				Source:       p.Spec.Source,
				Dependencies: []common.Binding{binding("StacksAccount", account, account.Status.Digest)},
			}
			// Candidate-policy failure is not itself a reason to discard a still-valid admission.
			p.Status.Conditions = []metav1.Condition{
				{Type: "Resolved", Status: metav1.ConditionFalse, Reason: "RequiresReplacement"},
			}
			if mode == "cycle" {
				p.Status.Admission.Dependencies = []common.Binding{binding("StacksNetworkParticipant", p, "")}
			}
			objects := []client.Object{p}
			if mode != "child-missing" {
				objects = append(objects, account)
			}
			if mode != "source-missing" {
				source := &bitcoin.BitcoinNode{
					ObjectMeta: metav1.ObjectMeta{Name: "definition", Namespace: "test", UID: "definition-uid"},
				}
				if mode == "source-replaced" {
					source.UID = "new-definition"
				}
				objects = append(objects, source)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			err := newDependencyCheck(
				c,
				root,
			).validate(context.Background(), []common.Binding{binding("StacksNetworkParticipant", p, "")})
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode %s: %v", mode, err)
			}
		})
	}
}
