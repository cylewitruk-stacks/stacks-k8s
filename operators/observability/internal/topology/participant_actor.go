package topology

import (
	"context"
	"fmt"
	"sort"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// observeActor verifies the admission-to-process owner and configuration chain.
func observeActor(ctx context.Context, reads *directRead, p *api.StacksNetworkParticipant) (observation.ObservedActorIdentity, error) {
	state := p.Status.Runtime
	if state == nil || state.ObservedGeneration != p.Generation || state.Terminated || state.PodRef == nil || state.ContainerID == "" || state.ConfigRef == nil || len(state.WorkloadRefs) != 1 {
		return observation.ObservedActorIdentity{}, &NotReadyError{Reason: "actor runtime identity is incomplete or stopped"}
	}
	invalid := func(reason string) (observation.ObservedActorIdentity, error) {
		return observation.ObservedActorIdentity{}, &InconclusiveError{Reason: reason}
	}
	if state.PolicyDigest != p.Status.Admission.PolicyDigest || !validBinding(state.PodRef, "Pod") || !validBinding(state.ConfigRef, "Secret") || !validBinding(&state.WorkloadRefs[0], "StatefulSet") || immutableImageID(state.ConfigRef.Fingerprint) != state.ConfigRef.Fingerprint || state.ConfigRef.Fingerprint == "" {
		return invalid("actor runtime policy or configuration binding differs")
	}
	fields, role, err := admittedActor(p)
	if err != nil {
		return invalid(err.Error())
	}
	workload := &appsv1.StatefulSet{}
	if err := reads.get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: state.WorkloadRefs[0].Name}, workload); err != nil {
		return observation.ObservedActorIdentity{}, err
	}
	if workload.UID != state.WorkloadRefs[0].UID || !exactOwner(workload, api.GroupVersion.String(), "StacksNetworkParticipant", p.Name, p.UID) || workload.DeletionTimestamp != nil {
		return invalid("StatefulSet identity differs")
	}
	if workload.Spec.Replicas == nil || *workload.Spec.Replicas != 1 || workload.Status.ObservedGeneration != workload.Generation || workload.Status.ReadyReplicas != 1 || workload.Status.CurrentRevision == "" || workload.Status.CurrentRevision != workload.Status.UpdateRevision {
		return observation.ObservedActorIdentity{}, &NotReadyError{Reason: "actor StatefulSet is not converged"}
	}
	pod := &corev1.Pod{}
	if err := reads.get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: state.PodRef.Name}, pod); err != nil {
		return observation.ObservedActorIdentity{}, err
	}
	if pod.UID != state.PodRef.UID || !exactOwner(pod, "apps/v1", "StatefulSet", workload.Name, workload.UID) || pod.DeletionTimestamp != nil {
		return invalid("Pod identity differs")
	}
	if !podReady(pod) {
		return observation.ObservedActorIdentity{}, &NotReadyError{Reason: "actor Pod is not ready"}
	}
	expectedLabels := map[string]string{api.LabelNetworkUID: string(p.Spec.NetworkUID), api.LabelParticipant: p.Spec.ParticipantName, api.LabelParticipantUID: string(p.UID), api.LabelParticipantKind: string(p.Spec.Kind), api.LabelRole: "actor"}
	for _, labels := range []map[string]string{pod.Labels, workload.Labels, workload.Spec.Template.Labels} {
		for key, value := range expectedLabels {
			if labels[key] != value {
				return invalid("actor workload identity labels differ")
			}
		}
	}
	if workload.Spec.Selector == nil || len(workload.Spec.Selector.MatchExpressions) != 0 {
		return invalid("actor StatefulSet selector is not exact")
	}
	for key, value := range workload.Spec.Selector.MatchLabels {
		if pod.Labels[key] != value {
			return invalid("actor StatefulSet does not select the observed Pod")
		}
	}
	for key, value := range expectedLabels {
		if workload.Spec.Selector.MatchLabels[key] != value {
			return invalid("actor StatefulSet selector differs")
		}
	}
	if pod.Labels[appsv1.ControllerRevisionHashLabelKey] != workload.Status.CurrentRevision {
		return invalid("actor Pod revision differs")
	}
	for _, annotations := range []map[string]string{pod.Annotations, workload.Spec.Template.Annotations} {
		if annotations[policyAnnotation] != state.PolicyDigest {
			return invalid("actor Pod admitted policy annotation differs")
		}
		if p.Spec.Kind != api.ParticipantBitcoinNode && (state.ConfigurationDigest == "" || annotations[configurationAnnotation] != state.ConfigurationDigest) {
			return invalid("actor Pod public configuration annotation differs")
		}
	}
	containerName := actorContainerV2(p.Spec.Kind)
	for _, spec := range []*corev1.PodSpec{&pod.Spec, &workload.Spec.Template.Spec} {
		if err := verifyActorSpec(spec, containerName, *fields.Image, state.ConfigRef.Name); err != nil {
			return invalid(err.Error())
		}
	}
	var current *corev1.ContainerStatus
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == containerName {
			current = &pod.Status.ContainerStatuses[i]
		}
	}
	if current == nil || !current.Ready || current.State.Running == nil || current.ContainerID != state.ContainerID || immutableImageID(current.ImageID) == "" || current.ImageID != state.ImageID {
		return invalid("actor container process or immutable image differs")
	}
	services, err := observeServices(ctx, reads, p, pod, expectedLabels)
	if err != nil {
		return observation.ObservedActorIdentity{}, err
	}
	reportUID, err := observeConfigurationReport(ctx, reads, p)
	if err != nil {
		return observation.ObservedActorIdentity{}, err
	}
	return observation.ObservedActorIdentity{
		Kind: string(p.Spec.Kind), Name: p.Spec.ParticipantName, Role: role,
		ResourceName: p.Name, ResourceUID: p.UID,
		ServiceName: services[len(services)-1].Name, Services: services,
		StatefulSetName: workload.Name, StatefulSetUID: workload.UID, ControllerRevision: workload.Status.CurrentRevision,
		PodName: pod.Name, PodUID: pod.UID, ContainerID: current.ContainerID,
		RequestedImage: *fields.Image, RuntimeImageID: immutableImageID(current.ImageID),
		ConfigDigest: state.ConfigurationDigest, SpecDigest: state.PolicyDigest,
		ConfigurationName: state.ConfigRef.Name, ConfigurationUID: state.ConfigRef.UID,
		ConfigurationFingerprint: state.ConfigRef.Fingerprint, ConfigurationEvidence: "controller-reported",
		ConfigurationReportUID: reportUID, EvidenceClass: "orchestrator-observed",
	}, nil
}

// admittedActor selects the complete admitted actor image and public role.
func admittedActor(p *api.StacksNetworkParticipant) (*common.ActorFields, string, error) {
	c := p.Status.Admission.Configuration
	var fields *common.ActorFields
	role := ""
	switch p.Spec.Kind {
	case api.ParticipantBitcoinNode:
		if c.BitcoinNode != nil {
			fields = &c.BitcoinNode.ActorFields
		}
	case api.ParticipantStacksNode:
		if c.StacksNode != nil {
			fields = &c.StacksNode.ActorFields
			role = "follower"
			if c.StacksNode.Mining != nil && c.StacksNode.Mining.Enabled != nil && *c.StacksNode.Mining.Enabled {
				role = "miner"
			}
		}
	case api.ParticipantStacksSigner:
		if c.StacksSigner != nil {
			fields = &c.StacksSigner.ActorFields
			role = "signer"
		}
	}
	if fields == nil || fields.Image == nil || *fields.Image == "" {
		return nil, "", fmt.Errorf("admitted actor image is absent")
	}
	return fields, role, nil
}

// verifyActorSpec checks the requested image and private configuration mount name.
func verifyActorSpec(spec *corev1.PodSpec, containerName, image, configuration string) error {
	var actor *corev1.Container
	for i := range spec.Containers {
		if spec.Containers[i].Name == containerName {
			actor = &spec.Containers[i]
		}
	}
	if actor == nil || actor.Image != image {
		return fmt.Errorf("actor requested image differs")
	}
	mounted := false
	for _, mount := range actor.VolumeMounts {
		if mount.Name == "config" && mount.MountPath == "/config" && mount.ReadOnly {
			mounted = true
		}
	}
	found := false
	for _, volume := range spec.Volumes {
		if volume.Name == "config" && volume.Secret != nil && volume.Secret.SecretName == configuration {
			found = true
		}
	}
	if !mounted || !found {
		return fmt.Errorf("actor mounted configuration name differs")
	}
	return nil
}

// observeServices verifies every native endpoint against the selected actor Pod.
func observeServices(ctx context.Context, reads *directRead, p *api.StacksNetworkParticipant, pod *corev1.Pod, expectedLabels map[string]string) ([]observation.ObservedServiceIdentity, error) {
	services := []observation.ObservedServiceIdentity{}
	ports := map[string]int32{"p2p": 18444, "rpc": 18443}
	if p.Spec.Kind == api.ParticipantStacksNode {
		ports = map[string]int32{"p2p": 20444, "rpc": 20443}
	} else if p.Spec.Kind == api.ParticipantStacksSigner {
		ports = map[string]int32{"events": 30000}
	}
	if len(p.Status.Runtime.Endpoints) != len(ports) {
		return nil, &InconclusiveError{Reason: "actor endpoint set differs"}
	}
	seen := map[string]bool{}
	for _, endpoint := range p.Status.Runtime.Endpoints {
		suffix := "." + p.Namespace + ".svc"
		name := strings.TrimSuffix(endpoint.Host, suffix)
		if !strings.HasSuffix(endpoint.Host, suffix) || strings.Contains(name, ".") || name == "" || ports[endpoint.Name] != endpoint.Port || seen[endpoint.Name] {
			return nil, &InconclusiveError{Reason: "actor endpoint identity differs"}
		}
		seen[endpoint.Name] = true
		service := &corev1.Service{}
		if err := reads.get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: name}, service); err != nil {
			return nil, err
		}
		if service.UID == "" || service.DeletionTimestamp != nil || !exactOwner(service, api.GroupVersion.String(), "StacksNetworkParticipant", p.Name, p.UID) || len(service.Spec.Ports) != 1 {
			return nil, &InconclusiveError{Reason: "actor Service identity differs"}
		}
		for key, value := range expectedLabels {
			if service.Spec.Selector[key] != value {
				return nil, &InconclusiveError{Reason: "actor Service selector differs"}
			}
		}
		for key, value := range service.Spec.Selector {
			if pod.Labels[key] != value {
				return nil, &InconclusiveError{Reason: "actor Service does not select the observed Pod"}
			}
		}
		port := service.Spec.Ports[0]
		if port.Name != endpoint.Name || port.Port != endpoint.Port || (port.Protocol != "" && port.Protocol != corev1.ProtocolTCP) || (port.TargetPort != intstr.FromInt32(0) && port.TargetPort != intstr.FromInt32(endpoint.Port) && port.TargetPort != intstr.FromString(endpoint.Name)) {
			return nil, &InconclusiveError{Reason: "actor Service port differs"}
		}
		if port.TargetPort.Type == intstr.String {
			found := false
			for _, container := range pod.Spec.Containers {
				for _, native := range container.Ports {
					if native.Name == port.TargetPort.StrVal && native.ContainerPort == endpoint.Port {
						found = true
					}
				}
			}
			if !found {
				return nil, &InconclusiveError{Reason: "named Service port does not select the native port"}
			}
		}
		services = append(services, observation.ObservedServiceIdentity{Name: name, UID: service.UID, Protocol: endpoint.Name, Port: endpoint.Port})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Protocol < services[j].Protocol })
	return services, nil
}
