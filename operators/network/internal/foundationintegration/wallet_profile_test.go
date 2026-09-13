//go:build integration

package foundationintegration

import (
	"context"
	"strings"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyWalletProfileSchema rejects unsupported Core private wallets before resolution or bootstrap.
func verifyWalletProfileSchema(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "wallet-profile"}}
	if err := c.Create(ctx, namespace); err != nil {
		t.Fatal(err)
	}
	for name, watchOnly := range map[string]*bool{
		"omitted": nil,
		"public":  ptr.To(true),
		"private": ptr.To(false),
	} {
		wallet := &bitcoin.BitcoinWallet{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace.Name},
			Spec:       bitcoin.BitcoinWalletSpec{WatchOnly: watchOnly},
		}
		err := c.Create(ctx, wallet)
		if name == "private" {
			if !apierrors.IsInvalid(err) ||
				!strings.Contains(err.Error(), "only watch-only Core wallets are supported") {
				t.Fatalf("private wallet admitted or unclear rejection: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("supported %s rejected: %v", name, err)
		}
		wallet.Spec.WatchOnly = ptr.To(false)
		if err := c.Update(
			ctx,
			wallet,
		); !apierrors.IsInvalid(err) ||
			!strings.Contains(err.Error(), "only watch-only Core wallets are supported") {
			t.Fatalf("private update admitted: %v", err)
		}
	}
}
