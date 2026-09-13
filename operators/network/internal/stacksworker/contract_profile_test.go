package stacksworker

import (
	"context"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestSingleAccountProfilesKeepPrivateInputsScoped(t *testing.T) {
	for _, kind := range []api.ParticipantKind{"StacksContractSet", "StacksFaucet"} {
		t.Run(string(kind), func(t *testing.T) {
			ctx := context.Background()
			root, p, _, _ := fixture(t)
			p.Spec.Kind = kind
			keyRole := "deployer"
			p.Status.Admission.Configuration = api.Configuration{
				StacksContractSet: &stacks.StacksContractSetSpec{
					DeployerAccountRef: &common.NameRef{Name: "deployer"},
					Initialization: &stacks.RegistryInitialization{
						SignerAccountRefs:      []common.NameRef{{Name: "public-a"}, {Name: "public-b"}},
						AggregateKeyAccountRef: common.NameRef{Name: "public-aggregate"},
					},
				},
			}
			if kind == "StacksFaucet" {
				keyRole = "sender"
				p.Status.Admission.Configuration = api.Configuration{
					StacksFaucet: &stacks.StacksFaucetSpec{
						AccountRef:    &common.NameRef{Name: "deployer"},
						TargetNodeRef: &common.NameRef{Name: "target"},
					},
				}
			}
			account := &stacks.StacksAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "deployer", Namespace: p.Namespace, UID: "deployer", Generation: 1},
				Status: common.ResolutionStatus{
					ObservedGeneration: 1,
					Digest:             "public",
					Identity:           &common.PublicIdentity{Address: "STPUBLIC", PublicKey: "02public"},
					CredentialsRef:     &common.SecretKeyRef{Name: "deployer-key", Key: "privateKey"},
					CredentialsUID:     "key",
					Conditions:         []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}},
				},
			}
			key := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "deployer-key", Namespace: p.Namespace, UID: "key"},
				Data:       map[string][]byte{"privateKey": []byte("never-read")},
			}
			g := &api.StacksGenesis{
				ObjectMeta: metav1.ObjectMeta{
					Name:      root.Status.GenesisRef.Name,
					Namespace: p.Namespace,
					UID:       root.Status.GenesisRef.UID,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: api.GroupVersion.String(),
							Kind:       "StacksNetwork",
							Name:       root.Name,
							UID:        root.UID,
							Controller: ptr.To(true),
						},
					},
				},
			}
			root.Status.GenesisDigest = foundation.Digest(g.Spec.Chain)
			p.Status.Admission.Dependencies = []common.Binding{
				{Kind: "StacksAccount", Name: account.Name, UID: account.UID, Fingerprint: account.Status.Digest},
				{Kind: "StacksAccount", Name: "public-a", UID: "a"},
				{Kind: "StacksAccount", Name: "public-b", UID: "b"},
				{Kind: "StacksAccount", Name: "public-aggregate", UID: "aggregate"},
			}
			c := &publicOnlyClient{Client: fakeClient(t, root, p, account, key, g)}
			profile, err := (Profiles{Client: c, Reader: c, Image: "worker:fixed"}).Resolve(ctx, root, p)
			if err != nil {
				t.Fatal(err)
			}
			if c.privateReads != 0 || len(profile.Keys) != 1 || profile.Keys[0].Role != keyRole ||
				profile.Keys[0].Secret.UID != key.UID {
				t.Fatal("single-account profile expanded private authority")
			}
			found := map[string]bool{}
			for _, read := range profile.Reads {
				found[read.Name] = true
			}
			for _, name := range []string{"public-a", "public-b", "public-aggregate"} {
				if !found[name] {
					t.Fatalf("public registry identity %s not readable", name)
				}
			}
		})
	}
}
