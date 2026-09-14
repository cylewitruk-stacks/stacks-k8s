// Package telemetry tests the optional backend and local dashboard installation contract.
package telemetry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// chart resolves the repository chart from this verification package.
func chart() string {
	return filepath.Join("..", "..", "..", "..", "charts", "stacks-observability-operator")
}

// render runs Helm without cluster access or dependency downloads.
func render(t *testing.T, arguments ...string) ([]byte, error) {
	t.Helper()
	args := append([]string{"template", "telemetry", chart(), "--namespace", "observations"}, arguments...)
	// Test-owned chart path and settings; no untrusted command arguments.
	command := exec.CommandContext(t.Context(), "helm", args...) // #nosec G204
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("helm: %w: %s", err, stderr.String())
	}
	return out, nil
}

// objects decodes the chart's typed-resource boundary for structural assertions.
func objects(t *testing.T, data []byte) []*unstructured.Unstructured {
	t.Helper()
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var result []*unstructured.Unstructured
	for {
		object := &unstructured.Unstructured{}
		if err := decoder.Decode(object); errors.Is(err, io.EOF) {
			return result
		} else if err != nil {
			t.Fatal(err)
		}
		if object.GetKind() != "" {
			result = append(result, object)
		}
	}
}

// decode uses production Kubernetes types rather than matching YAML substrings.
func decode(t *testing.T, object *unstructured.Unstructured, target any) {
	t.Helper()
	data, err := json.Marshal(object.Object)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func TestBackendAndDashboardComposition(t *testing.T) {
	for _, mode := range []struct {
		name             string
		enabled, exposed bool
		args             []string
	}{
		{name: "external"},
		{name: "internal", enabled: true, args: []string{"--set", "greptime.enabled=true"}},
		{
			name: "local", enabled: true, exposed: true,
			args: []string{"--values", filepath.Join(chart(), "values-local.yaml")},
		},
	} {
		t.Run(mode.name, func(t *testing.T) {
			data, err := render(t, mode.args...)
			if err != nil {
				t.Fatal(err)
			}
			var backend *appsv1.StatefulSet
			var dashboard *corev1.Service
			var initializer *batchv1.Job
			for _, object := range objects(t, data) {
				switch object.GetKind() {
				case "StatefulSet":
					backend = &appsv1.StatefulSet{}
					decode(t, object, backend)
				case "Job":
					initializer = &batchv1.Job{}
					decode(t, object, initializer)
				case "Secret":
					t.Fatal("chart must use an existing credential, not render passwords")
				case "Service":
					service := &corev1.Service{}
					decode(t, object, service)
					if service.Spec.Type == corev1.ServiceTypeNodePort {
						dashboard = service
					}
				}
			}
			if (backend != nil) != mode.enabled || (initializer != nil) != mode.enabled ||
				(dashboard != nil) != mode.exposed {
				t.Fatal("optional backend resources differ from enabled settings")
			}
			if !mode.enabled {
				return
			}
			retention := backend.Spec.PersistentVolumeClaimRetentionPolicy
			if retention.WhenDeleted != appsv1.RetainPersistentVolumeClaimRetentionPolicyType {
				t.Fatal("uninstall would delete backend storage")
			}
			pod := initializer.Spec.Template.Spec
			if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken ||
				pod.SecurityContext == nil || !*pod.SecurityContext.RunAsNonRoot ||
				pod.RestartPolicy != corev1.RestartPolicyNever || initializer.Spec.ActiveDeadlineSeconds == nil ||
				initializer.Annotations["helm.sh/hook"] != "post-install,post-upgrade" {
				t.Fatal("initializer must be bounded, tokenless and run after backend installation")
			}
			if len(pod.Containers) != 1 || !reflect.DeepEqual(pod.Containers[0].Command, []string{"/backend-init"}) ||
				len(pod.Volumes) != 1 || pod.Volumes[0].Secret.SecretName != "greptime-auth" {
				t.Fatal("initializer credential or entrypoint changed")
			}
			endpoint := "--endpoint=http://telemetry-greptime.observations.svc:4000"
			if mode.name == "local" {
				endpoint = "--endpoint=http://greptime.observations.svc:4000"
				if !reflect.DeepEqual(backend.Spec.Template.Spec.NodeSelector,
					map[string]string{"kubernetes.io/hostname": "stacks-k8s-control-plane"}) ||
					!reflect.DeepEqual(backend.Spec.Template.Spec.Tolerations, []corev1.Toleration{{
						Key: "node-role.kubernetes.io/control-plane", Operator: corev1.TolerationOpExists,
						Effect: corev1.TaintEffectNoSchedule,
					}}) {
					t.Fatal("local backend must retain control-plane placement and toleration")
				}
			}
			if !slices.Contains(pod.Containers[0].Args, endpoint) {
				t.Fatalf("initializer endpoint must be %s; got %v", endpoint, pod.Containers[0].Args)
			}
			if !mode.exposed {
				return
			}
			if !reflect.DeepEqual(dashboard.Spec.Selector, backend.Spec.Template.Labels) ||
				len(dashboard.Spec.Ports) != 1 || dashboard.Spec.Ports[0].NodePort != 30400 ||
				dashboard.Spec.Ports[0].TargetPort.String() != "http" || dashboard.Spec.Ports[0].Port != 4000 {
				t.Fatal("dashboard must select the bundled backend and expose HTTP alone")
			}
		})
	}
}

func TestUnsafeBackendSettingsRejected(t *testing.T) {
	for _, setting := range []string{
		"dashboard.enabled=true", "greptime.enabled=true,greptime.auth.enabled=false",
		"greptime.enabled=true,greptime.auth.existingSecretName=", "dashboard.nodePort=14000",
		"greptime.enabled=true,greptime.service.type=NodePort",
	} {
		t.Run(setting, func(t *testing.T) {
			if _, err := render(t, "--set", setting); err == nil {
				t.Fatal("invalid backend settings rendered")
			}
		})
	}
}

func TestKindDashboardMapping(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "tools", "local-cluster", "kind.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Nodes []struct {
			Role              string `json:"role"`
			ExtraPortMappings []struct {
				ContainerPort int    `json:"containerPort"`
				HostPort      int    `json:"hostPort"`
				ListenAddress string `json:"listenAddress"`
				Protocol      string `json:"protocol"`
			} `json:"extraPortMappings"`
		} `json:"nodes"`
	}
	if err := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096).Decode(&config); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range config.Nodes {
		for _, port := range node.ExtraPortMappings {
			if port.HostPort != 14000 && port.ContainerPort != 30400 {
				continue
			}
			if found || node.Role != "control-plane" || port.ContainerPort != 30400 ||
				port.HostPort != 14000 || port.ListenAddress != "127.0.0.1" || port.Protocol != "TCP" {
				t.Fatal("kind dashboard mapping must uniquely expose the control-plane NodePort on loopback")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing kind dashboard mapping")
	}
}

// TestInitializerEndpointOverride checks service discovery independently of dashboard exposure.
func TestInitializerEndpointOverride(t *testing.T) {
	data, err := render(t, "--set", "greptime.enabled=true,greptime.fullnameOverride=custom-backend",
		"--set", "greptime.httpServicePort=4100", "--namespace", "custom-observations")
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range objects(t, data) {
		if object.GetKind() != "Job" {
			continue
		}
		job := &batchv1.Job{}
		decode(t, object, job)
		if len(job.Spec.Template.Spec.Containers) != 1 || !slices.Contains(job.Spec.Template.Spec.Containers[0].Args,
			"--endpoint=http://custom-backend.custom-observations.svc:4100") {
			t.Fatal("initializer ignored backend name, namespace or HTTP port override")
		}
		return
	}
	t.Fatal("initializer job absent")
}

// TestBackendOverrides ensures chart options do not require the local fixed resource name.
func TestBackendOverrides(t *testing.T) {
	data, err := render(t, "--set", "greptime.enabled=true,dashboard.enabled=true,dashboard.nodePort=31400",
		"--set", "greptime.fullnameOverride=custom-backend,backendInitialization.enabled=false")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range objects(t, data) {
		if object.GetKind() == "Job" {
			t.Fatal("disabled initializer still rendered")
		}
		if object.GetKind() != "Service" {
			continue
		}
		service := &corev1.Service{}
		decode(t, object, service)
		if service.Spec.Type != corev1.ServiceTypeNodePort {
			continue
		}
		found = true
		if service.Name != "custom-backend-dashboard" || service.Spec.Ports[0].NodePort != 31400 {
			t.Fatal("dashboard ignored backend-name or NodePort override")
		}
	}
	if !found {
		t.Fatal("dashboard service absent")
	}
}
