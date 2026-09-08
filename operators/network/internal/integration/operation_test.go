//go:build integration

package integration

import (
	"context"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/accountledger"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"testing"
)

// verifyManagedOperation checks real API immutability, exclusive account assignment and retained identity.
func verifyManagedOperation(t *testing.T, ctx context.Context, c client.Client) {
	parent := bitcoinNetwork("managed")
	parent.Spec.StacksNodes = []network.StacksNodeTemplate{{Name: "ingress", Role: "follower", BitcoinNodeRef: "bitcoin", Config: generatedStacks()}}
	address := "ST1PQHQKV0RJXZFY1DGX8MNSNYVE3VGZJSRTPGZGM"
	digest := "sha256:" + strings.Repeat("a", 64)
	ref := stacks.ArtifactReference{Name: "artifact", Key: "key", Digest: digest}
	parent.Spec.Operation = &network.NetworkOperation{Accounts: []stacks.ManagedAccountPolicy{{Name: "deployer", Address: address, Target: "ingress", ConfigDigest: digest, KeySecretRef: ref, ConsumerKind: "StacksContractSet", Consumer: "contracts"}}, ContractSets: []stacks.ContractSetPolicy{{Name: "contracts", Account: "deployer", Contracts: []stacks.ContractArtifact{{Name: "example", SourceRef: ref, ClarityVersion: 3}}}}}
	original := parent.Spec.Operation.DeepCopy()
	for _, tc := range []struct {
		name   string
		mutate func(*network.StacksNetwork)
	}{
		{"missing-consumer", func(n *network.StacksNetwork) { n.Spec.Operation.Accounts[0].Consumer = "missing" }},
		{"missing-account", func(n *network.StacksNetwork) { n.Spec.Operation.ContractSets[0].Account = "missing" }},
		{"unknown-ingress", func(n *network.StacksNetwork) { n.Spec.Operation.Accounts[0].Target = "missing" }},
		{"duplicate-owner", func(n *network.StacksNetwork) {
			p := n.Spec.Operation.Accounts[0]
			p.Name = "other"
			n.Spec.Operation.Accounts = append(n.Spec.Operation.Accounts, p)
		}},
		{"shared-transfer", func(n *network.StacksNetwork) {
			n.Spec.StacksTransactionProduction = &stacks.TransferPolicy{Target: "ingress", Sender: address, Recipient: "ST2CY5V39NHDPWSXMW9QDT3HC3GD6Q6XX4CFRK9AG", AmountMicroSTX: 1, FeeMicroSTX: 1000, IntervalSeconds: 10}
		}},
		{"missing-dependency", func(n *network.StacksNetwork) {
			n.Spec.Operation.ContractSets[0].Contracts[0].DependsOn = []string{"missing"}
		}},
	} {
		t.Run("managed-admission-"+tc.name, func(t *testing.T) {
			candidate := parent.DeepCopy()
			candidate.Name = "managed-" + tc.name
			tc.mutate(candidate)
			if err := c.Create(ctx, candidate); !apierrors.IsInvalid(err) {
				t.Fatalf("invalid declaration admitted: %v", err)
			}
		})
	}
	must(t, c.Create(ctx, parent))
	account := &stacks.StacksAccount{}
	set := &stacks.StacksContractSet{}
	accountKey := client.ObjectKey{Namespace: testNamespace, Name: "managed-account-deployer"}
	setKey := client.ObjectKey{Namespace: testNamespace, Name: "managed-contracts-contracts"}
	eventually(t, "managed account and consumer pinned", func() bool {
		return c.Get(ctx, client.ObjectKeyFromObject(parent), parent) == nil && len(parent.Status.Capabilities) == 2 && c.Get(ctx, accountKey, account) == nil && c.Get(ctx, setKey, set) == nil
	})
	uid := account.UID
	changed := account.DeepCopy()
	changed.Spec.Policy.Address = "ST2CY5V39NHDPWSXMW9QDT3HC3GD6Q6XX4CFRK9AG"
	if err := c.Update(ctx, changed); !apierrors.IsInvalid(err) {
		t.Fatalf("account reassignment admitted: %v", err)
	}
	altered := set.DeepCopy()
	altered.Spec.Policy.Contracts[0].SourceRef.Digest = "sha256:" + strings.Repeat("b", 64)
	if err := c.Update(ctx, altered); !apierrors.IsInvalid(err) {
		t.Fatalf("contract source replacement admitted: %v", err)
	}
	must(t, updateNetworkWithRetry(ctx, c, parent, func(n *network.StacksNetwork) { n.Spec.Operation = nil }))
	must(t, updateNetworkWithRetry(ctx, c, parent, func(n *network.StacksNetwork) { n.Spec.Operation = original.DeepCopy() }))
	eventually(t, "remove/readd keeps account authority", func() bool { return c.Get(ctx, accountKey, account) == nil && account.UID == uid })
	// Arm a retained ledger; deleting it cannot reopen nonce authority.
	must(t, c.Get(ctx, accountKey, account))
	account.Finalizers = []string{accountledger.Finalizer}
	must(t, c.Update(ctx, account))
	account.Status.Phase = "Pending"
	account.Status.Transaction = &stacks.AccountTransaction{Ordinal: 1, OperationDigest: digest, ConsumerUID: string(set.UID), TxID: strings.Repeat("c", 64), Nonce: 0, TargetUID: "actor", PodUID: "pod", ContainerID: "container", AuthorizedAt: metav1.Now()}
	must(t, c.Status().Update(ctx, account))
	must(t, c.Get(ctx, accountKey, account))
	if account.Status.Transaction == nil || account.Status.Transaction.TxID != strings.Repeat("c", 64) {
		t.Fatal("authorization was pruned")
	}
	must(t, c.Delete(ctx, account))
	must(t, c.Get(ctx, accountKey, account))
	if account.DeletionTimestamp.IsZero() {
		t.Fatal("real finalized account deletion did not begin")
	}
	// An unavailable capability does not block unrelated actor updates.
	must(t, updateNetworkWithRetry(ctx, c, parent, func(n *network.StacksNetwork) { n.Spec.Defaults.BitcoinImage = "bitcoin:changed" }))
	node := &network.BitcoinNode{}
	eventually(t, "topology independent of account failure", func() bool {
		return c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "managed-bitcoin"}, node) == nil && node.Spec.Image == "bitcoin:changed"
	})
	must(t, c.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	must(t, c.Delete(ctx, parent))
	eventually(t, "environment removal releases account finalizer", func() bool { return apierrors.IsNotFound(c.Get(ctx, accountKey, account)) })
	// Envtest has no garbage collector; remove the unfinalized consumer explicitly.
	must(t, c.Delete(ctx, set))
}
