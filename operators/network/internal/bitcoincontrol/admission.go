package bitcoincontrol

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// admitted contains the exact current record and actor identity for one mutation.
type admitted struct {
	// root contains current operation and durable infrastructure bindings.
	root *api.StacksNetwork
	// participant contains admitted public actor inputs.
	participant *api.StacksNetworkParticipant
	// initialization preserves bootstrap policy independent of production replacement.
	initialization *bitcoin.BitcoinInitialization
	// pod retains the exact kubelet process facts used in finite action attribution.
	pod *corev1.Pod
	// target binds the admitted actor process and immutable transport inputs.
	target bitcoin.BitcoinTargetIdentity
}

// participantCurrent validates only the selected participant's public admitted identity.
func participantCurrent(root *api.StacksNetwork, p *api.StacksNetworkParticipant) bool {
	return !failed(root) && participantIdentity(root, p)
}

// participantIdentity checks retained actor selection independently of cleanup control state.
func participantIdentity(root *api.StacksNetwork, p *api.StacksNetworkParticipant) bool {
	if root.UID == "" || root.DeletionTimestamp != nil || p.DeletionTimestamp != nil || p.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(p, root) || p.Status.Admission == nil || foundation.Digest(p.Status.Admission.Configuration) != p.Status.Admission.PolicyDigest {
		return false
	}
	selected, pinned := false, false
	for _, entry := range root.Spec.Participants {
		selected = selected || entry.Name == p.Spec.ParticipantName && entry.Kind == p.Spec.Kind
	}
	for _, id := range root.Status.Identities {
		pinned = pinned || id.Name == p.Spec.ParticipantName && id.UID == p.UID && !id.Removing
	}
	return selected && pinned
}

// authorize requires current mutation permission in addition to exact observation identity.
func (w *Worker) authorize(ctx context.Context, record *bitcoin.BitcoinExecution) (admitted, error) {
	a, err := w.authorizeObservation(ctx, record)
	if err != nil {
		return admitted{}, err
	}
	if a.root.Spec.Operation == "Paused" {
		return admitted{}, fmt.Errorf("network is paused")
	}
	if err := foundation.ValidateAdmissionEligibility(ctx, w.Reader, a.participant); err != nil {
		return admitted{}, err
	}
	return a, nil
}

// authorizeObservation resolves exact actor identity for native reads, including during pause.
func (w *Worker) authorizeObservation(ctx context.Context, record *bitcoin.BitcoinExecution) (admitted, error) {
	return w.resolveActor(ctx, record, false)
}

// resolveActor permits terminal control only for finite compensation on an unchanged selected actor.
func (w *Worker) resolveActor(ctx context.Context, record *bitcoin.BitcoinExecution, cleanup bool) (admitted, error) {
	var out admitted
	root := &api.StacksNetwork{}
	if e := w.Reader.Get(ctx, client.ObjectKey{Namespace: w.Input.Namespace, Name: "network"}, root); e != nil {
		return out, e
	}
	if root.UID != record.Spec.NetworkUID || root.Status.Bitcoin == nil || !hasBinding(root.Status.Bitcoin.ExecutionRefs, record.Name, record.UID) || root.Status.Bitcoin.InitializationRef == nil || !metav1.IsControlledBy(record, root) || record.DeletionTimestamp != nil || (!cleanup && (root.Spec.Operation == "Stopped" || failed(root))) {
		return out, fmt.Errorf("network has not authorized this execution record")
	}
	p := &api.StacksNetworkParticipant{}
	if e := w.Reader.Get(ctx, client.ObjectKey{Namespace: record.Namespace, Name: record.Spec.Participant.Name}, p); e != nil {
		return out, e
	}
	if p.UID != record.Spec.Participant.UID || p.Spec.Kind != "BitcoinNode" || !participantIdentity(root, p) || p.Spec.Control != nil && ptr.Deref(p.Spec.Control.Suspended, false) {
		return out, fmt.Errorf("Bitcoin participant is not currently admitted")
	}
	if config := p.Status.Admission.Configuration.BitcoinNode; config == nil || config.Config != nil && ptr.Deref(config.Config.Compatibility, "Managed") == "Unverified" {
		return out, fmt.Errorf("managed Bitcoin configuration unavailable")
	}
	runtime := p.Status.Runtime
	if runtime == nil || runtime.PolicyDigest != p.Status.Admission.PolicyDigest || runtime.Terminated || runtime.PodRef == nil || runtime.RPCSecretRef == nil || runtime.ConfigRef == nil || runtime.ContainerID == "" {
		return out, fmt.Errorf("exact actor runtime identity unavailable")
	}
	if runtime.RPCSecretRef.UID != w.Input.CredentialsUID || runtime.RPCSecretRef.Name != w.Input.CredentialsName {
		return out, fmt.Errorf("mounted credentials differ from admitted identity")
	}
	for _, ref := range []common.Binding{*runtime.RPCSecretRef, *runtime.ConfigRef} {
		var secret corev1.Secret
		if e := w.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: ref.Name}, &secret); e != nil {
			return out, e
		}
		if secret.UID != ref.UID || secret.DeletionTimestamp != nil || !ptr.Deref(secret.Immutable, false) || !metav1.IsControlledBy(&secret, p) {
			return out, fmt.Errorf("immutable actor credential/configuration identity differs")
		}
	}
	var pod corev1.Pod
	if e := w.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: runtime.PodRef.Name}, &pod); e != nil {
		return out, e
	}
	if pod.UID != runtime.PodRef.UID || pod.DeletionTimestamp != nil || pod.Status.PodIP == "" || pod.Labels["network.stacks.org/participant-uid"] != string(p.UID) || pod.Labels["network.stacks.org/network-uid"] != string(root.UID) {
		return out, fmt.Errorf("actor Pod identity differs")
	}
	owner := metav1.GetControllerOf(&pod)
	if owner == nil || owner.Kind != "StatefulSet" {
		return out, fmt.Errorf("actor Pod owner unavailable")
	}
	var workload appsv1.StatefulSet
	if e := w.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: owner.Name}, &workload); e != nil {
		return out, e
	}
	if workload.UID != owner.UID || !metav1.IsControlledBy(&workload, p) || !hasBinding(runtime.WorkloadRefs, workload.Name, workload.UID) || workload.DeletionTimestamp != nil || ptr.Deref(workload.Spec.Replicas, 1) != 1 {
		return out, fmt.Errorf("actor workload identity differs")
	}
	process := false
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "bitcoin" && status.ContainerID == runtime.ContainerID && status.State.Running != nil && status.Ready {
			process = true
		}
	}
	if !process {
		return out, fmt.Errorf("actor process is not currently ready")
	}
	var endpoint api.RuntimeEndpoint
	for _, candidate := range runtime.Endpoints {
		if candidate.Name == "rpc" {
			endpoint = candidate
		}
	}
	parts := strings.Split(endpoint.Host, ".")
	if len(parts) < 3 || parts[1] != p.Namespace || parts[2] != "svc" || endpoint.Port < 1 || endpoint.Port > 65535 {
		return out, fmt.Errorf("scoped RPC endpoint unavailable")
	}
	var service corev1.Service
	if e := w.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: parts[0]}, &service); e != nil {
		return out, e
	}
	if !metav1.IsControlledBy(&service, p) || service.DeletionTimestamp != nil || service.Spec.Selector["network.stacks.org/participant-uid"] != string(p.UID) || service.Spec.Selector["network.stacks.org/network-uid"] != string(root.UID) {
		return out, fmt.Errorf("RPC Service binding differs")
	}
	for k, v := range service.Spec.Selector {
		if pod.Labels[k] != v {
			return out, fmt.Errorf("RPC Service does not select actor")
		}
	}
	portFound := false
	for _, port := range service.Spec.Ports {
		portFound = portFound || port.Name == "rpc" && port.Port == endpoint.Port
	}
	if !portFound {
		return out, fmt.Errorf("RPC Service port differs")
	}
	init := &bitcoin.BitcoinInitialization{}
	ref := root.Status.Bitcoin.InitializationRef
	if e := w.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: ref.Name}, init); e != nil {
		return out, e
	}
	if init.UID != ref.UID || init.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(init, root) || init.DeletionTimestamp != nil {
		return out, fmt.Errorf("initialization record identity unavailable")
	}
	out = admitted{root: root, participant: p, initialization: init, pod: &pod, target: bitcoin.BitcoinTargetIdentity{Participant: binding("StacksNetworkParticipant", p), Pod: *runtime.PodRef, ContainerID: runtime.ContainerID, Endpoint: "http://" + net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(int(endpoint.Port))), Configuration: *runtime.ConfigRef, Credentials: *runtime.RPCSecretRef, PolicyDigest: p.Status.Admission.PolicyDigest}}
	return out, nil
}

// authorizeOffer verifies current selection and the frozen ceiling independently of freshness snapshots.
func (w *Worker) authorizeOffer(ctx context.Context, a admitted, offer *bitcoin.BitcoinBlockOffer) error {
	if offer == nil || offer.Number < 1 || offer.ExpectedHeight >= offer.Ceiling || !w.Now().Before(offer.ExpiresAt.Time) || offer.Initialization != binding("BitcoinInitialization", a.initialization) || offer.Production != productionBinding(a.initialization) || !equality.Semantic.DeepEqual(a.initialization.Status.Offer, offer) {
		return fmt.Errorf("generation opportunity is not currently authorized")
	}
	if err := w.authorizeTimingOverride(ctx, a, offer); err != nil {
		return err
	}
	production := &api.StacksNetworkParticipant{}
	if e := w.Reader.Get(ctx, client.ObjectKey{Namespace: a.root.Namespace, Name: offer.Production.Name}, production); e != nil {
		return e
	}
	if production.UID != offer.Production.UID || !participantCurrent(a.root, production) || production.Spec.Kind != "BitcoinBlockProduction" || production.Status.Admission.PolicyDigest != offer.PolicyDigest || production.Spec.Control != nil && ptr.Deref(production.Spec.Control.Paused, false) {
		return fmt.Errorf("production participant is not currently authorized")
	}
	if err := foundation.ValidateAdmissionEligibility(ctx, w.Reader, production); err != nil {
		return err
	}
	if offer.Mode == "Baseline" {
		if err := foundation.ValidatePublicParticipantAdmission(ctx, w.Reader, a.root, a.participant); err != nil {
			return err
		}
		if err := baselineCompleted(ctx, w.Reader, a.root, a.initialization); err != nil {
			return err
		}
		inputs, err := resolveBaselineInputs(ctx, w.Reader, a.root, a.initialization, production, true)
		if err != nil {
			return err
		}
		return baselineOfferMatches(a.initialization, offer, inputs, a.participant)
	}
	if offer.Mode != "" && offer.Mode != "Bootstrap" || a.initialization.Spec.Target.UID != a.participant.UID {
		return fmt.Errorf("bootstrap target unavailable")
	}
	authority, e := currentGate(ctx, w.Reader, a.root, a.initialization)
	if e != nil || offer.Ceiling != authority.gate.BitcoinCeiling {
		return fmt.Errorf("current bootstrap ceiling unavailable")
	}
	ready, e := advancementReady(ctx, w.Reader, a.root, a.initialization, authority, offer.ExpectedHeight, w.Now())
	if e != nil || !ready {
		return fmt.Errorf("current bootstrap advancement unavailable")
	}
	return nil
}

// failed preserves the root failure latch independently of condition projection timing.
func failed(root *api.StacksNetwork) bool {
	if root.Status.Phase == "Failed" {
		return true
	}
	for _, condition := range root.Status.Conditions {
		if condition.Type == "Failed" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
