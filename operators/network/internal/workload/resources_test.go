package workload

import (
	"context"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	operatorlabels "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/labels"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/leaf"
)

func TestRenderedPodLabelsMapToEveryLeafController(t *testing.T) {
	tests := []struct {
		kind  string
		label operatorlabels.ActorKind
		owner client.Object
	}{
		{kind: "BitcoinNode", label: operatorlabels.Bitcoin, owner: &networkv1alpha1.BitcoinNode{}},
		{kind: "StacksNode", label: operatorlabels.StacksNode, owner: &networkv1alpha1.StacksNode{}},
		{kind: "StacksSigner", label: operatorlabels.StacksSigner, owner: &networkv1alpha1.StacksSigner{}},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			test.owner.SetName("network-actor")
			test.owner.SetNamespace("test")
			result, err := render(Descriptor{
				Owner: test.owner, Kind: test.kind, Network: "network", Actor: "actor", Image: "actor:test",
				Config: networkv1alpha1.ConfigSource{Inline: &networkv1alpha1.InlineConfig{Data: "configuration"}},
			}, func(metav1.Object) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Labels: result.statefulSet.Spec.Template.Labels}}
			requests := leaf.PodRequest(test.label)(context.Background(), pod)
			if len(requests) != 1 || requests[0].Name != test.owner.GetName() || requests[0].Namespace != test.owner.GetNamespace() {
				t.Fatalf("requests = %#v, labels = %#v", requests, pod.Labels)
			}
		})
	}
}

func TestRenderCreatesRestrictedWorkloadWithoutServiceAccountCredentials(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := networkv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	owner := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "net-follower", Namespace: "test", UID: "actor-uid"}}
	descriptor := Descriptor{Owner: owner, Kind: "StacksNode", Network: "net", Actor: "follower", Role: "follower", Image: "stacks:test", ImagePullPolicy: corev1.PullIfNotPresent,
		Config: networkv1alpha1.ConfigSource{Inline: &networkv1alpha1.InlineConfig{Data: "[node]\n", Key: "config.toml"}}, ServiceMap: map[string]string{"bitcoin": "net-bitcoin"},
		Command: []string{"stacks-node", "start", "--config", "/tmp/stacks-network-config/config.toml"}, Ports: []networkv1alpha1.PortSpec{{Name: "rpc", Port: 20443}},
		DependencyImage: "busybox:test", Workload: networkv1alpha1.WorkloadSpec{Storage: &networkv1alpha1.StorageSpec{Size: "2Gi"}, Resources: &networkv1alpha1.ResourceSpec{Requests: networkv1alpha1.ResourceValues{CPU: "100m", Memory: "128Mi"}}}}
	resources, err := render(descriptor, func(object metav1.Object) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	pod := resources.statefulSet.Spec.Template.Spec
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("service-account token is mounted")
	}
	container := pod.Containers[0]
	if container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("privilege escalation is not disabled")
	}
	if len(container.SecurityContext.Capabilities.Drop) != 1 || container.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("capabilities = %#v", container.SecurityContext.Capabilities)
	}
	if len(resources.statefulSet.Spec.VolumeClaimTemplates) != 1 || resources.statefulSet.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage().String() != "2Gi" {
		t.Fatal("storage policy was not rendered")
	}
	if resources.configMap == nil || resources.configMap.Data["config.toml"] != "[node]\n" {
		t.Fatal("inline config was not rendered")
	}
	if resources.statefulSet.Spec.Template.Annotations[configDigestAnnotation] == "" {
		t.Fatal("config digest annotation is absent")
	}
	if len(resources.service.Spec.Selector) != 4 || len(resources.statefulSet.Spec.Selector.MatchLabels) != 4 {
		t.Fatalf("identity selectors are not fully scoped: service=%v statefulSet=%v", resources.service.Spec.Selector, resources.statefulSet.Spec.Selector.MatchLabels)
	}
}

func TestRenderRejectsInvalidResourceQuantity(t *testing.T) {
	owner := &networkv1alpha1.BitcoinNode{ObjectMeta: metav1.ObjectMeta{Name: "net-bitcoin", Namespace: "test"}}
	_, err := render(Descriptor{
		Owner: owner, Kind: "BitcoinNode", Network: "net", Actor: "bitcoin", Image: "bitcoin:test",
		Config:   networkv1alpha1.ConfigSource{Inline: &networkv1alpha1.InlineConfig{Data: "regtest=1"}},
		Workload: networkv1alpha1.WorkloadSpec{Resources: &networkv1alpha1.ResourceSpec{Requests: networkv1alpha1.ResourceValues{CPU: "not-a-quantity"}}},
	}, func(object metav1.Object) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "resources") {
		t.Fatalf("error = %v", err)
	}
}

func TestVolumeClaimCompatibilityAllowsAPIDefaultsAndRejectsStorageChange(t *testing.T) {
	desired := []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "data"}, Spec: corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
	}}}
	current := desired[0].DeepCopy()
	mode := corev1.PersistentVolumeFilesystem
	current.Spec.VolumeMode = &mode
	if !volumeClaimTemplatesCompatible([]corev1.PersistentVolumeClaim{*current}, desired) {
		t.Fatal("API-defaulted volume claim was classified as an immutable change")
	}
	current.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
	if volumeClaimTemplatesCompatible([]corev1.PersistentVolumeClaim{*current}, desired) {
		t.Fatal("changed access mode was classified as compatible")
	}
}

func TestRenderExternalSecretDoesNotRequireSecretRead(t *testing.T) {
	owner := &networkv1alpha1.StacksSigner{ObjectMeta: metav1.ObjectMeta{Name: "net-signer", Namespace: "test", UID: "actor-uid"}}
	descriptor := Descriptor{Owner: owner, Kind: "StacksSigner", Network: "net", Actor: "signer", Role: "signer", Image: "signer:test", ImagePullPolicy: corev1.PullIfNotPresent,
		Config:  networkv1alpha1.ConfigSource{SecretRef: &networkv1alpha1.ConfigObjectRef{Name: "signer-secret", Key: "signer.toml", ExpectedDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		Command: []string{"stacks-signer"}, Ports: []networkv1alpha1.PortSpec{{Name: "events", Port: 30000}}, DependencyImage: "busybox:test"}
	resources, err := render(descriptor, func(object metav1.Object) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if resources.configMap != nil {
		t.Fatal("operator copied secret data into a ConfigMap")
	}
	secret := resources.statefulSet.Spec.Template.Spec.Volumes[0].Secret
	if secret == nil || secret.SecretName != "signer-secret" {
		t.Fatalf("secret volume = %#v", secret)
	}
	if len(resources.statefulSet.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatal("expected digest verifier was not rendered")
	}
	for _, volume := range resources.statefulSet.Spec.Template.Spec.Volumes {
		if volume.Name != "rendered-config" {
			continue
		}
		if volume.EmptyDir == nil || volume.EmptyDir.Medium != corev1.StorageMediumMemory || volume.EmptyDir.SizeLimit == nil || volume.EmptyDir.SizeLimit.String() != "2Mi" {
			t.Fatalf("rendered configuration storage = %#v", volume.EmptyDir)
		}
		return
	}
	t.Fatal("rendered configuration volume is absent")
}

func TestRenderContainerOverrideIsDeterministicAndPreservesStacksConfigWrapper(t *testing.T) {
	owner := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "net-follower", Namespace: "test", UID: "actor-uid"}}
	descriptor := Descriptor{Owner: owner, Kind: "StacksNode", Network: "net", Actor: "follower", Role: "follower", Image: "stacks:test",
		Config:  networkv1alpha1.ConfigSource{Inline: &networkv1alpha1.InlineConfig{Data: "[node]\n", Key: "config.toml"}},
		Command: []string{"stacks-node", "start"}, Args: []string{"--config", "/tmp/stacks-network-config/config.toml"},
		Container: &networkv1alpha1.ContainerOverride{Command: []string{"custom-stacks-node"}, Args: []string{"serve"}, Env: map[string]string{
			"ZETA": "6", "ALPHA": "1", "DELTA": "4", "BRAVO": "2", "ECHO": "5", "CHARLIE": "3",
		}},
	}
	first, err := render(descriptor, func(metav1.Object) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	second, err := render(descriptor, func(metav1.Object) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	firstContainer := first.statefulSet.Spec.Template.Spec.Containers[0]
	secondContainer := second.statefulSet.Spec.Template.Spec.Containers[0]
	if !reflect.DeepEqual(firstContainer.Env, secondContainer.Env) {
		t.Fatalf("environment order changed: first=%v second=%v", firstContainer.Env, secondContainer.Env)
	}
	if got := firstContainer.Command; len(got) != 5 || got[0] != "/bin/bash" || got[3] != "--" || got[4] != "custom-stacks-node" {
		t.Fatalf("wrapped command = %#v", got)
	}
	if !reflect.DeepEqual(firstContainer.Args, []string{"serve"}) {
		t.Fatalf("arguments = %#v", firstContainer.Args)
	}
	names := make([]string, 0, 6)
	for _, variable := range firstContainer.Env[len(firstContainer.Env)-6:] {
		names = append(names, variable.Name)
	}
	if !reflect.DeepEqual(names, []string{"ALPHA", "BRAVO", "CHARLIE", "DELTA", "ECHO", "ZETA"}) {
		t.Fatalf("custom environment order = %#v", names)
	}
}

func TestRenderRejectsReservedContainerEnvironment(t *testing.T) {
	for name := range reservedEnvironment {
		t.Run(name, func(t *testing.T) {
			owner := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "net-follower", Namespace: "test", UID: "actor-uid"}}
			_, err := render(Descriptor{Owner: owner, Kind: "StacksNode", Network: "net", Actor: "follower", Image: "stacks:test",
				Config: networkv1alpha1.ConfigSource{Inline: &networkv1alpha1.InlineConfig{Data: "[node]\n"}}, Command: []string{"stacks-node"},
				Container: &networkv1alpha1.ContainerOverride{Env: map[string]string{name: "forged"}},
			}, func(metav1.Object) error { return nil })
			if err == nil || !strings.Contains(err.Error(), "reserved by the operator") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
