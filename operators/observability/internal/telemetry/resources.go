package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Resources renders recording-owned workloads and least-privilege namespace identities.
func Resources(t *observation.NetworkTelemetry, workerImage, collectorImage string) ([]client.Object, error) {
	if workerImage == "" || (NeedsCollector(t) && collectorImage == "") {
		return nil, fmt.Errorf("recording images must be configured")
	}
	config, err := CollectorConfig(t)
	if err != nil {
		return nil, err
	}
	name := Name(t)
	labels := map[string]string{observation.LabelTelemetryUID: string(t.UID)}
	metadata := func(suffix string) metav1.ObjectMeta {
		return metav1.ObjectMeta{
			Name:      name + suffix,
			Namespace: t.Namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         observation.GroupVersion.String(),
					Kind:               observation.KindNetworkTelemetry,
					Name:               t.Name,
					UID:                t.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		}
	}
	configMap := &corev1.ConfigMap{ObjectMeta: metadata(""), Data: map[string]string{ConfigKey: config}}
	configVolume := corev1.Volume{
		Name: "config",
		VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: name}, DefaultMode: ptr.To[int32](0o444),
		}},
	}
	secretEnv := func(name, key string) corev1.EnvVar {
		return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: t.Spec.StorageSecretRef}, Key: key,
		}}}
	}
	fieldEnv := func(name, path string) corev1.EnvVar {
		return corev1.EnvVar{
			Name:      name,
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: path}},
		}
	}
	security := &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true),
		Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
	requests := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("50m"),
		corev1.ResourceMemory: resource.MustParse("128Mi"),
	}
	limits := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1"),
		corev1.ResourceMemory: resource.MustParse("384Mi"),
	}
	worker := &appsv1.Deployment{ObjectMeta: metadata("-recorder"), Spec: appsv1.DeploymentSpec{
		Replicas: ptr.To[int32](1),
		Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{observation.LabelTelemetryUID: string(t.UID), "app": "recorder"},
		},
		Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
			observation.LabelTelemetryUID: string(t.UID), "app": "recorder",
		}}, Spec: corev1.PodSpec{
			ServiceAccountName: name + "-recorder", AutomountServiceAccountToken: ptr.To(true),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](65532),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name: "recorder", Image: workerImage, ImagePullPolicy: corev1.PullIfNotPresent,
				Command: []string{"/recorder"}, Args: []string{
					"--namespace=" + t.Namespace, "--telemetry=" + t.Name,
					"--uid=" + string(t.UID), "--generation=" + strconv.FormatInt(t.Generation, 10),
				},
				Env: []corev1.EnvVar{
					fieldEnv("POD_UID", "metadata.uid"), secretEnv("GREPTIME_ENDPOINT", EndpointKey),
					secretEnv("GREPTIME_AUTH", AuthorizationKey),
				},
				SecurityContext: security, Resources: corev1.ResourceRequirements{Requests: requests, Limits: limits},
			}},
		}},
	}}
	collector := &appsv1.DaemonSet{ObjectMeta: metadata("-collector"), Spec: appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{observation.LabelTelemetryUID: string(t.UID), "app": "collector"},
		},
		Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
			observation.LabelTelemetryUID: string(t.UID), "app": "collector",
		}, Annotations: map[string]string{"observation.stacks.org/config-digest": digest(config)}}, Spec: corev1.PodSpec{
			ServiceAccountName: name + "-collector", AutomountServiceAccountToken: ptr.To(true),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser:      ptr.To[int64](0),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Tolerations: []corev1.Toleration{{
				Key: "node-role.kubernetes.io/control-plane", Operator: corev1.TolerationOpExists,
				Effect: corev1.TaintEffectNoSchedule,
			}},
			Volumes: []corev1.Volume{
				configVolume,
				{
					Name: "logs",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/var/log/pods",
							Type: ptr.To(corev1.HostPathDirectory),
						},
					},
				},
				{Name: "checkpoints", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{
					Path: "/var/lib/stacks-telemetry/" + string(t.UID), Type: ptr.To(corev1.HostPathDirectoryOrCreate),
				}}},
			},
			Containers: []corev1.Container{
				{
					Name:            "collector",
					Image:           collectorImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args:            []string{"--config=/config/" + ConfigKey},
					Env: []corev1.EnvVar{
						fieldEnv("NODE_NAME", "spec.nodeName"), fieldEnv("POD_UID", "metadata.uid"),
						secretEnv("GREPTIME_ENDPOINT", EndpointKey), secretEnv("GREPTIME_AUTH", AuthorizationKey),
					},
					SecurityContext: security,
					Resources:       corev1.ResourceRequirements{Requests: requests, Limits: limits},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "config", MountPath: "/config", ReadOnly: true},
						{Name: "logs", MountPath: "/var/log/pods", ReadOnly: true},
						{Name: "checkpoints", MountPath: "/var/lib/otelcol"},
					},
					Ports: []corev1.ContainerPort{
						{Name: "health", ContainerPort: 13133},
						{Name: "metrics", ContainerPort: 8888},
					},
					ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
						Path: "/", Port: intstr.FromInt32(13133),
					}}, PeriodSeconds: 5},
				},
			},
		}},
	}}
	if !t.Spec.Sources.Logs {
		collector.Spec.Template.Spec.Volumes = []corev1.Volume{
			configVolume,
			{
				Name: "checkpoints",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptr.To(resource.MustParse("512Mi"))},
				},
			},
		}
		collector.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: "config", MountPath: "/config", ReadOnly: true},
			{Name: "checkpoints", MountPath: "/var/lib/otelcol"},
		}
		collector.Spec.Template.Spec.SecurityContext.RunAsUser = ptr.To[int64](65532)
		collector.Spec.Template.Spec.SecurityContext.RunAsNonRoot = ptr.To(true)
		collector.Spec.Template.Spec.SecurityContext.FSGroup = ptr.To[int64](65532)
	}
	objects := []client.Object{}
	if NeedsCollector(t) {
		objects = append(objects, configMap, collector)
	}
	for _, entry := range []struct {
		name  string
		rules []rbacv1.PolicyRule
	}{
		{"-recorder", RecorderRules(t.Name)}, {"-collector", CollectorRules()},
	} {
		if entry.name == "-collector" && !NeedsCollector(t) {
			continue
		}
		objects = append(
			objects,
			&corev1.ServiceAccount{ObjectMeta: metadata(entry.name), AutomountServiceAccountToken: ptr.To(true)},
			&rbacv1.Role{ObjectMeta: metadata(entry.name), Rules: entry.rules},
			&rbacv1.RoleBinding{
				ObjectMeta: metadata(
					entry.name,
				),
				RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name + entry.name},
				Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: name + entry.name, Namespace: t.Namespace}},
			},
		)
	}
	objects = append(objects, worker)
	return objects, nil
}

// digest detects collector configuration changes without exposing content in labels.
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// CollectorRules grants only Pod discovery in the recording namespace.
func CollectorRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list", "watch"}},
	}
}

// RecorderRules scopes status writes to the exact telemetry object and observation reads to supported sources.
func RecorderRules(name string) []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				observation.GroupVersion.Group,
			},
			Resources:     []string{observation.ResourceNetworkTelemetries},
			ResourceNames: []string{name},
			Verbs:         []string{"get"},
		},
		{
			APIGroups: []string{
				observation.GroupVersion.Group,
			},
			Resources:     []string{observation.ResourceNetworkTelemetries + "/status"},
			ResourceNames: []string{name},
			Verbs:         []string{"patch"},
		},
		{
			APIGroups: []string{"network.stacks.org"},
			Resources: []string{"stacksnetworks", "stacksnetworkparticipants", "stacksgeneses"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"bitcoin.stacks.org"},
			Resources: []string{"bitcoinexecutions"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"actions.stacks.org"},
			Resources: []string{"bitcoinblockgenerations", "bitcoinreorganizations"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"chaos-mesh.org"},
			Resources: []string{"networkchaos"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{APIGroups: []string{""}, Resources: []string{"pods", "events"}, Verbs: []string{"get", "list", "watch"}},
	}
}

// NeedsCollector reports whether any enabled source needs a node collection workload.
func NeedsCollector(t *observation.NetworkTelemetry) bool {
	return t.Spec.Sources.Logs || t.Spec.Sources.Metrics
}
