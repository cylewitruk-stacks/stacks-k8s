// Package participantworkload reconciles admitted v1alpha2 network actors.
package participantworkload

import (
	"fmt"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	// FieldManager owns only Bitcoin workload runtime and readiness status.
	FieldManager = "stacks-network-domain-bitcoinnode"
	// Finalizer preserves the actor until its termination is established.
	Finalizer = "network.stacks.org/bitcoin-workload"
	// PodFinalizer retains kubelet termination evidence before Pod disappearance.
	PodFinalizer = "network.stacks.org/actor-termination"
	// BitcoinImage is the release's default Core image; qualification records its imageID.
	BitcoinImage = "bitcoin/bitcoin:31.1"
	managedBy    = "stacks-network-operator"
)

// Name produces the public runtime naming contract for one participant purpose.
func Name(p *api.StacksNetworkParticipant, purpose string) string {
	return foundation.RuntimeName(string(p.Spec.NetworkUID), string(p.UID), string(p.Spec.Kind), p.Spec.ParticipantName, purpose)
}

// Labels identifies exact network/participant identity and isolates support from fault actors.
func Labels(p *api.StacksNetworkParticipant, role string) map[string]string {
	labels := map[string]string{api.LabelManagedBy: managedBy, api.LabelNetwork: "network", api.LabelNetworkUID: string(p.Spec.NetworkUID), api.LabelParticipant: p.Spec.ParticipantName, api.LabelParticipantUID: string(p.UID), api.LabelParticipantKind: string(p.Spec.Kind), api.LabelRole: role}
	if role == "actor" {
		labels[api.LabelActor] = p.Spec.ParticipantName
	}
	return labels
}

// objectMeta scopes generated resources to the exact participant owner.
func objectMeta(p *api.StacksNetworkParticipant, purpose, role string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: Name(p, purpose), Namespace: p.Namespace, Labels: Labels(p, role), OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID, Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(false)}}}
}

// binding records a public object identity without its contents.
func binding(kind string, obj metav1.Object) *common.Binding {
	return &common.Binding{Kind: kind, Name: obj.GetName(), UID: obj.GetUID()}
}

// digest uses the foundation canonical public-policy encoding.
func digest(value any) string {
	return foundation.Digest(value)
}

// owned requires the exact controller owner identity.
func owned(obj metav1.Object, p *api.StacksNetworkParticipant) bool {
	owner := metav1.GetControllerOf(obj)
	return owner != nil && owner.APIVersion == api.GroupVersion.String() && owner.Kind == "StacksNetworkParticipant" && owner.Name == p.Name && owner.UID == p.UID
}

// Services renders stable per-participant P2P and RPC addresses.
func Services(p *api.StacksNetworkParticipant) []*corev1.Service {
	result := make([]*corev1.Service, 0, 2)
	for _, endpoint := range runtimeEndpoints(p) {
		service := &corev1.Service{ObjectMeta: objectMeta(p, endpoint.Name, "actor"), Spec: corev1.ServiceSpec{Selector: Labels(p, "actor"), Ports: []corev1.ServicePort{{Name: endpoint.Name, Port: endpoint.Port}}}}
		if endpoint.Name == "p2p" && p.Spec.Kind == api.ParticipantBitcoinNode {
			service.Spec.ClusterIP = corev1.ClusterIPNone
			service.Spec.PublishNotReadyAddresses = true
		}
		result = append(result, service)
	}
	return result
}

// runtimeEndpoints contains the fixed native Services for one actor kind.
func runtimeEndpoints(p *api.StacksNetworkParticipant) []api.RuntimeEndpoint {
	ports := map[string]int32{"p2p": 18444, "rpc": 18443}
	if p.Spec.Kind == api.ParticipantStacksNode {
		ports = map[string]int32{"p2p": 20444, "rpc": 20443}
	} else if p.Spec.Kind == api.ParticipantStacksSigner {
		ports = map[string]int32{"events": 30000}
	}
	var endpoints []api.RuntimeEndpoint
	for _, name := range []string{"p2p", "rpc", "events"} {
		if port, ok := ports[name]; ok {
			endpoints = append(endpoints, api.RuntimeEndpoint{Name: name, Host: Name(p, name) + "." + p.Namespace + ".svc", Port: port})
		}
	}
	return endpoints
}

// actorFields selects admitted common workload settings without runtime-role coupling.
func actorFields(p *api.StacksNetworkParticipant) (*common.ActorFields, error) {
	if p.Status.Admission == nil {
		return nil, fmt.Errorf("actor admission is required")
	}
	c := p.Status.Admission.Configuration
	switch p.Spec.Kind {
	case api.ParticipantBitcoinNode:
		if c.BitcoinNode != nil {
			return &c.BitcoinNode.ActorFields, nil
		}
	case api.ParticipantStacksNode:
		if c.StacksNode != nil {
			return &c.StacksNode.ActorFields, nil
		}
	case api.ParticipantStacksSigner:
		if c.StacksSigner != nil {
			return &c.StacksSigner.ActorFields, nil
		}
	}
	return nil, fmt.Errorf("actor policy branch is not admitted")
}

// StatefulSet renders one admitted native process with shared explicit storage retention.
func StatefulSet(p *api.StacksNetworkParticipant, configName, actorCredentials string, replicas int32) (*appsv1.StatefulSet, error) {
	node, err := actorFields(p)
	if err != nil {
		return nil, err
	}
	labels := Labels(p, "actor")
	workload := &appsv1.StatefulSet{ObjectMeta: objectMeta(p, "actor", "actor"), Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(replicas), ServiceName: Name(p, "p2p"), Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels, Finalizers: []string{PodFinalizer}, Annotations: map[string]string{api.AnnotationPolicyDigest: p.Status.Admission.PolicyDigest}}, Spec: corev1.PodSpec{
		AutomountServiceAccountToken: ptr.To(false), TerminationGracePeriodSeconds: ptr.To[int64](60),
		SecurityContext: &corev1.PodSecurityContext{RunAsUser: ptr.To[int64](1000), RunAsGroup: ptr.To[int64](1000), RunAsNonRoot: ptr.To(true), FSGroup: ptr.To[int64](1000), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
		Containers: []corev1.Container{{Name: "bitcoin", Image: ptr.Deref(node.Image, BitcoinImage), ImagePullPolicy: ptr.Deref(node.ImagePullPolicy, corev1.PullIfNotPresent), Command: []string{"bitcoind"}, Args: []string{"-conf=/config/bitcoin.conf", "-datadir=/data"},
			Ports:           []corev1.ContainerPort{{Name: "rpc", ContainerPort: 18443}, {Name: "p2p", ContainerPort: 18444}},
			Env:             []corev1.EnvVar{secretEnv("RPC_USERNAME", actorCredentials, "username"), secretEnv("RPC_PASSWORD", actorCredentials, "password")},
			ReadinessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"sh", "-ec", "bitcoin-cli -regtest -rpcconnect=127.0.0.1 -rpcport=18443 -rpcuser=\"$RPC_USERNAME\" -rpcpassword=\"$RPC_PASSWORD\" getblockchaininfo >/dev/null"}}}, PeriodSeconds: 5, TimeoutSeconds: 3},
			SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
			VolumeMounts:    []corev1.VolumeMount{{Name: "config", MountPath: "/config", ReadOnly: true}, {Name: "data", MountPath: "/data"}, {Name: "tmp", MountPath: "/tmp"}},
		}}, Volumes: []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: configName, DefaultMode: ptr.To[int32](0440)}}}, {Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
	}}}}
	if p.Spec.Kind != api.ParticipantBitcoinNode {
		if ptr.Deref(node.Image, "") == "" {
			return nil, fmt.Errorf("Stacks actor image is required")
		}
		container := &workload.Spec.Template.Spec.Containers[0]
		container.Image = *node.Image
		container.Name = actorContainer(p.Spec.Kind)
		container.Command = []string{container.Name}
		container.Args = []string{"start", "--config", "/config/config.toml"}
		container.Env = nil
		container.Ports = []corev1.ContainerPort{{Name: "rpc", ContainerPort: 20443}, {Name: "p2p", ContainerPort: 20444}}
		container.ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/v2/info", Port: intstr.FromInt32(20443)}}, PeriodSeconds: 5, TimeoutSeconds: 3}
		if p.Spec.Kind == api.ParticipantStacksSigner {
			workload.Spec.ServiceName = Name(p, "events")
			container.Args = []string{"run", "--config", "/config/config.toml"}
			container.Ports = []corev1.ContainerPort{{Name: "events", ContainerPort: 30000}}
			container.ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(30000)}}, PeriodSeconds: 5, TimeoutSeconds: 3}
		}
	}
	if node.Resources != nil {
		workload.Spec.Template.Spec.Containers[0].Resources = *node.Resources.DeepCopy()
	}
	applyPlacement(&workload.Spec.Template.Spec, node.Placement, labels)
	storage := node.Storage
	if storage != nil && ptr.Deref(storage.Ephemeral, false) {
		workload.Spec.Template.Spec.Volumes = append(workload.Spec.Template.Spec.Volumes, corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
		return workload, nil
	}
	size, class, retain := "2Gi", (*string)(nil), false
	if storage != nil {
		size = ptr.Deref(storage.Size, size)
		class = storage.Class
		retain = ptr.Deref(storage.RetainOnDelete, false)
	}
	quantity, err := resource.ParseQuantity(size)
	if err != nil || quantity.Sign() <= 0 {
		return nil, fmt.Errorf("invalid positive storage size")
	}
	policy := appsv1.DeletePersistentVolumeClaimRetentionPolicyType
	if retain {
		policy = appsv1.RetainPersistentVolumeClaimRetentionPolicyType
	}
	workload.Spec.PersistentVolumeClaimRetentionPolicy = &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{WhenScaled: appsv1.RetainPersistentVolumeClaimRetentionPolicyType, WhenDeleted: policy}
	workload.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "data", Labels: labels}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: class, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: quantity}}}}}
	return workload, nil
}

// actorContainer selects the native process name recorded in runtime identity.
func actorContainer(kind api.ParticipantKind) string {
	switch kind {
	case api.ParticipantStacksNode:
		return "stacks-node"
	case api.ParticipantStacksSigner:
		return "stacks-signer"
	default:
		return "bitcoin"
	}
}

// actorWorkload mounts the exact stable event token only for the node and paired signer.
func actorWorkload(p *api.StacksNetworkParticipant, state *api.ParticipantRuntimeStatus) (*appsv1.StatefulSet, error) {
	credentials := ""
	if state.ActorRPCSecretRef != nil {
		credentials = state.ActorRPCSecretRef.Name
	}
	workload, err := StatefulSet(p, state.ConfigRef.Name, credentials, 1)
	if err != nil {
		return nil, err
	}
	if p.Spec.Kind != api.ParticipantBitcoinNode {
		if state.EventAuthSecretRef == nil || state.EventAuthSecretRef.UID == "" {
			return nil, fmt.Errorf("event authentication identity missing")
		}
		pod := &workload.Spec.Template.Spec
		pod.Volumes = append(pod.Volumes, corev1.Volume{Name: "event-auth", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: state.EventAuthSecretRef.Name, DefaultMode: ptr.To[int32](0440)}}})
		pod.Containers[0].VolumeMounts = append(pod.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "event-auth", MountPath: "/event-auth", ReadOnly: true})
		workload.Spec.Template.Annotations[api.AnnotationConfigurationDigest] = state.ConfigurationDigest
		if node := p.Status.Admission.Configuration.StacksNode; node != nil && node.Mining != nil && ptr.Deref(node.Mining.Enabled, false) {
			workload.Spec.Template.Annotations[miningEnabledAnnotation] = "true"
		}
		check := *pod.Containers[0].DeepCopy()
		check.Name = "config-check"
		check.Command = []string{"sh", "-ec", `exec "$1" check-config --config /config/config.toml >/dev/null 2>&1`, "--", pod.Containers[0].Name}
		check.Args = nil
		check.Ports = nil
		check.ReadinessProbe = nil
		pod.InitContainers = []corev1.Container{check}
	}
	return workload, nil
}

// secretEnv references a role-specific credential without reading its value.
func secretEnv(name, secret, key string) corev1.EnvVar {
	return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: key}}}
}

// applyPlacement keeps actor and support scheduling independent.
func applyPlacement(pod *corev1.PodSpec, placement *common.Placement, labels map[string]string) {
	if placement == nil {
		return
	}
	pod.NodeSelector = placement.NodeSelector
	if placement.Tolerations != nil {
		pod.Tolerations = *placement.Tolerations
	}
	if ptr.Deref(placement.SpreadAcrossNodes, false) {
		pod.Affinity = &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{Weight: 100, PodAffinityTerm: corev1.PodAffinityTerm{TopologyKey: corev1.LabelHostname, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{api.LabelNetworkUID: labels[api.LabelNetworkUID], api.LabelParticipantKind: labels[api.LabelParticipantKind], api.LabelRole: labels[api.LabelRole]}}}}}}}
	}
}
