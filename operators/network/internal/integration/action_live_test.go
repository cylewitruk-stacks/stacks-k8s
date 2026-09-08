//go:build live && bitcoinproduction

package integration

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// waitActionQuota permits the initial CRD discovery and quota resync interval before creating actions.
func waitActionQuota(t *testing.T, c client.Client, namespace, resource string) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Minute)
	for time.Now().Before(deadline) {
		quotas := &corev1.ResourceQuotaList{}
		if c.List(context.Background(), quotas, client.InNamespace(namespace)) == nil {
			for _, q := range quotas.Items {
				if _, ok := q.Status.Used[corev1.ResourceName("count/"+resource+".actions.stacks.org")]; ok {
					return
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("quota for %s did not initialize", resource)
}
