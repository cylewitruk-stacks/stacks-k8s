//go:build live

package publicintegration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var chaosGVK = schema.GroupVersionKind{Group: "chaos-mesh.org", Version: "v1alpha1", Kind: "NetworkChaos"}

// chaosActor fixes the actual network namespace endpoint independently of native fault status.
type chaosActor struct {
	Participant            identity `json:"participant"`
	LogicalName            string   `json:"logicalName"`
	Pod                    identity `json:"pod"`
	IP                     string   `json:"ip"`
	Container, ContainerID string
	Port                   int32 `json:"port"`
}

// chaosProbe records HTTP round-trip behavior, never independent one-way packet direction.
type chaosProbe struct {
	Reachable bool            `json:"reachable"`
	Seconds   float64         `json:"seconds,omitempty"`
	Info      json.RawMessage `json:"info,omitempty"`
	Failure   string          `json:"failure,omitempty"`
}

// chaosSelectors scopes both native selectors to one immutable participant identity.
func chaosSelectors(namespace string, rootUID types.UID, actor chaosActor) map[string]any {
	return map[string]any{"namespaces": []any{namespace}, "labelSelectors": map[string]any{"network.stacks.org/network": "network", "network.stacks.org/actor": actor.LogicalName, "network.stacks.org/network-uid": string(rootUID), "network.stacks.org/participant-uid": string(actor.Participant.UID), "network.stacks.org/role": "actor"}}
}

// chaosRequest creates one bounded native fault without supplying controller lifecycle fields.
func chaosRequest(namespace string, rootUID types.UID, pair [2]chaosActor, action string) *unstructured.Unstructured {
	direction := "to"
	if action == "partition" {
		direction = "both"
	}
	object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": chaosGVK.GroupVersion().String(), "kind": chaosGVK.Kind, "spec": map[string]any{"action": action, "mode": "one", "direction": direction, "duration": "90s", "selector": chaosSelectors(namespace, rootUID, pair[0]), "target": map[string]any{"mode": "one", "selector": chaosSelectors(namespace, rootUID, pair[1])}}}}
	object.SetNamespace(namespace)
	object.SetName("qualify-" + action)
	object.SetLabels(map[string]string{"network.stacks.org/network": "network", "network.stacks.org/network-uid": string(rootUID), "actions.stacks.org/correlation-id": object.GetName()})
	if action == "delay" {
		_ = unstructured.SetNestedMap(object.Object, map[string]any{"latency": "500ms", "jitter": "0ms", "correlation": "0"}, "spec", "delay")
	}
	return object
}

// chaosCondition requires an actual native condition rather than elapsed duration alone.
func chaosCondition(object *unstructured.Unstructured, name string) bool {
	conditions, _, err := unstructured.NestedSlice(object.Object, "status", "conditions")
	if err != nil {
		return false
	}
	for _, raw := range conditions {
		c, ok := raw.(map[string]any)
		if ok && c["type"] == name && c["status"] == "True" {
			return true
		}
	}
	return false
}

// selectChaosActors chooses non-miners so the isolated pair excludes miner and worker control paths.
func (h *harness) selectChaosActors(ctx context.Context, s snapshot) ([2]chaosActor, error) {
	var pair [2]chaosActor
	n := 0
	for _, p := range s.Participants {
		r := p.Status.Runtime
		if p.Kind != "StacksNode" || p.Status.Admission == nil || p.Status.Admission.Configuration.StacksNode == nil || r == nil || r.PodRef == nil {
			continue
		}
		mining := p.Status.Admission.Configuration.StacksNode.Mining
		if mining != nil && ptr.Deref(mining.Enabled, false) {
			continue
		}
		actor := chaosActor{Participant: p.Identity, LogicalName: p.Name, Pod: identity{Name: r.PodRef.Name, UID: r.PodRef.UID}, IP: r.PodIP, Container: "stacks-node", ContainerID: r.ContainerID}
		for _, endpoint := range r.Endpoints {
			if endpoint.Name == "rpc" {
				actor.Port = endpoint.Port
			}
		}
		if actor.Port == 0 || net.ParseIP(actor.IP) == nil {
			continue
		}
		if err := h.validateChaosActor(ctx, actor); err != nil {
			return pair, err
		}
		pair[n] = actor
		n++
		if n == 2 {
			return pair, nil
		}
	}
	return pair, fmt.Errorf("Chaos qualification requires two current non-mining StacksNode actors")
}

// validateChaosActor verifies immutable leaf/Pod/process identities before and after each remote read.
func (h *harness) validateChaosActor(ctx context.Context, a chaosActor) error {
	var p api.StacksNetworkParticipant
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: a.Participant.Name}, &p); err != nil {
		return err
	}
	r := p.Status.Runtime
	if p.UID != a.Participant.UID || p.Spec.NetworkUID != h.rootUID || p.Spec.ParticipantName != a.LogicalName || p.DeletionTimestamp != nil || p.Status.Admission == nil || r == nil || r.PodRef == nil || r.PodRef.UID != a.Pod.UID || r.PodRef.Name != a.Pod.Name || r.ContainerID != a.ContainerID || r.PolicyDigest != p.Status.Admission.PolicyDigest {
		return fmt.Errorf("selected Chaos participant identity changed")
	}
	var pod corev1.Pod
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: a.Pod.Name}, &pod); err != nil {
		return err
	}
	if pod.UID != a.Pod.UID || pod.Status.PodIP != a.IP || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return fmt.Errorf("selected Chaos actor Pod changed")
	}
	expected := chaosSelectors(h.config.namespace, h.rootUID, a)["labelSelectors"].(map[string]any)
	for key, value := range expected {
		if pod.Labels[key] != value {
			return fmt.Errorf("selected actor label %s differs", key)
		}
	}
	for _, c := range pod.Status.ContainerStatuses {
		if c.Name == a.Container && c.ContainerID == a.ContainerID && c.State.Running != nil {
			return nil
		}
	}
	return fmt.Errorf("selected actor process changed")
}

// parseChaosProbe distinguishes native HTTP success from curl transport failures and exec failures.
func parseChaosProbe(stdout, stderr []byte, err error) (chaosProbe, error) {
	if err != nil {
		for _, code := range []string{"curl: (7)", "curl: (28)"} {
			if bytes.Contains(stderr, []byte(code)) {
				return chaosProbe{Failure: code}, nil
			}
		}
		return chaosProbe{}, fmt.Errorf("actor curl probe could not execute: %w; selected diagnostic actor image must provide curl", err)
	}
	parts := bytes.Split(bytes.TrimSpace(stdout), []byte("\n"))
	if len(parts) < 2 {
		return chaosProbe{}, fmt.Errorf("probe lacks native body or curl timing")
	}
	seconds, e := strconv.ParseFloat(string(parts[len(parts)-1]), 64)
	if e != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 5 {
		return chaosProbe{}, fmt.Errorf("invalid curl timing")
	}
	body := bytes.Join(parts[:len(parts)-1], []byte("\n"))
	var info struct {
		Tip    string  `json:"stacks_tip"`
		Height *uint64 `json:"stacks_tip_height"`
	}
	if json.Unmarshal(body, &info) != nil || info.Tip == "" || info.Height == nil {
		return chaosProbe{}, fmt.Errorf("probe did not return native Stacks /v2/info")
	}
	return chaosProbe{Reachable: true, Seconds: seconds, Info: append(json.RawMessage(nil), body...)}, nil
}

// probeChaosPeer executes only an unauthenticated GET; argv contains no credential material or shell.
func (h *harness) probeChaosPeer(ctx context.Context, source, target chaosActor) (chaosProbe, error) {
	for _, actor := range []chaosActor{source, target} {
		if err := h.validateChaosActor(ctx, actor); err != nil {
			return chaosProbe{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	address := "http://" + net.JoinHostPort(target.IP, strconv.Itoa(int(target.Port))) + "/v2/info"
	args := []string{"--kubeconfig", h.config.kubeconfig, "--context", h.config.kubecontext, "-n", h.config.namespace, "exec", source.Pod.Name, "-c", source.Container, "--", "curl", "--silent", "--show-error", "--fail", "--noproxy", "*", "--connect-timeout", "2", "--max-time", "4", "--write-out", "\n%{time_total}", address}
	command := exec.CommandContext(ctx, "kubectl", args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	for _, actor := range []chaosActor{source, target} {
		if err := h.validateChaosActor(ctx, actor); err != nil {
			return chaosProbe{}, err
		}
	}
	probe, err := parseChaosProbe(stdout.Bytes(), stderr.Bytes(), runErr)
	if err == nil {
		err = h.event("chaos-http-roundtrip", map[string]any{"source": source, "target": target, "probe": probe})
	}
	return probe, err
}

// chaosMedian samples curl's own round-trip timer, excluding kubectl/API startup latency.
func (h *harness) chaosMedian(ctx context.Context, source, target chaosActor) (float64, error) {
	samples := make([]float64, 0, 3)
	for range 3 {
		probe, err := h.probeChaosPeer(ctx, source, target)
		if err != nil {
			return 0, err
		}
		if !probe.Reachable {
			return 0, fmt.Errorf("actor HTTP round-trip unreachable: %s", probe.Failure)
		}
		samples = append(samples, probe.Seconds)
	}
	sort.Float64s(samples)
	return samples[1], nil
}

// requireChaosProfile checks external enrollment and current native policy compilation without installing it.
func (h *harness) requireChaosProfile(ctx context.Context, pair [2]chaosActor) error {
	var ns corev1.Namespace
	if err := h.c.Get(ctx, client.ObjectKey{Name: h.config.namespace}, &ns); err != nil {
		return err
	}
	if ns.UID != h.namespaceUID || ns.Labels["network.stacks.org/chaos-profile"] != "network-faults-v1" || ns.Annotations["chaos-mesh.org/inject"] != "enabled" {
		return fmt.Errorf("install compatible Chaos Mesh 2.8.4 and the delay+partition profile, then enroll the exact fixture namespace externally")
	}
	var policy admissionv1.ValidatingAdmissionPolicy
	if err := h.c.Get(ctx, client.ObjectKey{Name: "stacks-network-faults-" + h.config.namespace}, &policy); err != nil {
		return err
	}
	if policy.Status.ObservedGeneration != policy.Generation || policy.Status.TypeChecking == nil || len(policy.Status.TypeChecking.ExpressionWarnings) != 0 {
		return fmt.Errorf("native Chaos admission policy is not currently compiled")
	}
	faults := &unstructured.UnstructuredList{}
	faults.SetGroupVersionKind(chaosGVK.GroupVersion().WithKind("NetworkChaosList"))
	if err := h.c.List(ctx, faults, client.InNamespace(h.config.namespace)); err != nil {
		return fmt.Errorf("optional native Chaos API unavailable: %w", err)
	}
	if len(faults.Items) != 0 {
		return fmt.Errorf("Chaos qualification requires no existing namespace faults")
	}
	for _, kind := range []string{"delay", "partition"} {
		valid := chaosRequest(h.config.namespace, h.rootUID, pair, kind)
		if err := h.c.Create(ctx, valid, client.DryRunAll); err != nil {
			return fmt.Errorf("native %s profile unavailable: %w", kind, err)
		}
		invalid := chaosRequest(h.config.namespace, h.rootUID, pair, kind)
		unstructured.RemoveNestedField(invalid.Object, "spec", "target", "selector", "labelSelectors", "network.stacks.org/participant-uid")
		err := h.c.Create(ctx, invalid, client.DryRunAll)
		if err == nil {
			return fmt.Errorf("native Chaos admission did not reject a missing target participant UID")
		}
		if !apierrors.IsInvalid(err) && !apierrors.IsForbidden(err) {
			return err
		}
	}
	return nil
}

// errChaosObservationPending distinguishes incomplete canonical reads from identity failures.
var errChaosObservationPending = errors.New("canonical control observation pending")

// chaosControl checks successful reads on the worker-to-API/RPC paths independently of global Operational.
func chaosControl(s snapshot, before snapshot) (int64, error) {
	if s.Status.ObservationPolicy == nil || s.Status.GenesisRef == nil || !reflect.DeepEqual(workerIdentities(s), workerIdentities(before)) {
		return 0, fmt.Errorf("worker identities or observation policy unavailable during fault")
	}
	fresh := func(at metav1.Time) bool {
		return !at.IsZero() && !at.After(s.At) && s.At.Sub(at.Time) <= time.Duration(3*s.Status.ObservationPolicy.PollIntervalSeconds+s.Status.ObservationPolicy.RPCAllowanceSeconds)*time.Second
	}
	receipts := int64(0)
	var pending error
	nodes, workers := 0, 0
	for _, p := range s.Participants {
		r := p.Status.Runtime
		if p.Kind == "BitcoinNode" {
			found := false
			for _, e := range s.Executions {
				if e.ParticipantUID == p.Identity.UID {
					selected := actionSelection{Participant: p, Execution: e.Identity}
					if _, err := selected.observe(s); err != nil {
						return 0, err
					}
					receipts += e.Status.BlocksGenerated
					found = true
				}
			}
			if !found {
				return 0, fmt.Errorf("Bitcoin control observation absent")
			}
		}
		if p.Kind == "StacksNode" {
			if r == nil || r.PodRef == nil || r.Protocol == nil || r.Protocol.PodUID != r.PodRef.UID || r.Protocol.ContainerID != r.ContainerID || r.Protocol.ConfigurationDigest != r.ConfigurationDigest || r.Protocol.GenesisUID != s.Status.GenesisRef.UID {
				return 0, fmt.Errorf("Stacks actor observation identity unavailable during protocol-pair fault: %s", p.Name)
			}
			if !r.Protocol.Available || !fresh(r.Protocol.ObservedAt) {
				pending = fmt.Errorf("%w: Stacks actor %s reason=%s observedAt=%s", errChaosObservationPending, p.Name, r.Protocol.Reason, r.Protocol.ObservedAt.UTC().Format(time.RFC3339))
			}
			nodes++
		}
		if e := p.Status.Execution; e != nil {
			bound := false
			for _, id := range s.Status.Identities {
				bound = bound || id.UID == p.Identity.UID && id.Worker != nil && id.Worker.Pod.UID == e.PodUID && id.Worker.ProfileDigest == e.ProfileDigest
			}
			if !bound || e.ProcessNonce == "" || !fresh(e.ObservedAt) {
				return 0, fmt.Errorf("management worker API heartbeat stale during protocol-pair fault")
			}
			if p.Kind == "StacksTransactionProduction" && (e.Traffic == nil || !e.Traffic.Available || !fresh(e.Traffic.ObservedAt)) {
				pending = fmt.Errorf("%w: transaction worker %s", errChaosObservationPending, p.Name)
			}
			workers++
		}
	}
	if nodes < 2 || workers == 0 {
		return 0, fmt.Errorf("control observation cohort incomplete")
	}
	return receipts, pending
}

// sampleChaosControl records observation gaps without accepting them as successful control reads.
func (h *harness) sampleChaosControl(stage string, s, before snapshot) (int64, bool, error) {
	receipts, err := chaosControl(s, before)
	if errors.Is(err, errChaosObservationPending) {
		if recordErr := h.record(stage+"-observation-gap", s); recordErr != nil {
			return 0, false, recordErr
		}
		return 0, false, h.event(stage+"-observation-unavailable", map[string]any{"reason": err.Error()})
	}
	return receipts, err == nil, err
}

// qualifyChaos runs bounded delay/partition mechanisms and requires fresh canonical recovery after each cleanup.
func (h *harness) qualifyChaos(ctx context.Context, before snapshot) (snapshot, error) {
	if h.config.cadence > 10*time.Second {
		return before, fmt.Errorf("bounded Chaos qualification requires a baseline cadence of at most 10s")
	}
	current, err := h.readSnapshot(ctx)
	if err != nil {
		return before, err
	}
	pair, err := h.selectChaosActors(ctx, current)
	if err != nil {
		return current, err
	}
	if err := h.requireChaosProfile(ctx, pair); err != nil {
		return current, err
	}
	baseline, err := h.chaosMedian(ctx, pair[0], pair[1])
	if err != nil {
		return current, err
	}
	if _, err := h.chaosMedian(ctx, pair[1], pair[0]); err != nil {
		return current, err
	}
	for _, kind := range []string{"delay", "partition"} {
		current, err = h.qualifyChaosFault(ctx, pair, kind, baseline, current)
		if err != nil {
			return current, err
		}
	}
	return current, nil
}

// readChaosFault refuses replacement and writes a bounded latest native mechanism snapshot.
func (h *harness) readChaosFault(ctx context.Context, expected *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(chaosGVK)
	if err := h.c.Get(ctx, client.ObjectKeyFromObject(expected), current); err != nil {
		return nil, err
	}
	if current.GetUID() != expected.GetUID() {
		return nil, fmt.Errorf("native fault identity changed")
	}
	data, err := json.MarshalIndent(current.Object, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(h.evidence, expected.GetName()+"-latest.json"), data, 0600); err != nil {
		return nil, err
	}
	return current, nil
}

// deleteChaosFault uses normal finalization and exact UID, including after an assertion failure.
func (h *harness) deleteChaosFault(ctx context.Context, fault *unstructured.Unstructured) error {
	uid := fault.GetUID()
	if err := h.c.Delete(ctx, fault, client.Preconditions{UID: &uid}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_, err := h.readChaosFault(ctx, fault)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("native fault cleanup pending: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// qualifyChaosFault separates native injection/finalization from packet observations and protocol recovery.
func (h *harness) qualifyChaosFault(ctx context.Context, pair [2]chaosActor, kind string, baseline float64, before snapshot) (result snapshot, resultErr error) {
	result = before
	fault := chaosRequest(h.config.namespace, h.rootUID, pair, kind)
	if err := h.c.Create(ctx, fault); err != nil {
		return result, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := h.deleteChaosFault(cleanup, fault); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := h.event(fault.GetName()+"-created", fault.Object); err != nil {
		return result, err
	}
	injected, err := h.wait(ctx, fault.GetName()+"-injected", 30*time.Second, true, func(s snapshot) (bool, error) {
		if _, ready, err := h.sampleChaosControl(fault.GetName(), s, before); err != nil || !ready {
			return false, err
		}
		o, err := h.readChaosFault(ctx, fault)
		return err == nil && chaosCondition(o, "AllInjected") && !chaosCondition(o, "AllRecovered"), err
	})
	if err != nil {
		return injected, err
	}
	initial, err := chaosControl(injected, before)
	if err != nil {
		return injected, err
	}
	if kind == "delay" {
		delayed, err := h.chaosMedian(ctx, pair[0], pair[1])
		if err != nil {
			return injected, err
		}
		if delayed < baseline+0.35 {
			return injected, fmt.Errorf("native delay did not increase median HTTP round-trip by at least350ms: before=%fs after=%fs", baseline, delayed)
		}
	} else {
		for _, direction := range [][2]chaosActor{{pair[0], pair[1]}, {pair[1], pair[0]}} {
			probe, err := h.probeChaosPeer(ctx, direction[0], direction[1])
			if err != nil {
				return injected, err
			}
			if probe.Reachable {
				return injected, fmt.Errorf("actor HTTP round-trip remained reachable during partition")
			}
		}
	}
	started := time.Now()
	held, err := h.wait(ctx, fault.GetName()+"-control-held", 35*time.Second, true, func(s snapshot) (bool, error) {
		receipts, ready, err := h.sampleChaosControl(fault.GetName(), s, before)
		if err != nil {
			return false, err
		}
		o, err := h.readChaosFault(ctx, fault)
		if err != nil {
			return false, err
		}
		if !chaosCondition(o, "AllInjected") || chaosCondition(o, "AllRecovered") {
			return false, fmt.Errorf("native fault ceased before control observation completed")
		}
		return ready && receipts > initial && time.Since(started) >= 20*time.Second, nil
	})
	if err != nil {
		return held, err
	}
	if kind == "delay" {
		_, err = h.wait(ctx, "delay-native-expired", 100*time.Second, true, func(s snapshot) (bool, error) {
			if _, ready, err := h.sampleChaosControl(fault.GetName(), s, before); err != nil || !ready {
				return false, err
			}
			o, err := h.readChaosFault(ctx, fault)
			return err == nil && chaosCondition(o, "AllRecovered"), err
		})
		if err != nil {
			return held, err
		}
	}
	cleanup, cancel := context.WithTimeout(ctx, 45*time.Second)
	err = h.deleteChaosFault(cleanup, fault)
	cancel()
	if err != nil {
		return held, err
	}
	if err := h.event(fault.GetName()+"-native-cleaned", map[string]any{"uid": fault.GetUID(), "expiry": kind == "delay"}); err != nil {
		return held, err
	}
	recovered, err := h.chaosMedian(ctx, pair[0], pair[1])
	if err != nil {
		return held, err
	}
	if recovered > baseline+0.25 {
		return held, fmt.Errorf("actor HTTP round-trip delay persists after native cleanup")
	}
	if _, err := h.chaosMedian(ctx, pair[1], pair[0]); err != nil {
		return held, err
	}
	// Capture the recovery baseline only after native cleanup; later progress must be newly observed.
	ready, err := h.wait(ctx, fault.GetName()+"-recovery-baseline", h.config.progressTimeout, true, func(s snapshot) (bool, error) { _, ready := progress(s); return ready, nil })
	if err != nil {
		return ready, err
	}
	return h.awaitProgress(ctx, fault.GetName()+"-canonical-recovered", ready)
}
