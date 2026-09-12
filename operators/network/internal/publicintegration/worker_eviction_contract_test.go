//go:build live

package publicintegration

import (
	"context"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// evictionContractFixture supplies API facts without creating a cluster client or sending eviction.
func evictionContractFixture(t *testing.T) (*harness, evictionSelection, *api.StacksNetwork, *api.StacksNetworkParticipant, *corev1.Pod) {
	t.Helper()
	root := waitRoot()
	session := api.WorkerSession{Pod: api.WorkerPodBinding{Kind: "Pod", Name: "worker", UID: "pod"}, ProfileDigest: "profile"}
	execution := api.WorkerExecutionStatus{PodUID: "pod", ProcessNonce: "process", ProfileDigest: "profile", Phase: "Active", ObservedAt: metav1.Now(), Transactions: &api.TransactionExecutionStatus{Included: 1}}
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "participant", Namespace: root.Namespace, UID: "participant", OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetwork", Name: root.Name, UID: root.UID, Controller: ptr.To(true)}}}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: root.UID, ParticipantName: "traffic", Kind: "StacksTransactionProduction"}, Status: api.ParticipantStatus{Execution: &execution}}
	root.Status.Identities = []api.InstanceIdentity{{Name: "traffic", UID: p.UID, Worker: session.DeepCopy()}}
	root.Status.ObservationPolicy = &api.ObservationPolicy{PollIntervalSeconds: 2, RPCAllowanceSeconds: 10}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: root.Namespace, UID: "pod", OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID, Controller: ptr.To(true)}}}, Status: corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: []corev1.ContainerStatus{{Name: "worker", ContainerID: "container", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 137}}}}}}
	selected := evictionSelection{Participant: objectIdentity(p), LogicalName: "traffic", Session: session, Execution: execution, ContainerID: "container"}
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return &harness{rootUID: root.UID, config: liveConfig{fixtureOptions: fixtureOptions{namespace: root.Namespace}}, c: fake.NewClientBuilder().WithScheme(scheme).Build()}, selected, root, p, pod
}

func TestWorkerEvictionPreservesUncertaintyAndRejectsReplacement(t *testing.T) {
	for _, mode := range []string{"terminated", "running", "unknown-status", "missing", "retained-termination", "replacement-pod", "second-pod", "process-restart", "replacement-binding", "missing-binding", "replacement-root"} {
		t.Run(mode, func(t *testing.T) {
			h, selected, root, p, pod := evictionContractFixture(t)
			objects := []client.Object{root, p}
			switch mode {
			case "running":
				pod.Status.Phase = corev1.PodRunning
				pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
			case "unknown-status":
				pod.Status.ContainerStatuses[0].State.Terminated.Reason = "ContainerStatusUnknown"
			case "retained-termination":
				root.Status.Identities[0].Worker.Disposal = &api.WorkerDisposal{Outcome: "Unsettled", Terminated: true}
			case "replacement-pod":
				pod.UID = "replacement"
			case "second-pod":
				extra := pod.DeepCopy()
				extra.Name = "replacement-worker"
				extra.UID = "replacement"
				objects = append(objects, extra)
			case "process-restart":
				pod.Status.ContainerStatuses[0].RestartCount = 1
			case "replacement-binding":
				root.Status.Identities[0].Worker.Pod.UID = "replacement"
			case "missing-binding":
				root.Status.Identities = nil
			case "replacement-root":
				root.UID = "replacement"
			}
			if mode != "missing" && mode != "retained-termination" {
				objects = append(objects, pod)
			}
			h.c = fake.NewClientBuilder().WithScheme(h.c.Scheme()).WithObjects(objects...).Build()
			view, err := h.readEvictionView(context.Background(), selected)
			rejected := mode == "replacement-pod" || mode == "second-pod" || mode == "process-restart" || mode == "replacement-binding" || mode == "missing-binding" || mode == "replacement-root"
			if (err != nil) != rejected {
				t.Fatalf("error=%v", err)
			}
			if !rejected {
				terminated := mode == "terminated" || mode == "retained-termination"
				if (view.Termination == "Terminated") != terminated {
					t.Fatalf("termination=%s", view.Termination)
				}
			}
		})
	}
}

func TestWorkerEvictionSelectionRequiresCurrentBoundExecution(t *testing.T) {
	for _, mode := range []string{"valid", "stale", "nonce-missing", "unbound", "shutdown", "different-pod", "failed"} {
		t.Run(mode, func(t *testing.T) {
			_, _, root, p, _ := evictionContractFixture(t)
			s := snapshot{At: time.Now().UTC(), Operation: "Running", Status: root.Status, Participants: []participantEvidence{{Identity: objectIdentity(p), Name: "traffic", Kind: p.Spec.Kind, Status: p.Status}}}
			switch mode {
			case "stale":
				s.At = s.At.Add(17 * time.Second)
			case "nonce-missing":
				p.Status.Execution.ProcessNonce = ""
			case "unbound":
				s.Status.Identities = nil
			case "shutdown":
				s.Status.Identities[0].Worker.Shutdown = &api.WorkerShutdown{Reason: "NetworkStopped"}
			case "different-pod":
				p.Status.Execution.PodUID = "replacement"
			case "failed":
				s.Status.Phase = "Failed"
			}
			_, err := selectEvictionWorker(s)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
