package networkruntime

import (
	"context"
	"strings"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRootProjectsShutdownBeforeDeletionAndRetainsMissingWorkerFailure(t *testing.T) {
	for _, mode := range []string{"stop", "delete", "missing"} {
		t.Run(mode, func(t *testing.T) {
			root := rootFixture()
			root.Spec.Operation = "Running"
			p := participantFixture(root)
			p.Spec.Kind = "StacksTransactionProduction"
			root.Spec.Participants = []api.Participant{{Name: p.Spec.ParticipantName, Kind: p.Spec.Kind}}
			digest := "sha256:" + strings.Repeat("a", 64)
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: stacksworker.Name(p), Namespace: p.Namespace, UID: "worker", Labels: map[string]string{"network.stacks.org/network-uid": string(root.UID), "network.stacks.org/participant-uid": string(p.UID)}, Annotations: map[string]string{"network.stacks.org/worker-profile": digest}, OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID, Controller: ptr.To(true)}}}}
			root.Status.Identities = []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: p.UID, Worker: &api.WorkerSession{Pod: api.WorkerPodBinding{Kind: "Pod", Name: pod.Name, UID: pod.UID}, ProfileDigest: digest}}}
			objects := []client.Object{p, pod}
			switch mode {
			case "stop":
				root.Spec.Operation = "Stopped"
			case "delete":
				now := metav1.Now()
				root.DeletionTimestamp = &now
			case "missing":
				objects = []client.Object{p}
			}
			scheme := runtime.NewScheme()
			_ = api.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			r := &Reconciler{Client: c, Reader: c, Scheme: scheme}
			_, err := r.Reconcile(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			session := root.Status.Identities[0].Worker
			if mode == "missing" {
				if root.Status.Phase != "Failed" || !meta.IsStatusConditionTrue(root.Status.Conditions, "Failed") || session.Pod.UID != "worker" {
					t.Fatal("worker loss lost identity or failed to latch")
				}
				return
			}
			expected := "NetworkStopped"
			if mode == "delete" {
				expected = "NetworkDeleting"
			}
			if session.Shutdown == nil || string(session.Shutdown.Reason) != expected || session.Disposal != nil {
				t.Fatalf("incorrect disposal ordering: %+v", session)
			}
			if root.Status.Phase == "Stopped" {
				t.Fatal("stop claimed before worker acknowledgement")
			}
		})
	}
}

func TestNetworkPauseRequiresCurrentWorkerAcknowledgementWithoutClaimingSettlement(t *testing.T) {
	for _, mode := range []string{"acknowledged", "pending", "missing", "stale", "old-root", "old-control", "other-pod", "active"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			root, _, participants := cohortFixture(now)
			e := participants[0].Status.Execution
			e.Phase = "Paused"
			e.NetworkGeneration = root.Generation
			e.ObservedAt = metav1.NewTime(now)
			switch mode {
			case "pending":
				e.Pending = 1
			case "missing":
				participants = nil
			case "stale":
				e.ObservedAt = metav1.NewTime(now.Add(-17 * time.Second))
			case "old-root":
				e.NetworkGeneration--
			case "old-control":
				e.ObservedGeneration--
			case "other-pod":
				e.PodUID = "old"
			case "active":
				e.Phase = "Active"
			}
			if got := workersPaused(root, participants, now); got != (mode == "acknowledged" || mode == "pending") {
				t.Fatalf("pause acknowledged=%v", got)
			}
		})
	}
}
