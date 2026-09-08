// Package workers reconciles capability-owned execution Deployments and their scoped identities.
package workers

import (
	"fmt"
	"sort"
	"strconv"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Settings contains installation defaults shared by independently owned workers.
type Settings struct {
	// Image runs Bitcoin execution and receipt collection; SDKImage adds offline Stacks adapters.
	Image, SDKImage string
	// PullPolicy selects Kubernetes image fetching behavior.
	PullPolicy core.PullPolicy
	// PullSecrets names registry credentials provisioned separately in each network namespace.
	PullSecrets []core.LocalObjectReference
	// Generation and Reorganization enable the existing bounded RPC action surfaces.
	Generation, Reorganization bool
}

// plan describes one workload without containing private key bytes.
type plan struct {
	owner                          client.Object
	component, network, networkUID string
	credentials                    string
	keys                           []stacks.ArtifactReference
	accounts                       []string
}

// Name is stable for a capability and distinct across execution components.
func Name(owner client.Object, component string) string {
	return naming.Child(owner.GetName(), component)
}

// deployment renders a single bound executor; rollout strategy is not dispatch fencing.
func deployment(p plan, settings Settings) *apps.Deployment {
	name := Name(p.owner, p.component)
	labels := map[string]string{"app.kubernetes.io/name": p.component, "network.stacks.org/network": p.network, "network.stacks.org/network-uid": p.networkUID, "network.stacks.org/capability-uid": string(p.owner.GetUID())}
	one := int32(1)
	yes, no := true, false
	uid, grace := int64(65532), int64(45)
	defaultMode := int32(core.SecretVolumeSourceDefaultMode)
	image := settings.Image
	if p.component == "stacks-transactions" || p.component == "stacks-contracts" || p.component == "stacks-stacking" {
		image = settings.SDKImage
	}
	args := []string{"--component=" + p.component, "--watch-namespace=" + p.owner.GetNamespace(), "--capability-name=" + p.owner.GetName(), "--capability-uid=" + string(p.owner.GetUID()), "--network-uid=" + p.networkUID, "--leader-namespace=" + p.owner.GetNamespace(), "--leader-elect=true"}
	if p.component == "bitcoin-production" {
		args = append(args, "--bitcoin-generation-enabled="+strconv.FormatBool(settings.Generation), "--bitcoin-reorganization-enabled="+strconv.FormatBool(settings.Reorganization))
	}
	container := core.Container{Name: "manager", Image: image, ImagePullPolicy: settings.PullPolicy, Args: args,
		Ports:           []core.ContainerPort{{Name: "health", ContainerPort: 8081}, {Name: "metrics", ContainerPort: 8080}},
		SecurityContext: &core.SecurityContext{AllowPrivilegeEscalation: &no, ReadOnlyRootFilesystem: &yes, Capabilities: &core.Capabilities{Drop: []core.Capability{"ALL"}}},
		Resources:       core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("25m"), core.ResourceMemory: resource.MustParse("96Mi")}, Limits: core.ResourceList{core.ResourceCPU: resource.MustParse("1"), core.ResourceMemory: resource.MustParse("512Mi")}},
		ReadinessProbe:  &core.Probe{ProbeHandler: core.ProbeHandler{HTTPGet: &core.HTTPGetAction{Path: "/readyz", Port: intstr.FromString("health")}}},
		LivenessProbe:   &core.Probe{ProbeHandler: core.ProbeHandler{HTTPGet: &core.HTTPGetAction{Path: "/healthz", Port: intstr.FromString("health")}}},
	}
	pod := core.PodSpec{ServiceAccountName: name, AutomountServiceAccountToken: &yes, TerminationGracePeriodSeconds: &grace, SecurityContext: &core.PodSecurityContext{RunAsNonRoot: &yes, RunAsUser: &uid, RunAsGroup: &uid, SeccompProfile: &core.SeccompProfile{Type: core.SeccompProfileTypeRuntimeDefault}}}
	if p.credentials != "" {
		key, path := "credentials.json", "/etc/bitcoin-production"
		if p.component == "stacks-transactions" {
			key, path = "account.json", "/etc/stacks-transactions"
		}
		pod.Volumes = append(pod.Volumes, core.Volume{Name: "credentials", VolumeSource: core.VolumeSource{Secret: &core.SecretVolumeSource{SecretName: p.credentials, DefaultMode: &defaultMode, Items: []core.KeyToPath{{Key: key, Path: key}}}}})
		container.VolumeMounts = append(container.VolumeMounts, core.VolumeMount{Name: "credentials", MountPath: path, ReadOnly: true})
	}
	grouped := map[string]map[string]bool{}
	for _, ref := range p.keys {
		if grouped[ref.Name] == nil {
			grouped[ref.Name] = map[string]bool{}
		}
		grouped[ref.Name][ref.Key] = true
	}
	names := []string{}
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		keys := []string{}
		for key := range grouped[name] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		items := []core.KeyToPath{}
		for _, key := range keys {
			items = append(items, core.KeyToPath{Key: key, Path: key})
		}
		volume := fmt.Sprintf("key-%d", i)
		pod.Volumes = append(pod.Volumes, core.Volume{Name: volume, VolumeSource: core.VolumeSource{Secret: &core.SecretVolumeSource{SecretName: name, DefaultMode: &defaultMode, Items: items}}})
		container.VolumeMounts = append(container.VolumeMounts, core.VolumeMount{Name: volume, MountPath: "/etc/stacks-keys/" + name, ReadOnly: true})
	}
	if p.component == "stacks-receipts" {
		container.Ports = append(container.Ports, core.ContainerPort{Name: "receipts", ContainerPort: 8082})
	}
	pod.ImagePullSecrets = append([]core.LocalObjectReference(nil), settings.PullSecrets...)
	pod.Containers = []core.Container{container}
	return &apps.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.owner.GetNamespace()}, Spec: apps.DeploymentSpec{Replicas: &one, Strategy: apps.DeploymentStrategy{Type: apps.RecreateDeploymentStrategyType}, Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: core.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: pod}}}
}

// rules grants namespace reads but restricts capability/account writes to the assigned names.
func rules(p plan, settings Settings) ([]rbac.PolicyRule, error) {
	if p.component == "stacks-contracts" || p.component == "stacks-stacking" || p.component == "stacks-receipts" {
		if len(p.accounts) == 0 {
			return nil, fmt.Errorf("managed worker requires named account permissions")
		}
		for _, name := range p.accounts {
			if name == "" {
				return nil, fmt.Errorf("managed worker account name is empty")
			}
		}
	}
	rule := func(group string, resources, verbs, names []string) rbac.PolicyRule {
		return rbac.PolicyRule{APIGroups: []string{group}, Resources: resources, Verbs: verbs, ResourceNames: names}
	}
	read := []string{"get", "list", "watch"}
	get := []string{"get"}
	patch := []string{"get", "patch"}
	result := []rbac.PolicyRule{
		rule("network.stacks.org", []string{"stacksnetworks", "bitcoinnodes", "stacksnodes"}, read, nil),
		rule("", []string{"pods"}, get, nil), rule("", []string{"configmaps"}, []string{"get", "list"}, nil), rule("apps", []string{"statefulsets"}, get, nil),
		rule("coordination.k8s.io", []string{"leases"}, []string{"create"}, nil),
		rule("coordination.k8s.io", []string{"leases"}, []string{"get", "update"}, []string{"execution-" + string(p.owner.GetUID())}),
	}
	switch p.component {
	case "bitcoin-production":
		result = append(result, rule("bitcoin.stacks.org", []string{"bitcoinblockproductions", "bitcoinproductiontargets"}, read, nil), rule("bitcoin.stacks.org", []string{"bitcoinproductiontargets", "bitcoinproductiontargets/status"}, patch, []string{p.owner.GetName()}), rule("stacks.stacks.org", []string{"stacksaccounts", "stackscontractsets", "stacksstackingparticipants"}, get, nil))
		if settings.Generation {
			result = append(result, rule("actions.stacks.org", []string{"bitcoinblockgenerations"}, read, nil))
		}
		if settings.Reorganization {
			result = append(result, rule("actions.stacks.org", []string{"bitcoinreorganizations"}, read, nil))
		}
	case "stacks-transactions":
		result = append(result, rule("stacks.stacks.org", []string{"stackstransactionproductions"}, read, nil), rule("stacks.stacks.org", []string{"stackstransactionproductions", "stackstransactionproductions/status"}, patch, []string{p.owner.GetName()}))
	default:
		result = append(result, rule("stacks.stacks.org", []string{"stacksaccounts", "stackscontractsets", "stacksstackingparticipants"}, read, nil))
		if p.component == "stacks-receipts" {
			// The receipt sink can only record existing authorizations; it holds no signing keys.
			result = append(result, rule("stacks.stacks.org", []string{"stacksaccounts/status"}, patch, p.accounts))
		} else {
			kind := "stackscontractsets"
			if p.component == "stacks-stacking" {
				kind = "stacksstackingparticipants"
			}
			result = append(result, rule("stacks.stacks.org", []string{kind + "/status"}, patch, []string{p.owner.GetName()}), rule("stacks.stacks.org", []string{"stacksaccounts", "stacksaccounts/status"}, patch, p.accounts))
		}
	}
	return result, nil
}
