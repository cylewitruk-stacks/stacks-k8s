//go:build integration

package workers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// patchCounter observes requests even when the API server deduplicates their writes.
type patchCounter struct {
	client.Client
	merges, applies int
}

func (c *patchCounter) Patch(ctx context.Context, o client.Object, p client.Patch, opts ...client.PatchOption) error {
	if p.Type() == types.MergePatchType {
		c.merges++
	}
	if p.Type() == types.ApplyPatchType {
		c.applies++
	}
	return c.Client.Patch(ctx, o, p, opts...)
}

func TestDefaultedWorkersAvoidCredentialMergeChurn(t *testing.T) {
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	env := &envtest.Environment{DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.37.0", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest")}
	cfg, err := env.Start()
	must(err)
	t.Cleanup(func() { must(env.Stop()) })
	scheme := runtime.NewScheme()
	must(clientgoscheme.AddToScheme(scheme))
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	must(err)
	ctx := context.Background()
	must(c.Create(ctx, &core.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "patch-count"}}))
	for _, component := range []string{"bitcoin-production", "stacks-transactions", "stacks-contracts", "stacks-stacking"} {
		t.Run(component, func(t *testing.T) {
			owner := &core.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: component, Namespace: "patch-count"}}
			must(c.Create(ctx, owner))
			p := plan{owner: owner, component: component, network: "network", networkUID: "network-uid"}
			if component == "bitcoin-production" || component == "stacks-transactions" {
				p.credentials = "account"
			} else {
				p.keys = []stacks.ArtifactReference{{Name: "account", Key: "key.json"}}
			}
			settings := Settings{Image: "operator:test", SDKImage: "sdk:test", PullPolicy: core.PullIfNotPresent}
			counted := &patchCounter{Client: c}
			r := Reconciler{Client: counted, Reader: c, Scheme: scheme}
			desired := func() *apps.Deployment {
				d := deployment(p, settings)
				d.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}
				return d
			}
			must(r.apply(ctx, owner, desired()))
			current := &apps.Deployment{}
			must(c.Get(ctx, client.ObjectKeyFromObject(desired()), current))
			if *current.Spec.Template.Spec.Volumes[0].Secret.DefaultMode != 0644 {
				t.Fatal("fixture lacks server-compatible Secret default")
			}
			counted.merges, counted.applies = 0, 0
			for i := 0; i < 2; i++ {
				must(r.apply(ctx, owner, desired()))
			}
			if counted.merges != 0 || counted.applies != 2 {
				t.Fatalf("unchanged apply requests: merge=%d apply=%d", counted.merges, counted.applies)
			}
			p.credentials = ""
			p.keys = nil
			must(r.apply(ctx, owner, desired()))
			must(c.Get(ctx, client.ObjectKeyFromObject(desired()), current))
			if len(current.Spec.Template.Spec.Volumes) != 0 || len(current.Spec.Template.Spec.Containers[0].VolumeMounts) != 0 {
				t.Fatal("withdrawal retained credential mounts")
			}
			counted.merges = 0
			must(r.apply(ctx, owner, desired()))
			if counted.merges != 0 {
				t.Fatal("keyless worker generated another credential patch")
			}
		})
	}
}
