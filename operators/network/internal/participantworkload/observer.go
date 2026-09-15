package participantworkload

import (
	"fmt"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoinobserver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

// observerEnabled selects only the admitted opt-in Bitcoin policy.
func observerEnabled(p *api.StacksNetworkParticipant) bool {
	if p.Spec.Kind != api.ParticipantBitcoinNode || p.Status.Admission == nil {
		return false
	}
	node := p.Status.Admission.Configuration.BitcoinNode
	return node != nil && node.Observer != nil && ptr.Deref(node.Observer.Enabled, false)
}

// attachObserver provisions a tokenless sidecar with only its dedicated read-only credential.
func attachObserver(
	p *api.StacksNetworkParticipant,
	state *api.ParticipantRuntimeStatus,
	workload *appsv1.StatefulSet,
	image string,
) error {
	if !observerEnabled(p) {
		return nil
	}
	if image == "" || state.ObserverRPCSecretRef == nil || state.ObserverRPCSecretRef.UID == "" {
		return fmt.Errorf("observer image or credential identity missing")
	}
	interval := time.Duration(ptr.Deref(p.Status.Admission.Configuration.BitcoinNode.Observer.IntervalSeconds,
		int32(bitcoinobserver.DefaultInterval/time.Second))) * time.Second
	if interval < bitcoinobserver.MinInterval || interval > bitcoinobserver.MaxInterval {
		return fmt.Errorf("observer interval must be between 5s and 300s")
	}
	pod := &workload.Spec.Template.Spec
	pod.Volumes = append(
		pod.Volumes,
		corev1.Volume{
			Name: bitcoinobserver.ContainerName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  state.ObserverRPCSecretRef.Name,
					DefaultMode: ptr.To[int32](0o440),
				},
			},
		},
	)
	pod.Containers = append(pod.Containers, corev1.Container{
		Name:            bitcoinobserver.ContainerName,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bitcoin-observer"},
		Args:            []string{"--interval=" + interval.String()},
		Ports: []corev1.ContainerPort{
			{Name: common.EndpointMetrics, ContainerPort: bitcoinobserver.MetricsPort},
		},
		VolumeMounts: []corev1.VolumeMount{{Name: bitcoinobserver.ContainerName, MountPath: "/rpc", ReadOnly: true}},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	})
	return nil
}
