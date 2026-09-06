// Package policy verifies security invariants in rendered operator workloads.
package policy

import (
	"encoding/json"
	"fmt"
	"io"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Validate requires hardened operator Deployments, including optional capability controllers.
func Validate(reader io.Reader) error {
	decoder := yaml.NewYAMLOrJSONDecoder(reader, 4096)
	var deployments []appsv1.Deployment
	for {
		object := &unstructured.Unstructured{}
		if err := decoder.Decode(object); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("decode rendered chart: %w", err)
		}
		if object.GetKind() != "Deployment" {
			continue
		}
		encoded, err := json.Marshal(object.Object)
		if err != nil {
			return err
		}
		var deployment appsv1.Deployment
		if err := json.Unmarshal(encoded, &deployment); err != nil {
			return fmt.Errorf("decode rendered Deployment: %w", err)
		}
		deployments = append(deployments, deployment)
	}
	if len(deployments) == 0 {
		return fmt.Errorf("expected at least one operator Deployment")
	}
	for _, deployment := range deployments {
		if err := validatePodSpec(deployment.Spec.Template.Spec); err != nil {
			return fmt.Errorf("Deployment %s: %w", deployment.Name, err)
		}
	}
	return nil
}

func validatePodSpec(pod corev1.PodSpec) error {
	if pod.HostNetwork || pod.HostPID || pod.HostIPC || enabled(pod.ShareProcessNamespace) {
		return fmt.Errorf("operator Deployment enables host or shared process namespaces")
	}
	if len(pod.InitContainers) != 0 || len(pod.EphemeralContainers) != 0 || len(pod.Containers) != 1 || pod.Containers[0].Name != "manager" {
		return fmt.Errorf("operator Deployment must contain only the manager container")
	}
	for _, volume := range pod.Volumes {
		if volume.HostPath != nil {
			return fmt.Errorf("operator Deployment must not use hostPath volumes")
		}
	}
	container := pod.Containers[0]
	for _, port := range container.Ports {
		if port.HostPort != 0 {
			return fmt.Errorf("operator Deployment must not expose host ports")
		}
	}
	security := container.SecurityContext
	if security == nil || enabled(security.Privileged) || security.AllowPrivilegeEscalation == nil || *security.AllowPrivilegeEscalation ||
		security.ReadOnlyRootFilesystem == nil || !*security.ReadOnlyRootFilesystem || security.Capabilities == nil ||
		len(security.Capabilities.Add) != 0 || len(security.Capabilities.Drop) != 1 || security.Capabilities.Drop[0] != corev1.Capability("ALL") ||
		security.RunAsNonRoot != nil && !*security.RunAsNonRoot || security.RunAsUser != nil && *security.RunAsUser == 0 ||
		security.SeccompProfile != nil && security.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault ||
		security.AppArmorProfile != nil && security.AppArmorProfile.Type == corev1.AppArmorProfileTypeUnconfined ||
		security.ProcMount != nil && *security.ProcMount != corev1.DefaultProcMount {
		return fmt.Errorf("manager container security context is not hardened")
	}
	podSecurity := pod.SecurityContext
	if podSecurity == nil || podSecurity.RunAsNonRoot == nil || !*podSecurity.RunAsNonRoot ||
		podSecurity.RunAsUser != nil && *podSecurity.RunAsUser == 0 || podSecurity.SeccompProfile == nil ||
		podSecurity.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault ||
		podSecurity.AppArmorProfile != nil && podSecurity.AppArmorProfile.Type == corev1.AppArmorProfileTypeUnconfined {
		return fmt.Errorf("operator Pod security context is not hardened")
	}
	return nil
}

func enabled(value *bool) bool { return value != nil && *value }
