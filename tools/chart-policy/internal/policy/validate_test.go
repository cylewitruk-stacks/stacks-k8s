package policy

import (
	"bytes"
	"encoding/json"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateAcceptsHardenedDeployment(t *testing.T) {
	if err := validate(hardenedDeployment()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateChecksEveryCapabilityDeployment(t *testing.T) {
	first := hardenedDeployment()
	second := hardenedDeployment()
	encode := func() *bytes.Reader {
		left, _ := json.Marshal(first)
		right, _ := json.Marshal(second)
		return bytes.NewReader(append(append(left, []byte("\n---\n")...), right...))
	}
	if err := Validate(encode()); err != nil {
		t.Fatal(err)
	}
	second.Spec.Template.Spec.HostNetwork = true
	if err := Validate(encode()); err == nil {
		t.Fatal("unsafe additional capability Deployment accepted")
	}
}

func TestValidateRejectsEveryWorkloadWeakening(t *testing.T) {
	tests := map[string]func(*corev1.PodSpec){
		"host network":             func(p *corev1.PodSpec) { p.HostNetwork = true },
		"host PID":                 func(p *corev1.PodSpec) { p.HostPID = true },
		"host IPC":                 func(p *corev1.PodSpec) { p.HostIPC = true },
		"shared process namespace": func(p *corev1.PodSpec) { p.ShareProcessNamespace = pointer(true) },
		"init container":           func(p *corev1.PodSpec) { p.InitContainers = []corev1.Container{{Name: "init"}} },
		"ephemeral container":      func(p *corev1.PodSpec) { p.EphemeralContainers = []corev1.EphemeralContainer{{}} },
		"sidecar":                  func(p *corev1.PodSpec) { p.Containers = append(p.Containers, corev1.Container{Name: "sidecar"}) },
		"host path": func(p *corev1.PodSpec) {
			p.Volumes = []corev1.Volume{{VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}}
		},
		"host port":            func(p *corev1.PodSpec) { p.Containers[0].Ports = []corev1.ContainerPort{{HostPort: 8080}} },
		"privileged":           func(p *corev1.PodSpec) { p.Containers[0].SecurityContext.Privileged = pointer(true) },
		"privilege escalation": func(p *corev1.PodSpec) { p.Containers[0].SecurityContext.AllowPrivilegeEscalation = pointer(true) },
		"writable root":        func(p *corev1.PodSpec) { p.Containers[0].SecurityContext.ReadOnlyRootFilesystem = pointer(false) },
		"capability addition": func(p *corev1.PodSpec) {
			p.Containers[0].SecurityContext.Capabilities.Add = []corev1.Capability{"SYS_ADMIN"}
		},
		"incomplete capability drop": func(p *corev1.PodSpec) {
			p.Containers[0].SecurityContext.Capabilities.Drop = []corev1.Capability{"NET_RAW"}
		},
		"container root override": func(p *corev1.PodSpec) { p.Containers[0].SecurityContext.RunAsNonRoot = pointer(false) },
		"container UID zero":      func(p *corev1.PodSpec) { p.Containers[0].SecurityContext.RunAsUser = pointer(int64(0)) },
		"unconfined seccomp": func(p *corev1.PodSpec) {
			p.Containers[0].SecurityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
		},
		"unconfined AppArmor": func(p *corev1.PodSpec) {
			p.Containers[0].SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}
		},
		"unmasked proc":          func(p *corev1.PodSpec) { p.Containers[0].SecurityContext.ProcMount = pointer(corev1.UnmaskedProcMount) },
		"pod permits root":       func(p *corev1.PodSpec) { p.SecurityContext.RunAsNonRoot = pointer(false) },
		"pod UID zero":           func(p *corev1.PodSpec) { p.SecurityContext.RunAsUser = pointer(int64(0)) },
		"pod unconfined seccomp": func(p *corev1.PodSpec) { p.SecurityContext.SeccompProfile.Type = corev1.SeccompProfileTypeUnconfined },
		"pod unconfined AppArmor": func(p *corev1.PodSpec) {
			p.SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			deployment := hardenedDeployment()
			mutate(&deployment.Spec.Template.Spec)
			if err := validate(deployment); err == nil {
				t.Fatal("unsafe Deployment was accepted")
			}
		})
	}
}

func TestValidateAllowsExplicitSafeOverrides(t *testing.T) {
	deployment := hardenedDeployment()
	pod := &deployment.Spec.Template.Spec
	pod.ShareProcessNamespace = pointer(false)
	pod.Containers[0].SecurityContext.RunAsNonRoot = pointer(true)
	pod.Containers[0].SecurityContext.RunAsUser = pointer(int64(65532))
	pod.Containers[0].SecurityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	pod.Containers[0].SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeRuntimeDefault}
	pod.Containers[0].SecurityContext.ProcMount = pointer(corev1.DefaultProcMount)
	pod.SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeRuntimeDefault}
	if err := validate(deployment); err != nil {
		t.Fatal(err)
	}
}

func validate(deployment appsv1.Deployment) error {
	value, _ := json.Marshal(deployment)
	return Validate(bytes.NewReader(value))
}

func hardenedDeployment() appsv1.Deployment {
	return appsv1.Deployment{TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: pointer(true), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
		Containers: []corev1.Container{{Name: "manager", SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: pointer(false), ReadOnlyRootFilesystem: pointer(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		}}},
	}}}}
}

func pointer[T any](value T) *T { return &value }
