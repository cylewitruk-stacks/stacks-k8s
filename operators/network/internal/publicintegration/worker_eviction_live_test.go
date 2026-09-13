//go:build live

package publicintegration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestPublicBoundWorkerEviction qualifies deliberate terminal worker loss in its own fresh fixture.
func TestPublicBoundWorkerEviction(t *testing.T) {
	if os.Getenv("STACKS_PUBLIC_LIVE") != "1" || os.Getenv("STACKS_PUBLIC_WORKER_EVICTION") != "1" {
		t.Skip("set STACKS_PUBLIC_LIVE=1 and STACKS_PUBLIC_WORKER_EVICTION=1 for a separate terminal-failure fixture")
	}
	config, err := readConfig()
	if err != nil {
		t.Fatal(err)
	}
	declared, err := loadFixture(config.fixtureOptions)
	if err != nil {
		t.Fatal(err)
	}
	h, err := newHarness(t, config, declared)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.cleanup(); err != nil {
			t.Errorf(
				"eviction fixture cleanup incomplete: %v; namespace=%s UID=%s evidence=%s",
				err,
				config.namespace,
				h.namespaceUID,
				h.evidence,
			)
		}
	})
	signals, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithTimeout(signals, config.timeout)
	defer cancel()
	if err := h.create(ctx); err != nil {
		t.Fatal(err)
	}
	initialized, err := h.wait(ctx, "eviction-initialized", config.timeout, true, func(s snapshot) (bool, error) {
		_, ready := progress(s)
		return ready && condition(s, "Initialized", metav1.ConditionTrue) &&
			condition(s, "Operational", metav1.ConditionTrue), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.awaitProgress(ctx, "eviction-native-progress", initialized); err != nil {
		t.Fatal(err)
	}
	if err := h.evictBoundWorker(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.cleanup(); err != nil {
		t.Fatal(err)
	}
	t.Logf(
		"bound management Pod evicted through policy/v1; Failed latched, binding "+
			"retained, no replacement or resumed execution observed; normal disposal "+
			"completed; evidence=%s",
		h.evidence,
	)
}

// evictionSelection pins the live management session and process before submitting eviction.
type evictionSelection struct {
	Participant identity                  `json:"participant"`
	LogicalName string                    `json:"logicalName"`
	Session     api.WorkerSession         `json:"session"`
	Execution   api.WorkerExecutionStatus `json:"execution"`
	ContainerID string                    `json:"containerID"`
}

// selectEvictionWorker requires fresh active traffic execution, making resumed work observable.
func selectEvictionWorker(s snapshot) (evictionSelection, error) {
	if s.Operation != "Running" || s.Deleting || failed(s) || s.Status.ObservationPolicy == nil {
		return evictionSelection{}, fmt.Errorf("eviction requires a running healthy root")
	}
	freshness := time.Duration(
		3*s.Status.ObservationPolicy.PollIntervalSeconds+s.Status.ObservationPolicy.RPCAllowanceSeconds,
	) * time.Second
	for _, p := range s.Participants {
		e := p.Status.Execution
		if p.Kind != "StacksTransactionProduction" || e == nil || e.Phase != "Active" || e.ProcessNonce == "" ||
			e.Transactions == nil ||
			e.Transactions.Included == 0 ||
			e.ObservedAt.IsZero() ||
			e.ObservedAt.After(s.At) ||
			s.At.Sub(e.ObservedAt.Time) > freshness {
			continue
		}
		for _, id := range s.Status.Identities {
			session := id.Worker
			if id.UID == p.Identity.UID && id.Name == p.Name && !id.Removing && session != nil &&
				session.Shutdown == nil &&
				session.Disposal == nil &&
				session.Pod.Kind == "Pod" &&
				session.Pod.UID == e.PodUID &&
				session.ProfileDigest == e.ProfileDigest {
				return evictionSelection{
					Participant: p.Identity,
					LogicalName: p.Name,
					Session:     *session.DeepCopy(),
					Execution:   *e.DeepCopy(),
				}, nil
			}
		}
	}
	return evictionSelection{}, fmt.Errorf("no fresh active bound transaction worker")
}

// evictBoundWorker submits exactly one UID-preconditioned native eviction, never a direct deletion.
func (h *harness) evictBoundWorker(ctx context.Context) error {
	var selected evictionSelection
	_, err := h.wait(ctx, "eviction-worker-selected", 30*time.Second, true, func(s snapshot) (bool, error) {
		var err error
		selected, err = selectEvictionWorker(s)
		return err == nil, nil
	})
	if err != nil {
		return err
	}
	var participant api.StacksNetworkParticipant
	if err := h.c.Get(
		ctx,
		client.ObjectKey{Namespace: h.config.namespace, Name: selected.Participant.Name},
		&participant,
	); err != nil {
		return err
	}
	if participant.UID != selected.Participant.UID || participant.Spec.NetworkUID != h.rootUID ||
		participant.DeletionTimestamp != nil {
		return fmt.Errorf("eviction participant identity changed")
	}
	var pod corev1.Pod
	if err := h.c.Get(
		ctx,
		client.ObjectKey{Namespace: h.config.namespace, Name: selected.Session.Pod.Name},
		&pod,
	); err != nil {
		return err
	}
	if pod.UID != selected.Session.Pod.UID || !metav1.IsControlledBy(&pod, &participant) ||
		pod.DeletionTimestamp != nil ||
		pod.Spec.RestartPolicy != corev1.RestartPolicyNever ||
		pod.Status.Phase != corev1.PodRunning ||
		len(pod.Spec.Containers) != 1 ||
		len(pod.Spec.InitContainers) != 0 ||
		len(pod.Spec.EphemeralContainers) != 0 ||
		pod.Annotations["network.stacks.org/worker-profile"] != selected.Session.ProfileDigest {
		return fmt.Errorf("eviction target is not the exact current standalone worker")
	}
	retained := false
	for _, f := range pod.Finalizers {
		retained = retained || f == "network.stacks.org/stacks-worker-termination"
	}
	if !retained {
		return fmt.Errorf("bound worker lacks termination-evidence finalizer")
	}
	for _, c := range pod.Status.ContainerStatuses {
		if c.Name == "worker" && c.State.Running != nil && c.RestartCount == 0 {
			selected.ContainerID = c.ContainerID
		}
	}
	if selected.ContainerID == "" {
		return fmt.Errorf("bound worker process is not a fresh running instance")
	}
	if err := h.event("worker-eviction-request", map[string]any{"selected": selected, "pod": pod}); err != nil {
		return err
	}
	eviction := &policyv1.Eviction{
		ObjectMeta:    metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name},
		DeleteOptions: &metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &pod.UID}},
	}
	if err := h.c.SubResource("eviction").Create(ctx, &pod, eviction); err != nil {
		return fmt.Errorf("native eviction failed (no alternate delete attempted): %w", err)
	}
	return h.awaitEvictedWorker(ctx, selected)
}

// evictionView records public facts even after ordinary snapshots omit failed-root participants.
type evictionView struct {
	At          time.Time                     `json:"observedAt"`
	Root        *api.StacksNetwork            `json:"root"`
	Participant *api.StacksNetworkParticipant `json:"participant"`
	Pod         *corev1.Pod                   `json:"pod,omitempty"`
	Session     *api.WorkerSession            `json:"session,omitempty"`
	Termination string                        `json:"termination"`
}

// readEvictionView rejects replacement Pods at either the deterministic name or another participant-owned name.
func (h *harness) readEvictionView(ctx context.Context, selected evictionSelection) (evictionView, error) {
	v := evictionView{
		At:          time.Now().UTC(),
		Root:        &api.StacksNetwork{},
		Participant: &api.StacksNetworkParticipant{},
		Termination: "TerminationUnknown",
	}
	if err := h.c.Get(ctx, client.ObjectKey{
		Namespace: h.config.namespace,
		Name:      "network",
	}, v.Root); err != nil {
		return v, err
	}
	if v.Root.UID != h.rootUID {
		return v, fmt.Errorf("eviction root identity changed")
	}
	if err := h.c.Get(
		ctx,
		client.ObjectKey{Namespace: h.config.namespace, Name: selected.Participant.Name},
		v.Participant,
	); err != nil {
		return v, err
	}
	if v.Participant.UID != selected.Participant.UID || v.Participant.Spec.NetworkUID != h.rootUID ||
		!metav1.IsControlledBy(v.Participant, v.Root) {
		return v, fmt.Errorf("eviction participant identity changed")
	}
	for _, id := range v.Root.Status.Identities {
		if id.Name == selected.LogicalName {
			if id.UID != selected.Participant.UID {
				return v, fmt.Errorf("evicted participant rebound")
			}
			v.Session = id.Worker.DeepCopy()
		}
	}
	if v.Session == nil || v.Session.Pod != selected.Session.Pod ||
		v.Session.ProfileDigest != selected.Session.ProfileDigest {
		return v, fmt.Errorf("evicted worker binding changed or disappeared")
	}
	var pods corev1.PodList
	if err := h.c.List(ctx, &pods, client.InNamespace(h.config.namespace)); err != nil {
		return v, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Name == selected.Session.Pod.Name || metav1.IsControlledBy(pod, v.Participant) {
			if pod.UID != selected.Session.Pod.UID {
				return v, fmt.Errorf("replacement Pod %s appeared after eviction", pod.Name)
			}
			v.Pod = pod.DeepCopy()
		}
	}
	if v.Pod != nil {
		if !metav1.IsControlledBy(v.Pod, v.Participant) {
			return v, fmt.Errorf("evicted Pod ownership changed")
		}
		for _, c := range v.Pod.Status.ContainerStatuses {
			if c.Name == "worker" {
				if c.ContainerID != "" && c.ContainerID != selected.ContainerID || c.RestartCount != 0 {
					return v, fmt.Errorf("bound worker process restarted after eviction")
				}
				if (v.Pod.Status.Phase == corev1.PodSucceeded || v.Pod.Status.Phase == corev1.PodFailed) &&
					c.State.Terminated != nil &&
					c.State.Terminated.Reason != "ContainerStatusUnknown" &&
					c.ContainerID == selected.ContainerID {
					v.Termination = "Terminated"
				}
			}
		}
	} else if v.Session.Disposal != nil && v.Session.Disposal.Terminated {
		v.Termination = "Terminated"
	}
	return v, nil
}

// awaitEvictedWorker starts the no-resumption window only after exact process termination is known.
func (h *harness) awaitEvictedWorker(ctx context.Context, selected evictionSelection) error {
	ctx, cancel := context.WithTimeout(ctx, h.config.progressTimeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var heldSince time.Time
	var settled *api.WorkerExecutionStatus
	for {
		view, err := h.readEvictionView(ctx, selected)
		if data, encodeErr := json.MarshalIndent(view, "", "  "); encodeErr == nil {
			if writeErr := os.WriteFile(
				filepath.Join(h.evidence, "worker-eviction-latest.json"),
				data,
				0o600,
			); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			return err
		}
		if view.Root.Spec.Operation != "Running" || view.Root.DeletionTimestamp != nil {
			return fmt.Errorf("root terminal control changed before eviction qualification completed")
		}
		execution := view.Participant.Status.Execution
		if execution == nil || execution.PodUID != selected.Execution.PodUID ||
			execution.ProcessNonce != selected.Execution.ProcessNonce ||
			execution.ProfileDigest != selected.Execution.ProfileDigest {
			return fmt.Errorf("worker execution identity disappeared or restarted after eviction")
		}
		rootFailed := failed(snapshot{Status: view.Root.Status})
		if view.Pod == nil && view.Termination != "Terminated" {
			return fmt.Errorf(
				"TerminationUnknown: bound Pod disappeared without retained process termination; " +
					"normal cleanup must retain uncertainty",
			)
		}
		if !heldSince.IsZero() && !rootFailed {
			return fmt.Errorf("root Failed latch cleared after eviction")
		}
		if rootFailed && view.Termination == "Terminated" {
			if settled == nil {
				settled = execution.DeepCopy()
				heldSince = time.Now()
				if err := h.event("worker-eviction-failed", view); err != nil {
					return err
				}
			}
			if !reflect.DeepEqual(settled, execution) {
				return fmt.Errorf("worker execution changed after confirmed eviction termination")
			}
			if time.Since(heldSince) >= h.config.pauseWindow {
				return h.event("worker-eviction-failure-held", view)
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"eviction failure/termination observation ended: %w; latest public evidence retained",
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}
