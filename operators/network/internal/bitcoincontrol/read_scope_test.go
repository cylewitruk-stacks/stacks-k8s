package bitcoincontrol

import (
	"context"
	"fmt"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// denySigningSecretReader makes private-key metadata permission an explicit test boundary.
type denySigningSecretReader struct {
	client.Reader
	reads int
}

func (r *denySigningSecretReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*metav1.PartialObjectMetadata); ok {
		r.reads++
		return fmt.Errorf("signing Secret metadata forbidden")
	}
	if _, ok := obj.(*corev1.Secret); ok && key.Name == "holder-key" {
		r.reads++
		return fmt.Errorf("signing Secret body forbidden")
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func TestBitcoinDispatchPublicScopeNeverReadsOrGrantsSigningSecrets(t *testing.T) {
	f := baselineFixture(t)
	ctx := context.Background()
	if err := stacks.AddToScheme(f.c.Scheme()); err != nil {
		t.Fatal(err)
	}
	account := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "holder", Namespace: "test", UID: "holder-uid"}, Status: common.ResolutionStatus{Digest: "holder-digest", Identity: &common.PublicIdentity{}, CredentialsRef: &common.SecretKeyRef{Name: "holder-key", Key: "privateKey"}, CredentialsUID: "private-secret-uid", Conditions: []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}}}}
	if err := f.c.Create(ctx, account); err != nil {
		t.Fatal(err)
	}
	var wallet bitcoin.BitcoinWallet
	if err := f.c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "miner"}, &wallet); err != nil {
		t.Fatal(err)
	}
	wallet.Spec.KeySource = &bitcoin.WalletKeySource{StacksMinerAccountRef: &common.NameRef{Name: account.Name}}
	wallet.Status.Dependencies = []common.Binding{{Kind: "StacksAccount", Name: account.Name, UID: account.UID, Fingerprint: account.Status.Digest}}
	if err := f.c.Update(ctx, &wallet); err != nil {
		t.Fatal(err)
	}
	reader := &denySigningSecretReader{Reader: f.c}
	if err := foundation.ValidatePublicParticipantAdmission(ctx, reader, f.root, f.node); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveBaselineInputs(ctx, reader, f.root, f.initial, f.production, true); err != nil {
		t.Fatal(err)
	}
	reads, err := publicReadScope(ctx, reader, f.node, f.initial)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := workerResources(f.node, f.record, f.initial, "worker:test", 1, reads)
	if err != nil {
		t.Fatal(err)
	}
	publicAccount := false
	for _, rule := range objects[1].(*rbacv1.Role).Rules {
		if len(rule.ResourceNames) == 0 {
			t.Fatal("public read grant became namespace-wide")
		}
		for _, resource := range rule.Resources {
			for _, name := range rule.ResourceNames {
				if resource == "secrets" && name != "rpc" && name != "config" {
					t.Fatalf("private signing Secret granted: %+v", rule)
				}
				publicAccount = publicAccount || resource == "stacksaccounts" && name == account.Name
			}
		}
	}
	if reader.reads != 0 || !publicAccount {
		t.Fatalf("private reads=%d public account grant=%v", reader.reads, publicAccount)
	}
	if _, err := baselineInputs(ctx, reader, f.root, f.initial, f.production); err == nil || reader.reads == 0 {
		t.Fatal("scheduler silently omitted its credential metadata validation")
	}
}
