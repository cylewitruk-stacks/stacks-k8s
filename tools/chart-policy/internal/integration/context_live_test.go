//go:build live

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// logCycleContext captures sequential public observations without changing test outcomes.
// A separate short context permits diagnostics even when the main test deadline expires.
func (f *faultFixture) logCycleContext(stage string) {
	f.t.Helper()
	f.t.Logf("cycle context stage=%q capturedAt=%s (sequential, not an atomic snapshot)", stage, time.Now().UTC().Format(time.RFC3339Nano))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Use the same observer-only RPC as other Core probes, without a fatal Pod lookup.
	var height int64
	if err := f.observerRPC(ctx, f.network+"-bitcoin-0", "127.0.0.1", "18443", "getblockcount", &height); err != nil {
		f.t.Log("cycle context Core height unavailable")
	} else {
		f.t.Logf("cycle context Core height=%d", height)
	}

	for _, actor := range []string{"miner", "signer-node"} {
		pod := &corev1.Pod{}
		if err := f.admin.Get(ctx, client.ObjectKey{Namespace: f.namespace, Name: f.network + "-" + actor + "-0"}, pod); err != nil {
			f.t.Logf("cycle context actor=%s Pod identity unavailable", actor)
			continue
		}
		output, err := f.stacksQuery(ctx, pod.Name, "pox")
		if err != nil {
			f.t.Logf("cycle context actor=%s PodUID=%s PoX unavailable", actor, pod.UID)
			continue
		}
		pox, err := decodePoX(output)
		if err != nil {
			f.t.Logf("cycle context actor=%s PodUID=%s gap=%s", actor, pod.UID, err)
			continue
		}
		encoded, _ := json.Marshal(pox)
		f.t.Logf("cycle context actor=%s PodUID=%s phaseFromReportedBounds=%s pox=%s", actor, pod.UID, pox.phase(), encoded)
	}
}

// stacksQuery reads one public node endpoint through the explicitly selected administrator proxy.
func (f *faultFixture) stacksQuery(parent context.Context, pod, endpoint string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s:20443/proxy/v2/%s", f.namespace, pod, endpoint)
	return f.command(ctx, "get", "--raw", path).Output()
}
