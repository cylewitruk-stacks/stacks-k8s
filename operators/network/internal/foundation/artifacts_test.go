package foundation

import (
	"context"
	"fmt"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// TestArtifactRetentionWaitsForParticipantDisposal checks each shared kind and acknowledgement loss.
func TestArtifactRetentionWaitsForParticipantDisposal(t *testing.T) {
	for _, object := range []client.Object{&api.StacksGenesis{}, &bitcoin.BitcoinExecution{}, &bitcoin.BitcoinInitialization{}} {
		t.Run(fmt.Sprintf("%T", object), func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, bitcoin.AddToScheme} {
				if err := add(scheme); err != nil {
					t.Fatal(err)
				}
			}
			object.SetName("artifact")
			object.SetNamespace("test")
			object.SetUID("artifact")
			object.SetFinalizers([]string{ArtifactFinalizer, "other.example/retain"})
			object.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetwork", Name: "network", UID: "root", Controller: ptr.To(true)}})
			p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "test", UID: "participant", Finalizers: []string{"domain.example/drain"}}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: "root"}}
			patches := 0
			fail := false
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(object, p).WithInterceptorFuncs(interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, o client.Object, patch client.Patch, opts ...client.PatchOption) error {
				patches++
				if fail {
					return apierrors.NewConflict(schema.GroupResource{Group: api.GroupVersion.Group, Resource: "artifacts"}, o.GetName(), fmt.Errorf("injected"))
				}
				return c.Patch(ctx, o, patch, opts...)
			}}).Build()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(c.Delete(ctx, object))
			must(c.Delete(ctx, p))
			r := ArtifactReconciler{Client: c, Reader: c, Object: object}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)}
			result, err := r.Reconcile(ctx, request)
			must(err)
			if result.RequeueAfter == 0 || patches != 0 {
				t.Fatal("artifact released while participant finalizer retains its consumer")
			}
			must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
			p.Finalizers = nil
			must(c.Update(ctx, p))
			fail = true
			if _, err := r.Reconcile(ctx, request); !apierrors.IsConflict(err) {
				t.Fatalf("conflict lost: %v", err)
			}
			must(c.Get(ctx, client.ObjectKeyFromObject(object), object))
			if len(object.GetFinalizers()) != 2 {
				t.Fatal("unacknowledged removal lost retention")
			}
			fail = false
			_, err = r.Reconcile(ctx, request)
			must(err)
			must(c.Get(ctx, client.ObjectKeyFromObject(object), object))
			if got := object.GetFinalizers(); len(got) != 1 || got[0] != "other.example/retain" {
				t.Fatal("foreign finalizer changed")
			}
		})
	}
}
