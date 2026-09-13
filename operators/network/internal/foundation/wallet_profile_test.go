package foundation

import (
	"errors"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestWalletProfileAcceptsOnlyPublicCoreWallets(t *testing.T) {
	for _, watchOnly := range []*bool{nil, ptr.To(true), ptr.To(false)} {
		err := ValidateWalletProfile(bitcoin.BitcoinWalletSpec{WatchOnly: watchOnly})
		unsupported := watchOnly != nil && !*watchOnly
		if errors.Is(err, ErrUnsupportedWalletProfile) != unsupported {
			t.Fatalf("watchOnly=%v err=%v", watchOnly, err)
		}
	}
}

func TestUnsupportedWalletNeverResolvesOrEntersCandidateAdmission(t *testing.T) {
	for name, key := range map[string]*bitcoin.WalletKeySource{
		"generated": nil,
		"imported":  {SecretRef: &common.SecretKeyRef{Name: "absent", Key: "descriptor"}},
		"derived":   {StacksMinerAccountRef: &common.NameRef{Name: "absent"}},
	} {
		t.Run(name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{
				bitcoin.AddToScheme,
				corev1.AddToScheme,
				batchv1.AddToScheme,
			} {
				if err := add(scheme); err != nil {
					t.Fatal(err)
				}
			}
			wallet := &bitcoin.BitcoinWallet{
				ObjectMeta: metav1.ObjectMeta{Name: "unsupported", Namespace: "test", UID: "wallet", Generation: 1},
				Spec:       bitcoin.BitcoinWalletSpec{WatchOnly: ptr.To(false), KeySource: key},
				Status: common.ResolutionStatus{
					Digest:             "prior-public-evidence",
					ObservedGeneration: 1,
					Identity:           &common.PublicIdentity{},
					Conditions:         []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}},
				},
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(wallet).WithObjects(wallet).Build()
			p := &candidate{
				instance: &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Namespace: wallet.Namespace}},
			}
			if err := p.wallet(
				t.Context(),
				c,
				&common.NameRef{Name: wallet.Name},
			); !errors.Is(
				err,
				ErrUnsupportedWalletProfile,
			) {
				t.Fatalf("stale resolved candidate admitted: %v", err)
			}
			check := newDependencyCheck(
				c,
				&api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Namespace: wallet.Namespace}},
			)
			if err := check.validate(
				t.Context(),
				[]common.Binding{
					{Kind: "BitcoinWallet", Name: wallet.Name, UID: wallet.UID, Fingerprint: wallet.Status.Digest},
				},
			); !errors.Is(
				err,
				ErrUnsupportedWalletProfile,
			) {
				t.Fatalf("stale resolved dependency admitted: %v", err)
			}
			r := IdentityReconciler{Client: c, Reader: c, Scheme: scheme, Wallet: true}
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(wallet)})
			if err != nil || result.RequeueAfter != 0 {
				t.Fatalf("permanent unsupported input retried: %+v %v", result, err)
			}
			if err := c.Get(t.Context(), client.ObjectKeyFromObject(wallet), wallet); err != nil {
				t.Fatal(err)
			}
			cond := meta.FindStatusCondition(wallet.Status.Conditions, "Resolved")
			if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "UnsupportedWalletProfile" ||
				cond.ObservedGeneration != wallet.Generation ||
				wallet.Status.Digest != "prior-public-evidence" {
				t.Fatal("unsupported profile failed to preserve evidence and withdraw resolution", wallet.Status)
			}
			for _, list := range []client.ObjectList{
				&corev1.SecretList{},
				&corev1.ConfigMapList{},
				&batchv1.JobList{},
			} {
				if err := c.List(t.Context(), list); err != nil {
					t.Fatal(err)
				}
				items, err := meta.ExtractList(list)
				if err != nil || len(items) != 0 {
					t.Fatal("unsupported profile allocated resolver resources", list, err)
				}
			}
		})
	}
}
