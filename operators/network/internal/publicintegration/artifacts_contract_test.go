//go:build live

package publicintegration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// readOnlyEvidenceClient deliberately provides no mutation methods to the harness.
type readOnlyEvidenceClient struct {
	client.Client
	reader client.Reader
}

func (c readOnlyEvidenceClient) Get(ctx context.Context, key client.ObjectKey, out client.Object, opts ...client.GetOption) error {
	return c.reader.Get(ctx, key, out, opts...)
}
func (c readOnlyEvidenceClient) List(ctx context.Context, out client.ObjectList, opts ...client.ListOption) error {
	return c.reader.List(ctx, out, opts...)
}

func TestOperatorArtifactRecordsOwnedRuntimeWithoutMutation(t *testing.T) {
	for _, mode := range []string{"valid", "replaced-deployment", "foreign-rs", "foreign-pod", "missing-image", "pending-pod", "missing-status"} {
		t.Run(mode, func(t *testing.T) {
			dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "operator", Namespace: "system", UID: "dep"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Image: "operator:new", Env: []corev1.EnvVar{{Name: "PRIVATE", Value: "must-not-record"}}}}}}}}
			rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "rs", Namespace: "system", UID: "rs", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(dep, appsv1.SchemeGroupVersion.WithKind("Deployment"))}}}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "system", UID: "pod", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(rs, appsv1.SchemeGroupVersion.WithKind("ReplicaSet"))}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Image: "operator:old"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "manager", ImageID: "runtime://sha256:actual", ContainerID: "containerd://process", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
			switch mode {
			case "replaced-deployment":
				dep.UID = "replacement"
			case "foreign-rs":
				rs.OwnerReferences[0].UID = "foreign"
			case "foreign-pod":
				pod.OwnerReferences[0].UID = "foreign"
			case "missing-image":
				pod.Status.ContainerStatuses[0].ImageID = ""
			case "pending-pod":
				pod.Status.Phase = corev1.PodPending
			case "missing-status":
				pod.Status.ContainerStatuses = nil
			}
			scheme := runtime.NewScheme()
			_ = appsv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dep, rs, pod).Build()
			h := harness{c: readOnlyEvidenceClient{reader: c}, config: liveConfig{operatorNamespace: "system", operatorName: "operator", operatorUID: "dep"}, evidence: t.TempDir()}
			err := h.recordOperator(context.Background())
			if mode != "valid" {
				if err == nil {
					t.Fatal("incomplete or foreign evidence accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(h.evidence, "events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				Details struct {
					Requested map[string]string `json:"requestedImages"`
					Pods      []operatorPod     `json:"pods"`
				}
			}
			if err := json.Unmarshal(data, &event); err != nil {
				t.Fatal(err)
			}
			if event.Details.Requested["manager"] != "operator:new" || len(event.Details.Pods) != 1 || event.Details.Pods[0].Containers[0].Image != "operator:old" || event.Details.Pods[0].Containers[0].ImageID != "runtime://sha256:actual" || strings.Contains(string(data), "must-not-record") {
				t.Fatalf("incorrect public image evidence: %s", data)
			}
		})
	}
}

func TestGenesisArtifactPinsIdentityAndRecordsFrozenGatesOnce(t *testing.T) {
	for _, mode := range []string{"valid", "replaced", "foreign-root", "foreign-owner"} {
		t.Run(mode, func(t *testing.T) {
			root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "lab", UID: "network"}, Status: api.StacksNetworkStatus{GenesisRef: &common.Binding{Name: "genesis", UID: "genesis"}}}
			genesis := &api.StacksGenesis{ObjectMeta: metav1.ObjectMeta{Name: "genesis", Namespace: "lab", UID: "genesis", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(root, api.GroupVersion.WithKind("StacksNetwork"))}}, Spec: api.StacksGenesisSpec{Source: api.GenesisSource{NetworkUID: root.UID}, Bootstrap: api.Bootstrap{Gates: []api.Gate{{Name: "EnrollPoX5", BitcoinCeiling: 294, TargetCycle: ptr.To(int64(15))}}}}}
			switch mode {
			case "replaced":
				genesis.UID = "other"
			case "foreign-root":
				genesis.Spec.Source.NetworkUID = "other"
			case "foreign-owner":
				genesis.OwnerReferences[0].UID = "other"
			}
			scheme := runtime.NewScheme()
			_ = api.AddToScheme(scheme)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(genesis).Build()
			h := harness{c: readOnlyEvidenceClient{reader: c}, evidence: t.TempDir()}
			err := h.recordGenesis(context.Background(), root)
			if mode != "valid" {
				if err == nil {
					t.Fatal("foreign genesis accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := h.recordGenesis(context.Background(), root); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(h.evidence, "events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				Details struct {
					Gates []api.Gate `json:"gates"`
				}
			}
			if err := json.Unmarshal(data, &event); err != nil {
				t.Fatal(err)
			}
			if len(event.Details.Gates) != 1 || event.Details.Gates[0].BitcoinCeiling != 294 || *event.Details.Gates[0].TargetCycle != 15 || strings.Count(string(data), "\n") != 1 {
				t.Fatalf("incorrect frozen gates: %s", data)
			}
		})
	}
}

func TestOperatorEvidenceSelectionDoesNotEnableRestart(t *testing.T) {
	for key, value := range map[string]string{"KUBECONFIG": "selected", "CONTEXT": "kind-test", "NAMESPACE": "fresh", "BITCOIN_IMAGE": "bitcoin:test", "STACKS_IMAGE": "stacks:test", "OPERATOR_NAMESPACE": "system", "OPERATOR_NAME": "operator", "OPERATOR_UID": "dep", "OPERATOR_RESTART": ""} {
		t.Setenv("STACKS_PUBLIC_"+key, value)
	}
	c, err := readConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.restartOperatorEnabled {
		t.Fatal("evidence selection enabled mutation")
	}
	t.Setenv("STACKS_PUBLIC_OPERATOR_RESTART", "1")
	c, err = readConfig()
	if err != nil || !c.restartOperatorEnabled {
		t.Fatal("explicit restart not selected", err)
	}
	t.Setenv("STACKS_PUBLIC_OPERATOR_UID", "")
	if _, err := readConfig(); err == nil {
		t.Fatal("unpinned operator accepted")
	}
}
