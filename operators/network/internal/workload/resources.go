package workload

import (
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/network/api/v1alpha1"
	operatorlabels "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/labels"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
)

const configDigestAnnotation = "network.stacks.org/config-digest"

var reservedEnvironment = map[string]struct{}{
	"POD_IP": {}, "STACKS_ACTOR": {}, "STACKS_ACTOR_ROLE": {}, "STACKS_CONFIG_RENDERED": {},
	"STACKS_CONFIG_TEMPLATE": {}, "STACKS_NETWORK": {}, "STACKS_SERVICE_MAP": {},
}

type resources struct {
	configMap   *corev1.ConfigMap
	service     *corev1.Service
	statefulSet *appsv1.StatefulSet
}

func render(descriptor Descriptor, schemeOwner func(metav1.Object) error) (resources, error) {
	config, err := prepareConfig(descriptor)
	if err != nil {
		return resources{}, err
	}
	actorKind, err := operatorlabels.FromResourceKind(descriptor.Kind)
	if err != nil {
		return resources{}, err
	}
	labels := operatorlabels.ForActor(descriptor.Network, descriptor.Actor, actorKind)
	labels[operatorlabels.ActorResourceKey] = descriptor.Owner.GetName()
	metadata := func(name string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: name, Namespace: descriptor.Owner.GetNamespace(), Labels: copyMap(labels)}
	}
	var configMap *corev1.ConfigMap
	if config.configMapData != nil {
		configMap = &corev1.ConfigMap{ObjectMeta: metadata(configName(descriptor.Owner.GetName())), Data: config.configMapData}
		if err := schemeOwner(configMap); err != nil {
			return resources{}, err
		}
	}
	ports := descriptor.Ports
	servicePorts, containerPorts := make([]corev1.ServicePort, 0, len(ports)), make([]corev1.ContainerPort, 0, len(ports))
	for _, port := range ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = corev1.ProtocolTCP
		}
		servicePorts = append(servicePorts, corev1.ServicePort{Name: port.Name, Port: port.Port, TargetPort: intstr.FromString(port.Name), Protocol: protocol})
		containerPorts = append(containerPorts, corev1.ContainerPort{Name: port.Name, ContainerPort: port.Port, Protocol: protocol})
	}
	service := &corev1.Service{ObjectMeta: metadata(descriptor.Owner.GetName()), Spec: corev1.ServiceSpec{
		Type: corev1.ServiceTypeClusterIP, ClusterIP: corev1.ClusterIPNone, PublishNotReadyAddresses: true,
		SessionAffinity:       corev1.ServiceAffinityNone,
		InternalTrafficPolicy: pointer(corev1.ServiceInternalTrafficPolicyCluster),
		Selector:              identitySelector(labels), Ports: servicePorts,
	}}
	if err := schemeOwner(service); err != nil {
		return resources{}, err
	}

	storage := effectiveStorage(descriptor.Workload.Storage)
	actorCommand, actorArgs := append([]string(nil), descriptor.Command...), append([]string(nil), descriptor.Args...)
	environment := append([]corev1.EnvVar(nil), descriptor.Env...)
	environment = append(environment,
		corev1.EnvVar{Name: "STACKS_NETWORK", Value: descriptor.Network},
		corev1.EnvVar{Name: "STACKS_ACTOR", Value: descriptor.Actor},
		corev1.EnvVar{Name: "STACKS_ACTOR_ROLE", Value: descriptor.Role},
		corev1.EnvVar{Name: "POD_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "status.podIP"}}},
	)
	if descriptor.Container != nil {
		if descriptor.Container.Command != nil {
			actorCommand = append([]string(nil), descriptor.Container.Command...)
		}
		if descriptor.Container.Args != nil {
			actorArgs = append([]string(nil), descriptor.Container.Args...)
		}
	}
	command, args := actorCommand, actorArgs
	if descriptor.Kind != "BitcoinNode" {
		renderedPath := "/tmp/stacks-network-config/" + config.key
		environment = append(environment,
			corev1.EnvVar{Name: "STACKS_CONFIG_TEMPLATE", Value: config.mountPath + "/" + config.key},
			corev1.EnvVar{Name: "STACKS_CONFIG_RENDERED", Value: renderedPath},
			corev1.EnvVar{Name: "STACKS_SERVICE_MAP", Value: serviceMapEnvironment(descriptor.ServiceMap)},
		)
		command = []string{"/bin/bash", "-ceu", renderStacksConfigScript, "--"}
		command = append(command, actorCommand...)
		args = append([]string(nil), actorArgs...)
	}
	resourceRequirements, err := resourceRequirements(descriptor.Workload.Resources)
	if err != nil {
		return resources{}, fmt.Errorf("actor %q resources: %w", descriptor.Actor, err)
	}
	main := corev1.Container{Name: "actor", Image: descriptor.Image, ImagePullPolicy: descriptor.ImagePullPolicy,
		Command: command, Args: args, Env: environment, Ports: containerPorts, Resources: resourceRequirements,
		SecurityContext: restrictedSecurity(false),
		VolumeMounts:    []corev1.VolumeMount{{Name: "data", MountPath: storage.MountPath}, {Name: "actor-config", MountPath: config.mountPath, ReadOnly: true}},
	}
	if descriptor.Container != nil {
		names := make([]string, 0, len(descriptor.Container.Env))
		for name := range descriptor.Container.Env {
			if _, reserved := reservedEnvironment[name]; reserved {
				return resources{}, fmt.Errorf("actor %q container environment variable %q is reserved by the operator", descriptor.Actor, name)
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value := descriptor.Container.Env[name]
			main.Env = append(main.Env, corev1.EnvVar{Name: name, Value: value})
		}
		main.WorkingDir = descriptor.Container.WorkingDir
	}
	if main.ReadinessProbe == nil {
		main.ReadinessProbe = defaultReadiness(descriptor.Kind)
	}
	volumes := []corev1.Volume{config.volume}
	claims := []corev1.PersistentVolumeClaim{}
	if storage.Enabled {
		quantity, err := resource.ParseQuantity(storage.Size)
		if err != nil {
			return resources{}, fmt.Errorf("actor %q storage size: %w", descriptor.Actor, err)
		}
		claims = append(claims, corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Labels: copyMap(labels)}, Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: storage.AccessModes, StorageClassName: storage.StorageClassName,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: quantity}},
		}})
	} else {
		volumes = append(volumes, corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	}
	if descriptor.Kind != "BitcoinNode" {
		renderedConfigLimit := resource.MustParse("2Mi")
		volumes = append(volumes, corev1.Volume{Name: "rendered-config", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{
			Medium: corev1.StorageMediumMemory, SizeLimit: &renderedConfigLimit,
		}}})
		main.VolumeMounts = append(main.VolumeMounts, corev1.VolumeMount{Name: "rendered-config", MountPath: "/tmp/stacks-network-config"})
	}
	initContainers := []corev1.Container{}
	if config.expectedDigest != "" {
		initContainers = append(initContainers, corev1.Container{Name: "verify-config", Image: descriptor.DependencyImage,
			Command:      []string{"sh", "-ec", `set -- $(sha256sum "$CONFIG_FILE"); test "sha256:$1" = "$EXPECTED_DIGEST"`},
			Env:          []corev1.EnvVar{{Name: "CONFIG_FILE", Value: config.mountPath + "/" + config.key}, {Name: "EXPECTED_DIGEST", Value: config.expectedDigest}},
			VolumeMounts: []corev1.VolumeMount{{Name: "actor-config", MountPath: config.mountPath, ReadOnly: true}}, SecurityContext: restrictedSecurity(true),
		})
	}
	if len(descriptor.Dependencies) > 0 {
		checks := make([]string, 0, len(descriptor.Dependencies))
		for _, dependency := range descriptor.Dependencies {
			checks = append(checks, fmt.Sprintf("until nc -z %s %d; do sleep 1; done", dependency.Host, dependency.Port))
		}
		initContainers = append(initContainers, corev1.Container{Name: "wait-for-dependencies", Image: descriptor.DependencyImage,
			Command: []string{"sh", "-ec", strings.Join(checks, "; ")}, SecurityContext: restrictedSecurity(true)})
	}
	grace := int64(30)
	if descriptor.Workload.TerminationGracePeriodSeconds != nil {
		grace = *descriptor.Workload.TerminationGracePeriodSeconds
	}
	replicas := int32(1)
	if descriptor.Suspended {
		replicas = 0
	}
	deletePolicy := appsv1.DeletePersistentVolumeClaimRetentionPolicyType
	if storage.RetainOnDelete {
		deletePolicy = appsv1.RetainPersistentVolumeClaimRetentionPolicyType
	}
	statefulSet := &appsv1.StatefulSet{ObjectMeta: metadata(descriptor.Owner.GetName()), Spec: appsv1.StatefulSetSpec{
		ServiceName: service.Name, Replicas: &replicas, PodManagementPolicy: appsv1.ParallelPodManagement,
		Selector:                             &metav1.LabelSelector{MatchLabels: identitySelector(labels)},
		UpdateStrategy:                       appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
		PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{WhenDeleted: deletePolicy, WhenScaled: appsv1.RetainPersistentVolumeClaimRetentionPolicyType},
		Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: copyMap(labels), Annotations: map[string]string{configDigestAnnotation: config.digest}}, Spec: corev1.PodSpec{
			AutomountServiceAccountToken: pointer(false), RestartPolicy: corev1.RestartPolicyAlways, DNSPolicy: corev1.DNSClusterFirst,
			TerminationGracePeriodSeconds: &grace, SecurityContext: podSecurity(),
			Containers: []corev1.Container{main}, InitContainers: initContainers, Volumes: volumes,
			ImagePullSecrets: imagePullSecrets(descriptor.ImagePullSecrets), NodeSelector: copyMap(descriptor.Workload.NodeSelector),
			Tolerations: tolerations(descriptor.Workload.Tolerations),
		}}, VolumeClaimTemplates: claims,
	}}
	if err := schemeOwner(statefulSet); err != nil {
		return resources{}, err
	}
	return resources{configMap: configMap, service: service, statefulSet: statefulSet}, nil
}

type storageSettings struct {
	Enabled          bool
	Size, MountPath  string
	StorageClassName *string
	AccessModes      []corev1.PersistentVolumeAccessMode
	RetainOnDelete   bool
}

func effectiveStorage(value *networkv1alpha1.StorageSpec) storageSettings {
	result := storageSettings{Enabled: true, Size: "1Gi", MountPath: "/data", AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}}
	if value == nil {
		return result
	}
	if value.Enabled != nil {
		result.Enabled = *value.Enabled
	}
	if value.Size != "" {
		result.Size = value.Size
	}
	if value.MountPath != "" {
		result.MountPath = value.MountPath
	}
	result.StorageClassName = value.StorageClassName
	if len(value.AccessModes) > 0 {
		result.AccessModes = append([]corev1.PersistentVolumeAccessMode(nil), value.AccessModes...)
	}
	result.RetainOnDelete = value.RetainOnDelete
	return result
}
func resourceRequirements(value *networkv1alpha1.ResourceSpec) (corev1.ResourceRequirements, error) {
	if value == nil {
		return corev1.ResourceRequirements{}, nil
	}
	result := corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
	for name, input := range map[corev1.ResourceName]string{corev1.ResourceCPU: value.Requests.CPU, corev1.ResourceMemory: value.Requests.Memory} {
		if input == "" {
			continue
		}
		quantity, err := resource.ParseQuantity(input)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("request %s: %w", name, err)
		}
		result.Requests[name] = quantity
	}
	for name, input := range map[corev1.ResourceName]string{corev1.ResourceCPU: value.Limits.CPU, corev1.ResourceMemory: value.Limits.Memory} {
		if input == "" {
			continue
		}
		quantity, err := resource.ParseQuantity(input)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("limit %s: %w", name, err)
		}
		result.Limits[name] = quantity
	}
	if len(result.Requests) == 0 {
		result.Requests = nil
	}
	if len(result.Limits) == 0 {
		result.Limits = nil
	}
	return result, nil
}
func restrictedSecurity(readOnly bool) *corev1.SecurityContext {
	return &corev1.SecurityContext{AllowPrivilegeEscalation: pointer(false), ReadOnlyRootFilesystem: pointer(readOnly), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}
}
func podSecurity() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}
}

func tolerations(values []networkv1alpha1.TolerationSpec) []corev1.Toleration {
	result := make([]corev1.Toleration, len(values))
	for index, value := range values {
		result[index] = corev1.Toleration{Key: value.Key, Operator: value.Operator, Value: value.Value, Effect: value.Effect, TolerationSeconds: value.TolerationSeconds}
	}
	return result
}
func imagePullSecrets(values []networkv1alpha1.LocalObjectReference) []corev1.LocalObjectReference {
	result := make([]corev1.LocalObjectReference, len(values))
	for index, value := range values {
		result[index].Name = value.Name
	}
	return result
}
func defaultReadiness(kind string) *corev1.Probe {
	port := "rpc"
	if kind == "StacksSigner" {
		port = "events"
	}
	return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString(port)}}, PeriodSeconds: 2, FailureThreshold: 30}
}
func pointer[T any](value T) *T { return &value }
func copyMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func identitySelector(actorLabels map[string]string) map[string]string {
	return map[string]string{
		operatorlabels.ManagedByKey:     actorLabels[operatorlabels.ManagedByKey],
		operatorlabels.NetworkKey:       actorLabels[operatorlabels.NetworkKey],
		operatorlabels.ActorKey:         actorLabels[operatorlabels.ActorKey],
		operatorlabels.ActorResourceKey: actorLabels[operatorlabels.ActorResourceKey],
	}
}
func resourceName(owner, suffix string) string { return naming.Resource(owner, suffix) }
