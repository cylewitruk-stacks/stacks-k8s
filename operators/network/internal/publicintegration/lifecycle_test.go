//go:build live

package publicintegration

import (
	"context"
	"fmt"
	"reflect"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// chainProgress preserves native observations separately from lifecycle conditions.
type chainProgress struct {
	bitcoin, stacks map[types.UID]uint64
	receipts        int64
	included        uint64
}

// progress requires fresh observations bound to each current actor process.
func progress(s snapshot) (chainProgress, bool) {
	result := chainProgress{bitcoin: map[types.UID]uint64{}, stacks: map[types.UID]uint64{}}
	if s.Status.ObservationPolicy == nil || s.Status.GenesisRef == nil || s.Status.Bitcoin == nil {
		return result, false
	}
	boundRecords := map[types.UID]bool{}
	for _, ref := range s.Status.Bitcoin.ExecutionRefs {
		boundRecords[ref.UID] = true
	}
	freshness := time.Duration(3*s.Status.ObservationPolicy.PollIntervalSeconds+s.Status.ObservationPolicy.RPCAllowanceSeconds) * time.Second
	fresh := func(at metav1.Time) bool { return !at.IsZero() && !at.After(s.At) && s.At.Sub(at.Time) <= freshness }
	for _, p := range s.Participants {
		state := p.Status.Runtime
		switch p.Kind {
		case "StacksNode":
			if state == nil || state.PodRef == nil || state.Protocol == nil {
				return result, false
			}
			view := state.Protocol
			if !view.Available || !view.FullySynced || view.GenesisUID != s.Status.GenesisRef.UID || view.PodUID != state.PodRef.UID || view.ContainerID != state.ContainerID || view.ConfigurationDigest != state.ConfigurationDigest || !fresh(view.ObservedAt) || view.IndexBlockID == "" {
				return result, false
			}
			result.stacks[p.Identity.UID] = view.StacksHeight
		case "BitcoinNode":
			if state == nil || state.PodRef == nil {
				return result, false
			}
			found := false
			for _, record := range s.Executions {
				if record.ParticipantUID != p.Identity.UID || !boundRecords[record.Identity.UID] {
					continue
				}
				view := record.Status.Observation
				if view == nil || view.Target.Pod.UID != state.PodRef.UID || view.Target.ContainerID != state.ContainerID || !fresh(view.ObservedAt) {
					return result, false
				}
				result.bitcoin[p.Identity.UID] = uint64(view.Height)
				found = true
			}
			if !found {
				return result, false
			}
		case "StacksTransactionProduction":
			if p.Status.Execution == nil || p.Status.Execution.Transactions == nil {
				return result, false
			}
			tx := p.Status.Execution.Transactions
			if tx.LastInclusion == nil || !tx.LastInclusion.Success {
				return result, false
			}
			result.included += tx.Included
		}
	}
	for _, record := range s.Executions {
		if boundRecords[record.Identity.UID] {
			result.receipts += record.Status.BlocksGenerated
		}
	}
	return result, len(result.bitcoin) > 0 && len(result.stacks) > 0 && result.included > 0
}

// advanced requires every observed native chain view and successful traffic inclusion to advance.
func advanced(before, after chainProgress) bool {
	if after.receipts <= before.receipts || after.included <= before.included || len(before.bitcoin) != len(after.bitcoin) || len(before.stacks) != len(after.stacks) {
		return false
	}
	for uid, height := range before.bitcoin {
		if after.bitcoin[uid] <= height {
			return false
		}
	}
	for uid, height := range before.stacks {
		if after.stacks[uid] <= height {
			return false
		}
	}
	return true
}

// awaitProgress observes sustained behavior after initialization or resume.
func (h *harness) awaitProgress(ctx context.Context, stage string, before snapshot) (snapshot, error) {
	baseline, ready := progress(before)
	if !ready {
		var err error
		before, err = h.wait(ctx, stage+"-baseline", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
			_, coherent := progress(s)
			return coherent && condition(s, "Initialized", metav1.ConditionTrue) && condition(s, "Operational", metav1.ConditionTrue), nil
		})
		if err != nil {
			return before, err
		}
		baseline, _ = progress(before)
	}
	return h.wait(ctx, stage, h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		current, ready := progress(s)
		return condition(s, "Initialized", metav1.ConditionTrue) && condition(s, "Operational", metav1.ConditionTrue) && ready && advanced(baseline, current), nil
	})
}

// workerIdentity pins the process identity that an operator rollout must preserve.
type workerIdentity struct {
	UID          types.UID `json:"uid"`
	ProcessNonce string    `json:"processNonce,omitempty"`
}

func workerIdentities(s snapshot) map[string]workerIdentity {
	result := map[string]workerIdentity{}
	for _, id := range s.Status.Identities {
		if id.Worker == nil {
			continue
		}
		nonce := ""
		for _, p := range s.Participants {
			if p.Identity.UID == id.UID && p.Status.Execution != nil {
				nonce = p.Status.Execution.ProcessNonce
			}
		}
		result[id.Worker.Pod.Name] = workerIdentity{UID: id.Worker.Pod.UID, ProcessNonce: nonce}
	}
	for _, p := range s.Participants {
		if p.Status.BitcoinControl != nil {
			for _, pod := range p.Status.BitcoinControl.Pods {
				if !pod.Terminated {
					result[pod.Pod.Name] = workerIdentity{UID: pod.Pod.UID}
				}
			}
		}
	}
	return result
}

// verifyWorkers checks actual Pod existence in addition to retained API bindings.
func (h *harness) verifyWorkers(ctx context.Context, expected map[string]workerIdentity, s snapshot) error {
	if len(expected) == 0 || !reflect.DeepEqual(expected, workerIdentities(s)) {
		return fmt.Errorf("bound worker UID or process nonce changed")
	}
	for name, identity := range expected {
		var pod corev1.Pod
		if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: name}, &pod); err != nil {
			return err
		}
		if pod.UID != identity.UID || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
			return fmt.Errorf("bound worker Pod %s is no longer the same running instance", name)
		}
	}
	return nil
}

// restartOperator rolls only the explicitly named and UID-pinned operator Deployment.
func (h *harness) restartOperator(ctx context.Context, before snapshot) (snapshot, error) {
	key := client.ObjectKey{Namespace: h.config.operatorNamespace, Name: h.config.operatorName}
	var deployment appsv1.Deployment
	if err := h.c.Get(ctx, key, &deployment); err != nil {
		return before, err
	}
	if deployment.UID != h.config.operatorUID || deployment.Namespace != key.Namespace || deployment.DeletionTimestamp != nil || ptr.Deref(deployment.Spec.Replicas, 1) < 1 {
		return before, fmt.Errorf("selected operator Deployment identity or desired replicas differ")
	}
	podSpec := deployment.Spec.Template.Spec.DeepCopy()
	original := deployment.DeepCopy()
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["network.stacks.org/publicintegration-restart"] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.c.Patch(ctx, &deployment, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return before, err
	}
	generation := deployment.Generation
	expected := workerIdentities(before)
	return h.wait(ctx, "operator-restarted", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		if err := h.verifyWorkers(ctx, expected, s); err != nil {
			return false, err
		}
		var current appsv1.Deployment
		if err := h.c.Get(ctx, key, &current); err != nil {
			return false, err
		}
		if current.UID != deployment.UID || !reflect.DeepEqual(&current.Spec.Template.Spec, podSpec) {
			return false, fmt.Errorf("operator rollout changed Deployment identity or container configuration")
		}
		replicas := ptr.Deref(current.Spec.Replicas, 1)
		_, protocolReady := progress(s)
		ready := current.Status.ObservedGeneration >= generation && current.Status.UpdatedReplicas == replicas && current.Status.AvailableReplicas == replicas && current.Status.Replicas == replicas && current.Status.UnavailableReplicas == 0 && condition(s, "Operational", metav1.ConditionTrue) && protocolReady
		if ready {
			images := map[string]string{}
			for _, container := range current.Spec.Template.Spec.Containers {
				images[container.Name] = container.Image
			}
			return true, h.event("operator-rollout-ready", map[string]any{"namespace": current.Namespace, "deployment": objectIdentity(&current), "images": images, "status": current.Status})
		}
		return false, nil
	})
}

// pauseResume checks cooperative pause without claiming that independent chain activity stopped.
func (h *harness) pauseResume(ctx context.Context, before snapshot) (snapshot, error) {
	expected := workerIdentities(before)
	if err := h.setOperation(ctx, "Paused"); err != nil {
		return before, err
	}
	paused, err := h.wait(ctx, "paused", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		return s.Status.Phase == "Paused" && condition(s, "Running", metav1.ConditionFalse), nil
	})
	if err != nil {
		return paused, err
	}
	if err := h.verifyWorkers(ctx, expected, paused); err != nil {
		return paused, err
	}
	receiptCount := int64(0)
	for _, record := range paused.Executions {
		receiptCount += record.Status.BlocksGenerated
	}
	started := time.Now()
	_, err = h.wait(ctx, "pause-held", h.config.pauseWindow+30*time.Second, true, func(s snapshot) (bool, error) {
		if s.Status.Phase != "Paused" || !condition(s, "Running", metav1.ConditionFalse) {
			return false, fmt.Errorf("pause acknowledgement was lost")
		}
		count := int64(0)
		for _, record := range s.Executions {
			count += record.Status.BlocksGenerated
		}
		if count != receiptCount {
			return false, fmt.Errorf("managed Bitcoin generation receipts advanced after pause acknowledgement")
		}
		if err := h.verifyWorkers(ctx, expected, s); err != nil {
			return false, err
		}
		return time.Since(started) >= h.config.pauseWindow, nil
	})
	if err != nil {
		return paused, err
	}
	if err := h.setOperation(ctx, "Running"); err != nil {
		return paused, err
	}
	resumed, err := h.wait(ctx, "resumed", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		_, ready := progress(s)
		return condition(s, "Initialized", metav1.ConditionTrue) && condition(s, "Operational", metav1.ConditionTrue) && ready, nil
	})
	if err != nil {
		return resumed, err
	}
	if err := h.verifyWorkers(ctx, expected, resumed); err != nil {
		return resumed, err
	}
	return h.awaitProgress(ctx, "resumed-progress", resumed)
}

// cleanup uses terminal Stop, normal root deletion and namespace deletion without editing finalizers.
func (h *harness) cleanup() error {
	if h.cleaned || !h.createdNamespace {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.config.cleanupTimeout)
	defer cancel()
	if err := h.deleteNetwork(ctx); err != nil {
		return err
	}
	var ns corev1.Namespace
	err := h.c.Get(ctx, client.ObjectKey{Name: h.config.namespace}, &ns)
	if apierrors.IsNotFound(err) {
		h.cleaned = true
		return nil
	}
	if err != nil {
		return err
	}
	if ns.UID != h.namespaceUID {
		return fmt.Errorf("cleanup refused replacement namespace")
	}
	if err := h.c.Delete(ctx, &ns, client.Preconditions{UID: &h.namespaceUID}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err := h.waitAbsent(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: h.config.namespace}}); err != nil {
		return err
	}
	h.cleaned = true
	return h.event("namespace-deleted", map[string]any{"namespace": h.config.namespace, "uid": h.namespaceUID})
}

// deleteNetwork disposes only the exact root, retaining reusable declarations and the namespace.
func (h *harness) deleteNetwork(ctx context.Context) error {
	return h.disposeNetwork(ctx, true)
}

// disposeNetwork uses foreground direct deletion and background cleanup after confirmed Stop.
func (h *harness) disposeNetwork(ctx context.Context, stopFirst bool) error {
	if h.createdRoot {
		var root api.StacksNetwork
		err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: "network"}, &root)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if err == nil {
			if root.UID != h.rootUID {
				return fmt.Errorf("cleanup refused replacement root")
			}
			if stopFirst {
				if err := h.setOperation(ctx, "Stopped"); err != nil {
					return err
				}
				if _, err := h.wait(ctx, "stopped", h.config.cleanupTimeout, false, func(s snapshot) (bool, error) {
					return s.Status.Phase == "Stopped" && condition(s, "Running", metav1.ConditionFalse), nil
				}); err != nil {
					return err
				}
			} else if root.Spec.Operation != "Running" {
				return fmt.Errorf("direct deletion requires a running root")
			}
			if err := h.c.Get(ctx, client.ObjectKeyFromObject(&root), &root); err != nil {
				return err
			}
			if root.UID != h.rootUID {
				return fmt.Errorf("cleanup refused replacement root")
			}
			propagation := metav1.DeletePropagationForeground
			if stopFirst {
				propagation = metav1.DeletePropagationBackground
			}
			if err := h.event("root-deletion-requested", map[string]any{"uid": h.rootUID, "propagation": propagation, "stopFirst": stopFirst}); err != nil {
				return err
			}
			if err := h.c.Delete(ctx, &root, client.Preconditions{UID: &h.rootUID}, client.PropagationPolicy(propagation)); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			if err := h.waitAbsent(ctx, &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Namespace: h.config.namespace, Name: "network"}}); err != nil {
				return err
			}
		}
		retained := []identity{}
		for _, original := range h.declared.reusable {
			if original.GetUID() == "" {
				continue
			}
			current := original.DeepCopy()
			if err := h.c.Get(ctx, client.ObjectKeyFromObject(original), current); err != nil {
				return fmt.Errorf("reusable declaration %s/%s not retained: %w", original.GetKind(), original.GetName(), err)
			}
			if current.GetUID() != original.GetUID() || current.GetDeletionTimestamp() != nil {
				return fmt.Errorf("reusable declaration identity changed during root deletion")
			}
			retained = append(retained, objectIdentity(current))
		}
		if err := h.event("root-deleted-reusable-retained", retained); err != nil {
			return err
		}
	}
	h.createdRoot = false
	return nil
}

// waitAbsent waits for normal API deletion while preserving finalizer and ownership semantics.
func (h *harness) waitAbsent(ctx context.Context, object client.Object) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		err := h.c.Get(ctx, client.ObjectKeyFromObject(object), object)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("deletion pending for %T/%s: %w", object, object.GetName(), ctx.Err())
		case <-ticker.C:
		}
	}
}
