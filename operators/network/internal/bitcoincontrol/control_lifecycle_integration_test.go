//go:build integration

package bitcoincontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// controlIntegrationClient supplies a real API server; no kubelet or workload controller is simulated.
func controlIntegrationClient(t *testing.T) client.WithWatch {
	t.Helper()
	environment := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator", "crds")}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.37.0", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest")}
	config, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, api.AddToScheme, bitcoin.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	c, err := client.NewWithWatch(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestControlAPIStatusAndLostAcknowledgement(t *testing.T) {
	c := controlIntegrationClient(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "control-lifecycle"}}))
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "control-lifecycle"}, Spec: api.StacksNetworkSpec{Operation: "Stopped"}}
	must(c.Create(ctx, root))
	configuration := api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{}}
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "core", Namespace: root.Namespace}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: root.UID, ParticipantName: "core", Kind: "BitcoinNode", Configuration: configuration}}
	must(controllerutil.SetControllerReference(root, p, c.Scheme()))
	must(c.Create(ctx, p))
	admission := &api.Admission{Configuration: configuration, PolicyDigest: foundation.Digest(configuration)}
	must(participantstatus.Apply(ctx, c, p, api.ParticipantStatus{Admission: admission, Conditions: []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue, Reason: "Admitted", Message: "Admitted", LastTransitionTime: metav1.Now()}}}, participantstatus.AggregateManager))
	must(participantstatus.Apply(ctx, c, p, api.ParticipantStatus{Runtime: &api.ParticipantRuntimeStatus{ObservedGeneration: p.Generation, PolicyDigest: admission.PolicyDigest}}, "stacks-network-domain-bitcoinnode"))
	selector := labels(p, "support")
	selector["network.stacks.org/worker-role"] = "bitcoin-control"
	template := corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: selector, Finalizers: []string{ControlPodFinalizer}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "control", Image: "control:test"}}}}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: controlDeploymentName(p), Namespace: p.Namespace}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](0), Selector: &metav1.LabelSelector{MatchLabels: selector}, Template: template}}
	must(controllerutil.SetControllerReference(p, deployment, c.Scheme()))
	must(c.Create(ctx, deployment))
	deployment.Status.ObservedGeneration = deployment.Generation
	must(c.Status().Update(ctx, deployment))
	replica := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "control-replica", Namespace: p.Namespace, Labels: selector}, Spec: appsv1.ReplicaSetSpec{Replicas: ptr.To[int32](0), Selector: &metav1.LabelSelector{MatchLabels: selector}, Template: template}}
	must(controllerutil.SetControllerReference(deployment, replica, c.Scheme()))
	must(c.Create(ctx, replica))
	replica.Status.ObservedGeneration = replica.Generation
	must(c.Status().Update(ctx, replica))
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "control-pod", Namespace: p.Namespace, Labels: selector, Finalizers: template.Finalizers}, Spec: *template.Spec.DeepCopy()}
	pod.Spec.NodeName = "node"
	must(controllerutil.SetControllerReference(replica, pod, c.Scheme()))
	must(c.Create(ctx, pod))
	r := WorkloadReconciler{Client: c, Reader: c}
	_, err := r.ReconcileControlLifecycle(ctx, p, root)
	must(err)
	if p.Status.BitcoinControl.Terminated {
		t.Fatal("unconfirmed Pod claimed stopped")
	}
	must(c.Delete(ctx, p))
	must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
	if !controllerutil.ContainsFinalizer(p, ControlParticipantFinalizer) {
		t.Fatal("participant disappeared before control process termination")
	}
	pod.Status.Phase = corev1.PodFailed
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "control", Image: "control:test", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1}}}}
	must(c.Status().Update(ctx, pod))
	must(c.Delete(ctx, pod))
	must(c.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	if !controllerutil.ContainsFinalizer(pod, ControlPodFinalizer) {
		t.Fatal("Pod disappeared before evidence publication")
	}
	loseAck := true
	r.Client = interceptor.NewClient(c, interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
		if err := c.SubResource(sub).Patch(ctx, obj, patch, opts...); err != nil {
			return err
		}
		if loseAck {
			loseAck = false
			return fmt.Errorf("injected lost committed SSA response")
		}
		return nil
	}})
	if _, err := r.ReconcileControlLifecycle(ctx, p, root); err == nil {
		t.Fatal("lost acknowledgement not injected")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	if !controllerutil.ContainsFinalizer(pod, ControlPodFinalizer) {
		t.Fatal("unacknowledged write released retained Pod")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
	if !p.Status.BitcoinControl.Terminated || p.Status.Admission.PolicyDigest != admission.PolicyDigest || p.Status.Runtime.PolicyDigest != admission.PolicyDigest || !meta.IsStatusConditionTrue(p.Status.Conditions, "Resolved") {
		t.Fatal("control SSA changed unrelated status or lost committed evidence")
	}
	owned := false
	for _, entry := range p.ManagedFields {
		if entry.Manager != ControlLifecycleManager {
			continue
		}
		var fields map[string]map[string]json.RawMessage
		must(json.Unmarshal(entry.FieldsV1.Raw, &fields))
		status := fields["f:status"]
		if entry.Subresource != "status" || len(status) != 1 || status["f:bitcoinControl"] == nil {
			t.Fatal("control SSA owns unrelated fields", string(entry.FieldsV1.Raw))
		}
		owned = true
	}
	if !owned {
		t.Fatal("fixed control manager missing")
	}
	_, err = r.ReconcileControlLifecycle(ctx, p, root)
	must(err)
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), pod); err == nil && controllerutil.ContainsFinalizer(pod, ControlPodFinalizer) {
		t.Fatal("fresh retry did not release acknowledged terminal Pod")
	}
}
