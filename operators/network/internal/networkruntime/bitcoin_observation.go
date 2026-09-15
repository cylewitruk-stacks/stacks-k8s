package networkruntime

import (
	"context"
	"net"
	"strconv"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// currentBitcoinObservation validates public process/credential metadata without reading Secret data.
func (r *Reconciler) currentBitcoinObservation(
	ctx context.Context,
	root *api.StacksNetwork,
	record *bitcoin.BitcoinExecution,
) (bool, error) {
	o := record.Status.Observation
	if o == nil {
		return false, nil
	}
	var p api.StacksNetworkParticipant
	if err := r.observations().
		Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: record.Spec.Participant.Name}, &p); err != nil {
		return false, err
	}
	if p.UID != record.Spec.Participant.UID || p.Spec.Kind != api.ParticipantBitcoinNode ||
		!selectedInstance(root, &p) ||
		!currentActorReady(&p) {
		return false, nil
	}
	state := p.Status.Runtime
	target := o.Target
	if !bitcoinTargetMatchesRuntime(&p, o) {
		return false, nil
	}
	for _, binding := range []struct {
		name string
		uid  string
	}{{state.ConfigRef.Name, string(state.ConfigRef.UID)}, {state.RPCSecretRef.Name, string(state.RPCSecretRef.UID)}} {
		metadata := &metav1.PartialObjectMetadata{
			TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: common.KindSecret},
		}
		if err := r.Reader.Get(
			ctx,
			client.ObjectKey{Namespace: root.Namespace, Name: binding.name},
			metadata,
		); err != nil {
			return false, err
		}
		if string(metadata.UID) != binding.uid || metadata.DeletionTimestamp != nil ||
			!metav1.IsControlledBy(metadata, &p) {
			return false, nil
		}
	}
	var pod corev1.Pod
	if err := r.observations().
		Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: target.Pod.Name}, &pod); err != nil {
		return false, err
	}
	if pod.UID != target.Pod.UID || pod.DeletionTimestamp != nil || pod.Status.PodIP == "" ||
		pod.Labels[api.LabelNetworkUID] != string(root.UID) ||
		pod.Labels[api.LabelParticipantUID] != string(p.UID) {
		return false, nil
	}
	owner := metav1.GetControllerOf(&pod)
	if owner == nil || owner.Kind != common.KindStatefulSet {
		return false, nil
	}
	var workload appsv1.StatefulSet
	if err := r.observations().
		Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: owner.Name}, &workload); err != nil {
		return false, err
	}
	bound := false
	for _, ref := range state.WorkloadRefs {
		if ref.Kind == common.KindStatefulSet && ref.Name == workload.Name && ref.UID == workload.UID {
			bound = true
		}
	}
	if !bound || workload.UID != owner.UID || workload.DeletionTimestamp != nil ||
		!metav1.IsControlledBy(&workload, &p) ||
		ptr.Deref(workload.Spec.Replicas, 1) != 1 {
		return false, nil
	}
	process := false
	for _, container := range pod.Status.ContainerStatuses {
		if container.Name == api.ContainerBitcoin && container.ContainerID == target.ContainerID &&
			container.State.Running != nil &&
			container.Ready {
			process = true
		}
	}
	if !process {
		return false, nil
	}
	return bitcoinEndpointMatches(state, &target, pod.Status.PodIP), nil
}

// bitcoinTargetMatchesRuntime binds an observation to the participant's published process identity.
func bitcoinTargetMatchesRuntime(p *api.StacksNetworkParticipant, o *bitcoin.BitcoinObservation) bool {
	state := p.Status.Runtime
	if state == nil || state.PodRef == nil || state.ConfigRef == nil || state.RPCSecretRef == nil ||
		p.Status.Admission == nil {
		return false
	}
	target := o.Target
	return target.Participant.UID == p.UID && target.Participant.Name == p.Name &&
		target.Pod.UID == state.PodRef.UID && target.Pod.Name == state.PodRef.Name &&
		target.ContainerID == state.ContainerID &&
		target.Configuration.UID == state.ConfigRef.UID && target.Configuration.Name == state.ConfigRef.Name &&
		target.Credentials.UID == state.RPCSecretRef.UID && target.Credentials.Name == state.RPCSecretRef.Name &&
		target.PolicyDigest == p.Status.Admission.PolicyDigest
}

// bitcoinEndpointMatches verifies the RPC address against one published actor address.
func bitcoinEndpointMatches(
	state *api.ParticipantRuntimeStatus,
	target *bitcoin.BitcoinTargetIdentity,
	podIP string,
) bool {
	for _, endpoint := range state.Endpoints {
		if endpoint.Name == common.EndpointRPC && endpoint.Port > 0 && endpoint.Port <= 65535 {
			return target.Endpoint == "http://"+net.JoinHostPort(
				podIP,
				strconv.Itoa(int(endpoint.Port)),
			)
		}
	}
	return false
}
