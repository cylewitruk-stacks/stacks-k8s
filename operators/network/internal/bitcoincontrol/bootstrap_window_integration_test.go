//go:build integration

package bitcoincontrol

import (
	"context"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// TestBootstrapWindowAPI persists both sides of offer publication and accounting in
// a real API server. Actor identity and RPC are fixtures, not live workload evidence.
func TestBootstrapWindowAPI(t *testing.T) {
	apiClient := controlIntegrationClient(t)
	f := newFixture(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(apiClient.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}))
	initial := f.readInitial(t)
	initial.UID, initial.ResourceVersion = "", ""
	must(apiClient.Create(t.Context(), initial))
	execution := f.readRecord(t)
	execution.UID, execution.ResourceVersion = "", ""
	must(apiClient.Create(t.Context(), execution))
	root := f.root.DeepCopy()
	must(f.c.Get(t.Context(), client.ObjectKeyFromObject(root), root))
	root.Status.Bitcoin.InitializationRef.UID = initial.UID
	root.Status.Bitcoin.ExecutionRefs[0].UID = execution.UID
	must(f.c.Status().Update(t.Context(), root))
	f.initial, f.record = initial, execution
	f.worker.Input.RecordUID = execution.UID
	isRecord := func(obj client.Object) bool {
		switch obj.(type) {
		case *bitcoin.BitcoinInitialization, *bitcoin.BitcoinExecution:
			return true
		}
		return false
	}
	f.c = interceptor.NewClient(f.c.(client.WithWatch), interceptor.Funcs{
		Get: func(
			ctx context.Context, c client.WithWatch, key client.ObjectKey,
			obj client.Object, opts ...client.GetOption,
		) error {
			if isRecord(obj) {
				return apiClient.Get(ctx, key, obj, opts...)
			}
			return c.Get(ctx, key, obj, opts...)
		},
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if isRecord(obj) {
				return apiClient.Update(ctx, obj, opts...)
			}
			return c.Update(ctx, obj, opts...)
		},
		SubResourceUpdate: func(
			ctx context.Context, c client.Client, sub string, obj client.Object,
			opts ...client.SubResourceUpdateOption,
		) error {
			if isRecord(obj) {
				return apiClient.SubResource(sub).Update(ctx, obj, opts...)
			}
			return c.SubResource(sub).Update(ctx, obj, opts...)
		},
	})
	f.worker.Client, f.worker.Reader = f.c, f.c
	exerciseDelayedBootstrap(t, f)
}
