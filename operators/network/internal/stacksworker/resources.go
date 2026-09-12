package stacksworker

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
)

// KeyMount exposes exactly one immutable key under a role-specific filesystem path.
type KeyMount struct {
	// Role identifies the signer purpose, such as holder or administrator.
	Role string `json:"role"`
	// Secret pins the immutable reusable key resource.
	Secret common.Binding `json:"secret"`
	// Key selects only the required Secret entry.
	Key string `json:"key"`
}

// ReadBinding grants named read/watch access to one public prerequisite resource.
type ReadBinding struct {
	// APIVersion selects the public Kubernetes API version.
	APIVersion string `json:"apiVersion"`
	// Resource selects the plural resource name, never a subresource.
	Resource string `json:"resource"`
	// Name scopes every list/watch with metadata.name.
	Name string `json:"name"`
}

// Profile contains fixed workload/key inputs; live policy and controls are excluded.
type Profile struct {
	// Image is selected once before binding, then retained across operator upgrades.
	Image string `json:"image"`
	// Configuration pins immutable public bootstrap configuration.
	Configuration common.Binding `json:"configuration"`
	// Keys contains only the signing purposes required by this role.
	Keys []KeyMount `json:"keys"`
	// Reads grants explicitly named public prerequisite observations.
	Reads []ReadBinding `json:"reads,omitempty"`
	// Placement selects support-worker scheduling independently of actor placement.
	Placement *common.Placement `json:"placement,omitempty"`
	// Resources supplies the fixed support-worker budget.
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

var roleName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// Normalize validates and canonicalizes effective immutable profile inputs.
func (p Profile) Normalize() (Profile, error) {
	if p.Image == "" || len(p.Image) > 512 || p.Configuration.Kind != "ConfigMap" || p.Configuration.Name == "" || p.Configuration.UID == "" || len(p.Keys) == 0 || len(p.Keys) > 8 || len(p.Reads) > 100 {
		return p, fmt.Errorf("worker profile is incomplete or exceeds bounds")
	}
	p.Keys = append([]KeyMount(nil), p.Keys...)
	p.Reads = append([]ReadBinding(nil), p.Reads...)
	sort.Slice(p.Keys, func(i, j int) bool { return p.Keys[i].Role < p.Keys[j].Role })
	for i, key := range p.Keys {
		if !roleName.MatchString(key.Role) || key.Secret.Kind != "Secret" || key.Secret.Name == "" || key.Secret.UID == "" || key.Key == "" || (i > 0 && p.Keys[i-1].Role == key.Role) {
			return p, fmt.Errorf("worker key mount binding is invalid")
		}
	}
	for _, read := range p.Reads {
		gv, err := schema.ParseGroupVersion(read.APIVersion)
		if err != nil || read.Resource == "" || read.Name == "" || len(read.Name) > 253 || !publicResource(gv.Group, gv.Version, read.Resource) || read.Name == "*" {
			return p, fmt.Errorf("worker prerequisite must be one named public custom resource")
		}
	}
	sort.Slice(p.Reads, func(i, j int) bool {
		a, b := p.Reads[i], p.Reads[j]
		return a.APIVersion+"/"+a.Resource+"/"+a.Name < b.APIVersion+"/"+b.Resource+"/"+b.Name
	})
	return p, nil
}

// Digest identifies the effective fixed profile independently of admitted execution policy.
func (p Profile) Digest() string {
	p.Reads = nil
	return foundation.Digest(p)
}

// Name is the only candidate name permitted for this participant incarnation.
func Name(p *api.StacksNetworkParticipant) string {
	return foundation.RuntimeName(string(p.Spec.NetworkUID), string(p.UID), string(p.Spec.Kind), p.Spec.ParticipantName, "worker")
}

// labels identifies support workloads without exposing them as fault actor Pods.
func labels(p *api.StacksNetworkParticipant) map[string]string {
	return map[string]string{"app.kubernetes.io/managed-by": "stacks-network-operator", "network.stacks.org/network-uid": string(p.Spec.NetworkUID), "network.stacks.org/participant": p.Spec.ParticipantName, "network.stacks.org/participant-uid": string(p.UID), "network.stacks.org/participant-kind": string(p.Spec.Kind), "network.stacks.org/role": "support", "network.stacks.org/workload": "stacks-worker"}
}

// metadata sets the exact participant owner on standalone worker support resources.
func metadata(p *api.StacksNetworkParticipant) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: Name(p), Namespace: p.Namespace, Labels: labels(p), OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetworkParticipant", Name: p.Name, UID: p.UID, Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(false)}}}
}

// ownedPod checks participant ownership independently of name and labels.
func ownedPod(pod *corev1.Pod, p *api.StacksNetworkParticipant) bool {
	owner := metav1.GetControllerOf(pod)
	return owner != nil && owner.APIVersion == api.GroupVersion.String() && owner.Kind == "StacksNetworkParticipant" && owner.Name == p.Name && owner.UID == p.UID && pod.Labels["network.stacks.org/network-uid"] == string(p.Spec.NetworkUID) && pod.Labels["network.stacks.org/participant-uid"] == string(p.UID)
}

// Rules grants named public observations and only this worker's execution status patch.
func Rules(p *api.StacksNetworkParticipant, profile Profile) []rbacv1.PolicyRule {
	rules := []rbacv1.PolicyRule{{APIGroups: []string{api.GroupVersion.Group}, Resources: []string{"stacksnetworks"}, ResourceNames: []string{"network"}, Verbs: []string{"get", "list", "watch"}}, {APIGroups: []string{api.GroupVersion.Group}, Resources: []string{"stacksnetworkparticipants"}, ResourceNames: []string{p.Name}, Verbs: []string{"get", "list", "watch"}}, {APIGroups: []string{api.GroupVersion.Group}, Resources: []string{"stacksnetworkparticipants/status"}, ResourceNames: []string{p.Name}, Verbs: []string{"patch"}}, {APIGroups: []string{""}, Resources: []string{"pods"}, ResourceNames: []string{Name(p)}, Verbs: []string{"get"}}}
	if p.Spec.Kind == "StacksFaucet" {
		rules = append(rules,
			rbacv1.PolicyRule{APIGroups: []string{"stacks.stacks.org"}, Resources: []string{"stacksfaucetrequests"}, Verbs: []string{"get", "list", "watch"}},
			rbacv1.PolicyRule{APIGroups: []string{"stacks.stacks.org"}, Resources: []string{"stacksfaucetrequests/status"}, Verbs: []string{"patch"}},
		)
	}
	for _, read := range profile.Reads {
		gv, _ := schema.ParseGroupVersion(read.APIVersion)
		rules = append(rules, rbacv1.PolicyRule{APIGroups: []string{gv.Group}, Resources: []string{read.Resource}, ResourceNames: []string{read.Name}, Verbs: []string{"get", "list", "watch"}})
	}
	return rules
}

// Pod renders an inactive, non-restarting worker with selected keys and no administration sidecars.
func Pod(p *api.StacksNetworkParticipant, profile Profile) (*corev1.Pod, error) {
	profile, err := profile.Normalize()
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(profile)
	if len(raw) > 48*1024 {
		return nil, fmt.Errorf("worker bootstrap profile exceeds bound")
	}
	meta := metadata(p)
	meta.Finalizers = []string{PodFinalizer}
	meta.Annotations = map[string]string{profileLabel: profile.Digest(), "network.stacks.org/worker-profile-json": string(raw)}
	pod := &corev1.Pod{ObjectMeta: meta, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, ServiceAccountName: Name(p), TerminationGracePeriodSeconds: ptr.To[int64](35), SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](65532), RunAsGroup: ptr.To[int64](65532), FSGroup: ptr.To[int64](65532), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{{Name: "worker", Image: profile.Image, Command: []string{"/stacks-worker"}, Args: []string{"--role=" + string(p.Spec.Kind), "--namespace=" + p.Namespace, "--participant=" + p.Name, "--participant-uid=" + string(p.UID), "--network-uid=" + string(p.Spec.NetworkUID), "--profile=" + string(raw)}, Env: []corev1.EnvVar{{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.name"}}}, {Name: "POD_UID", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.uid"}}}}, Resources: profile.Resources, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, VolumeMounts: []corev1.VolumeMount{{Name: "configuration", MountPath: "/configuration", ReadOnly: true}}}}, Volumes: []corev1.Volume{{Name: "configuration", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: profile.Configuration.Name}, DefaultMode: ptr.To[int32](0440)}}}}}}
	for _, key := range profile.Keys {
		name := "key-" + key.Role
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: key.Secret.Name, DefaultMode: ptr.To[int32](0440), Items: []corev1.KeyToPath{{Key: key.Key, Path: "key"}}}}})
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: name, MountPath: "/keys/" + key.Role, ReadOnly: true})
	}
	if profile.Placement != nil {
		pod.Spec.NodeSelector = profile.Placement.NodeSelector
		if profile.Placement.Tolerations != nil {
			pod.Spec.Tolerations = *profile.Placement.Tolerations
		}
		if ptr.Deref(profile.Placement.SpreadAcrossNodes, false) {
			pod.Spec.Affinity = &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{Weight: 100, PodAffinityTerm: corev1.PodAffinityTerm{TopologyKey: "kubernetes.io/hostname", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"network.stacks.org/network-uid": string(p.Spec.NetworkUID), "network.stacks.org/participant-kind": string(p.Spec.Kind), "network.stacks.org/workload": "stacks-worker"}}}}}}}
		}
	}
	return pod, nil
}

// safeRule constrains retained reads to the same public resource vocabulary as fresh grants.
func safeRule(p *api.StacksNetworkParticipant, rule rbacv1.PolicyRule) bool {
	for _, base := range Rules(p, Profile{}) {
		if reflect.DeepEqual(base, rule) {
			return true
		}
	}
	if len(rule.APIGroups) != 1 || len(rule.Resources) != 1 || len(rule.ResourceNames) != 1 || rule.ResourceNames[0] == "" || rule.ResourceNames[0] == "*" || !reflect.DeepEqual(rule.Verbs, []string{"get", "list", "watch"}) || len(rule.NonResourceURLs) > 0 {
		return false
	}
	group, resource := rule.APIGroups[0], rule.Resources[0]
	return publicResource(group, "v1alpha2", resource)
}

// podBinding captures the bounded worker identity retained by the root ledger.
func podBinding(pod *corev1.Pod) api.WorkerPodBinding {
	return api.WorkerPodBinding{Kind: "Pod", Name: pod.Name, UID: pod.UID}
}

// publicResource is the explicit worker read vocabulary; wildcard groups cannot reach Secrets.
func publicResource(group, version, resource string) bool {
	if version != "v1alpha2" {
		return false
	}
	switch group {
	case api.GroupVersion.Group:
		return resource == "stacksgeneses" || resource == "stacksnetworkparticipants"
	case "stacks.stacks.org":
		return resource == "stacksaccounts" || resource == "stackssigners" || resource == "stackscontractsets" || resource == "stacksstackers" || resource == "stacksfaucets" || resource == "stackstransactionproductions"
	}
	return false
}
