//go:build integration

// Package integration tests the rendered profile against the upstream API schema.
package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/tools/chart-policy/internal/chaosprofile"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestNativeFaultAdmission exercises each profile against real admission and RBAC.
func TestNativeFaultAdmission(t *testing.T) {
	for _, profile := range []struct {
		name             string
		delay, partition bool
	}{
		{"delay", true, false}, {"partition", false, true}, {"combined", true, true},
	} {
		t.Run(profile.name, func(t *testing.T) { testNativeFaultAdmission(t, profile.delay, profile.partition) })
	}
}

// testNativeFaultAdmission shares lifecycle and selector checks across enabled mechanisms.
func testNativeFaultAdmission(t *testing.T, delay, partition bool) {
	ctx := context.Background()
	schema, err := chaosprofile.Schema(ctx)
	if err != nil {
		t.Fatal(err)
	}
	environment := &envtest.Environment{CRDDirectoryPaths: []string{schema}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.36", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest")}
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
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	admin, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "profile-test", Labels: map[string]string{"network.stacks.org/chaos-profile": "network-faults-v1"}, Annotations: map[string]string{"chaos-mesh.org/inject": "enabled"}}}
	if err := admin.Create(ctx, ns); err != nil {
		t.Fatal(err)
	}
	data, err := exec.Command("helm", "template", "test", "../../../../charts/stacks-chaos-profile", "--namespace", ns.Name, "--set", "networkDelay.enabled="+strconv.FormatBool(delay), "--set", "networkPartition.enabled="+strconv.FormatBool(partition), "--set", "chaosMesh.externalVersion="+chaosprofile.Version).CombinedOutput()
	if err != nil {
		t.Fatalf("render: %s: %v", data, err)
	}
	objects, err := chaosprofile.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := chaosprofile.Validate(objects); err != nil {
		t.Fatal(err)
	}
	mechanism := "delay"
	if !delay {
		mechanism = "partition"
	}
	raw, err := os.ReadFile("../../../../examples/chaos/network-" + mechanism + ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	faults, err := chaosprofile.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	valid := faults[0]
	valid.SetNamespace(ns.Name)
	for _, path := range [][]string{{"spec", "selector", "namespaces"}, {"spec", "target", "selector", "namespaces"}} {
		if err := unstructured.SetNestedStringSlice(valid.Object, []string{ns.Name}, path...); err != nil {
			t.Fatal(err)
		}
	}
	// Seed valid upstream resources before the profile exists. No Chaos controller
	// runs in envtest; subsequent writes model its API-level cleanup obligations.
	legacyDelay := valid.DeepCopy()
	legacyDelay.SetName("legacy-delay")
	_ = unstructured.SetNestedField(legacyDelay.Object, "delay", "spec", "action")
	_ = unstructured.SetNestedField(legacyDelay.Object, "all", "spec", "mode")
	_ = unstructured.SetNestedField(legacyDelay.Object, "both", "spec", "direction")
	_ = unstructured.SetNestedField(legacyDelay.Object, "5m", "spec", "duration")
	_ = unstructured.SetNestedField(legacyDelay.Object, "2s", "spec", "delay", "latency")
	_ = unstructured.SetNestedField(legacyDelay.Object, "10ms", "spec", "delay", "jitter")
	legacyPartition := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "chaos-mesh.org/v1alpha1", "kind": "NetworkChaos",
		"spec": map[string]any{"action": "partition", "mode": "all", "direction": "both", "duration": "5m",
			"selector": map[string]any{"namespaces": []any{ns.Name}}, "externalTargets": []any{"192.0.2.1"}},
	}}
	legacyPartition.SetName("legacy-partition")
	legacyPartition.SetNamespace(ns.Name)
	legacyFaults := []*unstructured.Unstructured{legacyDelay, legacyPartition}
	for _, o := range legacyFaults {
		o.SetFinalizers([]string{"chaos-mesh/records"})
		if err := admin.Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range objects {
		// Envtest has no quota controller. The rendered quota is checked above and enforced in live qualification.
		if o.GetKind() == "ResourceQuota" {
			continue
		}
		if err := admin.Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	bad := valid.DeepCopy()
	_ = unstructured.SetNestedField(bad.Object, "all", "spec", "mode")
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := admin.Create(ctx, bad.DeepCopy(), client.DryRunAll)
		if policyDenied(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("policy did not activate: %#v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, o := range legacyFaults {
		t.Run(o.GetName()+"-cleanup", func(t *testing.T) {
			changed := o.DeepCopy()
			_ = unstructured.SetNestedField(changed.Object, "30s", "spec", "duration")
			if err := admin.Update(ctx, changed); !policyDenied(err) {
				t.Fatalf("legacy spec mutation accepted: %v", err)
			}
			// New objects with these same unsupported specs still cannot be admitted.
			fresh := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": o.GetAPIVersion(), "kind": o.GetKind(), "spec": runtime.DeepCopyJSONValue(o.Object["spec"]),
			}}
			fresh.SetName(o.GetName() + "-new")
			fresh.SetNamespace(ns.Name)
			fresh.SetLabels(o.GetLabels())
			if err := admin.Create(ctx, fresh, client.DryRunAll); !policyDenied(err) {
				t.Fatalf("unsupported creation accepted: %v", err)
			}
			o.Object["status"] = map[string]any{"experiment": map[string]any{"desiredPhase": "Stop"}, "conditions": []any{map[string]any{"type": "AllRecovered", "status": "True"}}}
			if err := admin.Update(ctx, o); err != nil {
				t.Fatalf("legacy cleanup status rejected: %v", err)
			}
			if err := admin.Delete(ctx, o); err != nil {
				t.Fatal(err)
			}
			if err := admin.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
				t.Fatal(err)
			}
			phase, _, _ := unstructured.NestedString(o.Object, "status", "experiment", "desiredPhase")
			if phase != "Stop" || o.GetDeletionTimestamp() == nil || len(o.GetFinalizers()) != 1 {
				t.Fatal("cleanup status or finalized deletion intent was not retained")
			}
			o.SetFinalizers(nil)
			if err := admin.Update(ctx, o); err != nil {
				t.Fatalf("legacy finalizer removal rejected: %v", err)
			}
			if err := admin.Get(ctx, client.ObjectKeyFromObject(o), o); !apierrors.IsNotFound(err) {
				t.Fatalf("legacy object still retained: %v", err)
			}
		})
	}
	if err := admin.Create(ctx, valid.DeepCopy(), client.DryRunAll); err != nil {
		t.Fatalf("valid fault rejected: %v", err)
	}
	defaulted := valid.DeepCopy()
	defaulted.Object["status"] = map[string]any{"experiment": map[string]any{}}
	if err := admin.Create(ctx, defaulted, client.DryRunAll); err != nil {
		t.Fatalf("upstream webhook empty status default rejected: %v", err)
	}
	if delay {
		for _, edge := range []struct{ latency, duration string }{{"1ms", "1s"}, {"1000ms", "120s"}} {
			o := valid.DeepCopy()
			_ = unstructured.SetNestedField(o.Object, edge.latency, "spec", "delay", "latency")
			_ = unstructured.SetNestedField(o.Object, edge.duration, "spec", "duration")
			unstructured.RemoveNestedField(o.Object, "spec", "delay", "jitter")
			unstructured.RemoveNestedField(o.Object, "spec", "delay", "correlation")
			if err := admin.Create(ctx, o, client.DryRunAll); err != nil {
				t.Fatalf("valid profile boundary rejected: %v", err)
			}
		}
	}
	cases := []struct {
		name  string
		path  []string
		value any
	}{
		{"owner", []string{"metadata", "ownerReferences"}, []any{map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "fixture", "uid": "fixture-uid"}}},
		{"invalid-actor", []string{"spec", "selector", "labelSelectors", "network.stacks.org/actor"}, "Actor_1"},
		{"target-value", []string{"spec", "target", "value"}, "1"},
		{"reorder", []string{"spec", "delay", "reorder"}, map[string]any{"reorder": "1", "gap": int64(1), "correlation": "0"}},
		{"status", []string{"status"}, map[string]any{"experiment": map[string]any{"desiredPhase": "Stop"}}},
		{"finalizer", []string{"metadata", "finalizers"}, []any{"test.example/hold"}},
		{"managed", []string{"metadata", "labels", "managed-by"}, "workflow"},
		{"paused", []string{"metadata", "annotations", "experiment.chaos-mesh.org/pause"}, "true"},
		{"all", []string{"spec", "mode"}, "all"}, {"reverse", []string{"spec", "direction"}, "from"},
		{"target-all", []string{"spec", "target", "mode"}, "all"}, {"other-network", []string{"spec", "target", "selector", "labelSelectors", "network.stacks.org/network"}, "other"},
		{"same-actor", []string{"spec", "target", "selector", "labelSelectors", "network.stacks.org/actor"}, "bitcoin"},
		{"cross-namespace", []string{"spec", "target", "selector", "namespaces"}, []any{"other"}},
		{"external", []string{"spec", "externalTargets"}, []any{"192.0.2.1"}}, {"remote", []string{"spec", "remoteCluster"}, "other"},
		{"device", []string{"spec", "device"}, "eth0"}, {"target-device", []string{"spec", "targetDevice"}, "eth0"}, {"value", []string{"spec", "value"}, "1"},
		{"extra-label", []string{"spec", "selector", "labelSelectors", "extra"}, "value"},
		{"zero-duration", []string{"spec", "duration"}, "0s"}, {"long-duration", []string{"spec", "duration"}, "121s"}, {"fractional-duration", []string{"spec", "duration"}, "1.5s"},
		{"zero-delay", []string{"spec", "delay", "latency"}, "0ms"}, {"long-delay", []string{"spec", "delay", "latency"}, "1001ms"}, {"jitter", []string{"spec", "delay", "jitter"}, "1ms"},
		{"correlation", []string{"spec", "delay", "correlation"}, "1"}, {"loss", []string{"spec", "loss"}, map[string]any{"loss": "1"}},
	}
	for _, selector := range [][]string{{"spec", "selector"}, {"spec", "target", "selector"}} {
		for key, value := range map[string]any{"pods": map[string]any{ns.Name: []any{"pod"}}, "nodes": []any{"node"}, "nodeSelectors": map[string]any{"node": "x"}, "annotationSelectors": map[string]any{"a": "b"}, "fieldSelectors": map[string]any{"metadata.name": "pod"}, "podPhaseSelectors": []any{"Running"}, "expressionSelectors": []any{map[string]any{"key": "x", "operator": "Exists"}}} {
			cases = append(cases, struct {
				name  string
				path  []string
				value any
			}{"selector-" + key + "-" + selector[len(selector)-2], append(append([]string{}, selector...), key), value})
		}
	}
	for _, c := range cases {
		if !delay && len(c.path) > 1 && c.path[0] == "spec" && c.path[1] == "delay" {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			o := valid.DeepCopy()
			if err := unstructured.SetNestedField(o.Object, c.value, c.path...); err != nil {
				t.Fatal(err)
			}
			if err := admin.Create(ctx, o, client.DryRunAll); !policyDenied(err) {
				t.Fatalf("expected policy denial, got %v", err)
			}
		})
	}
	// A complete valid alternate mechanism must be accepted only when enabled.
	for _, action := range []string{"delay", "partition"} {
		o := valid.DeepCopy()
		_ = unstructured.SetNestedField(o.Object, action, "spec", "action")
		unstructured.RemoveNestedField(o.Object, "spec", "delay")
		direction := "both"
		enabled := partition
		if action == "delay" {
			direction, enabled = "to", delay
			_ = unstructured.SetNestedField(o.Object, map[string]any{"latency": "100ms"}, "spec", "delay")
		}
		_ = unstructured.SetNestedField(o.Object, direction, "spec", "direction")
		err := admin.Create(ctx, o.DeepCopy(), client.DryRunAll)
		if enabled && err != nil || !enabled && !policyDenied(err) {
			t.Fatalf("%s enabled=%v: %v", action, enabled, err)
		}
		if action == "partition" && enabled {
			for _, duration := range []string{"1s", "120s"} {
				edge := o.DeepCopy()
				_ = unstructured.SetNestedField(edge.Object, duration, "spec", "duration")
				if err := admin.Create(ctx, edge, client.DryRunAll); err != nil {
					t.Fatal(err)
				}
			}
			for _, direction := range []string{"to", "from"} {
				bad := o.DeepCopy()
				_ = unstructured.SetNestedField(bad.Object, direction, "spec", "direction")
				if err := admin.Create(ctx, bad, client.DryRunAll); !policyDenied(err) {
					t.Fatalf("partition direction %s: %v", direction, err)
				}
			}
			_ = unstructured.SetNestedField(o.Object, map[string]any{"latency": "100ms"}, "spec", "delay")
			if err := admin.Create(ctx, o, client.DryRunAll); !policyDenied(err) {
				t.Fatalf("mixed mechanisms: %v", err)
			}
		}
	}
	for _, path := range [][]string{{"spec", "selector", "labelSelectors"}, {"spec", "target", "selector", "labelSelectors"}} {
		o := valid.DeepCopy()
		_ = unstructured.SetNestedStringMap(o.Object, map[string]string{"app.kubernetes.io/name": "bitcoin-production"}, path...)
		if err := admin.Create(ctx, o, client.DryRunAll); !policyDenied(err) {
			t.Fatalf("producer selector accepted: %v", err)
		}
	}

	for _, path := range [][]string{{"spec", "duration"}, {"spec", "target"}, {"metadata", "labels", "actions.stacks.org/correlation-id"}, {"spec", "selector", "labelSelectors", "network.stacks.org/network"}} {
		o := valid.DeepCopy()
		unstructured.RemoveNestedField(o.Object, path...)
		if err := admin.Create(ctx, o, client.DryRunAll); !policyDenied(err) {
			t.Fatalf("missing %v accepted: %v", path, err)
		}
	}
	user, err := environment.AddUser(envtest.User{Name: "system:serviceaccount:" + ns.Name + ":stacks-chaos-agent", Groups: []string{"system:serviceaccounts", "system:serviceaccounts:" + ns.Name}}, config)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := client.New(user.Config(), client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		group, resource, subresource, verb string
		allowed                            bool
	}{{"chaos-mesh.org", "networkchaos", "", "create", true}, {"chaos-mesh.org", "networkchaos", "", "delete", true}, {"chaos-mesh.org", "networkchaos", "", "watch", true}, {"chaos-mesh.org", "networkchaos", "", "patch", false}, {"chaos-mesh.org", "networkchaos", "status", "update", false}, {"chaos-mesh.org", "workflows", "", "create", false}, {"chaos-mesh.org", "schedules", "", "create", false}, {"chaos-mesh.org", "podchaos", "", "create", false}, {"", "pods", "", "patch", false}, {"", "secrets", "", "get", false}} {
		review := &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authv1.ResourceAttributes{Namespace: ns.Name, Group: c.group, Resource: c.resource, Subresource: c.subresource, Verb: c.verb}}}
		if err := agent.Create(ctx, review); err != nil {
			t.Fatal(err)
		}
		if review.Status.Allowed != c.allowed {
			t.Fatalf("unexpected permission %+v: %+v", c, review.Status)
		}
	}
	crossNamespace := &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authv1.ResourceAttributes{Namespace: "other", Group: "chaos-mesh.org", Resource: "networkchaos", Verb: "create"}}}
	if err := agent.Create(ctx, crossNamespace); err != nil {
		t.Fatal(err)
	}
	if crossNamespace.Status.Allowed {
		t.Fatal("agent can create faults outside its namespace")
	}
	created := valid.DeepCopy()
	if err := agent.Create(ctx, created); err != nil {
		t.Fatal(err)
	}
	changed := created.DeepCopy()
	_ = unstructured.SetNestedField(changed.Object, "29s", "spec", "duration")
	if err := admin.Update(ctx, changed); !policyDenied(err) {
		t.Fatalf("mutable spec: %v", err)
	}
	created.SetFinalizers([]string{"test.example/cleanup"})
	if err := admin.Update(ctx, created); err != nil {
		t.Fatalf("controller metadata update rejected: %v", err)
	}
	created.Object["status"] = map[string]any{"experiment": map[string]any{"desiredPhase": "Run"}}
	if err := admin.Update(ctx, created); err != nil {
		t.Fatalf("controller status rejected: %v", err)
	}
	// De-enrollment retains the binding and rejects new writes, but cancellation stays possible.
	if err := admin.Get(ctx, client.ObjectKeyFromObject(ns), ns); err != nil {
		t.Fatal(err)
	}
	delete(ns.Labels, "network.stacks.org/chaos-profile")
	if err := admin.Update(ctx, ns); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		err := admin.Create(ctx, valid.DeepCopy(), client.DryRunAll)
		if policyDenied(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("de-enrollment bypass: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	created.SetFinalizers(nil)
	if err := admin.Update(ctx, created); err != nil {
		t.Fatalf("de-enrollment blocked finalizer cleanup: %v", err)
	}
	if err := agent.Delete(ctx, created); err != nil {
		t.Fatalf("cancellation rejected: %v", err)
	}
}

// policyDenied distinguishes CEL admission from schema validation and RBAC failures.
func policyDenied(err error) bool {
	return apierrors.IsInvalid(err) && strings.Contains(err.Error(), "ValidatingAdmissionPolicy") && strings.Contains(err.Error(), "denied request")
}
