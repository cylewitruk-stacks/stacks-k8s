//go:build integration

package foundationintegration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func verifyManagerAndScopedJob(t *testing.T, ctx context.Context, c client.Client, cfg *rest.Config, scheme *runtime.Scheme) {
	t.Helper()
	ns := "foundation-permissions"
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("helm", "template", "foundation", filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator"), "--namespace", ns, "--kube-version", "1.37.0").CombinedOutput()
	if err != nil {
		t.Fatalf("render: %v %s", err, out)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
	for {
		var obj unstructured.Unstructured
		err := decoder.Decode(&obj)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if obj.GetKind() == "Deployment" {
			continue
		}
		if obj.GetKind() == "ServiceAccount" {
			obj.SetNamespace(ns)
		}
		if err := c.Create(ctx, &obj); err != nil {
			t.Fatal(err)
		}
	}
	scoped := rest.CopyConfig(cfg)
	scoped.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:" + ns + ":foundation"}
	manager, err := ctrl.NewManager(scoped, ctrl.Options{Cache: foundation.CacheOptions(), Scheme: scheme, Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0", Client: client.Options{Cache: &client.CacheOptions{DisableFor: []client.Object{&corev1.Secret{}, &corev1.ServiceAccount{}, &rbacv1.Role{}, &rbacv1.RoleBinding{}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := foundation.Register(manager, "foundation:test"); err != nil {
		t.Fatal(err)
	}
	running, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- manager.Start(running) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("manager did not stop")
		}
	}()
	// Default source account has no network. Its watched lifecycle provisions a Job using rendered controller RBAC.
	a := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "generated", Namespace: ns}}
	if err := c.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	var job batchv1.Job
	for time.Now().Before(deadline) {
		var jobs batchv1.JobList
		if err := c.List(ctx, &jobs, client.InNamespace(ns)); err != nil {
			t.Fatal(err)
		}
		if len(jobs.Items) > 0 {
			job = jobs.Items[0]
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.Name == "" {
		t.Fatal("controller with rendered permissions did not provision identity Job")
	}
	var input foundation.KeyJobInput
	for _, arg := range job.Spec.Template.Spec.Containers[0].Args {
		if strings.HasPrefix(arg, "--input=") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(arg, "--input=")), &input); err != nil {
				t.Fatal(err)
			}
		}
	}
	jobConfig := rest.CopyConfig(cfg)
	jobConfig.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:" + ns + ":" + job.Spec.Template.Spec.ServiceAccountName}
	worker, err := client.New(jobConfig, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	// RBAC grants have propagated once the exact Secret read is authorized.
	for time.Now().Before(deadline) {
		var key corev1.Secret
		err = worker.Get(ctx, client.ObjectKey{Namespace: ns, Name: input.CredentialsRef.Name}, &key)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := foundation.RunKeyJob(ctx, worker, input); err != nil {
		t.Fatalf("scoped resolver: %v", err)
	}
	var other corev1.Secret
	if err := worker.Get(ctx, client.ObjectKey{Namespace: ns, Name: "unrelated"}, &other); !apierrors.IsForbidden(err) {
		t.Fatalf("unrelated Secret read not forbidden: %v", err)
	}
	if err := worker.Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "unexpected", Namespace: ns}}); !apierrors.IsForbidden(err) {
		t.Fatalf("worker workload creation not forbidden: %v", err)
	}
	for time.Now().Before(deadline) {
		if err := c.Get(ctx, client.ObjectKeyFromObject(a), a); err != nil {
			t.Fatal(err)
		}
		if meta.IsStatusConditionTrue(a.Status.Conditions, "Resolved") {
			verifyResolverFailureAndCache(t, ctx, c, manager, ns)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("report notification did not resolve account: %+v", a.Status)
}
