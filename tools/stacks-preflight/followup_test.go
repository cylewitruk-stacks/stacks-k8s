package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Deployment scope alone cannot establish current rollout readiness.
func TestObserverRolloutIsIndependentOfScope(t *testing.T) {
	for _, mode := range []string{
		"ready", "default-replicas", "zero", "old-generation", "updating",
		"not-ready", "unavailable", "overlap", "deleting",
	} {
		t.Run(mode, func(t *testing.T) {
			replicas := int32(1)
			d := &appsv1.Deployment{
				TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
				ObjectMeta: metav1.ObjectMeta{Name: "observer", Namespace: "system", Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2,
					Replicas:           1,
					UpdatedReplicas:    1,
					ReadyReplicas:      1,
					AvailableReplicas:  1,
				},
			}
			switch mode {
			case "default-replicas":
				d.Spec.Replicas = nil
			case "zero":
				replicas = 0
			case "old-generation":
				d.Status.ObservedGeneration = 1
			case "updating":
				d.Status.UpdatedReplicas = 0
			case "not-ready":
				d.Status.ReadyReplicas = 0
			case "unavailable":
				d.Status.AvailableReplicas = 0
			case "overlap":
				d.Status.Replicas = 2
			case "deleting":
				now := metav1.Now()
				d.DeletionTimestamp = &now
			}
			// Exercise the production dynamic GET and independent typed decoder.
			d.Spec.Template.Spec.Containers = append(
				d.Spec.Template.Spec.Containers,
				corev1.Container{Name: "manager", Args: []string{"--watch-namespace=lab"}},
			)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("unexpected mutation")
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(d)
			}))
			defer server.Close()
			c, err := dynamic.NewForConfig(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			out := report{Passed: true}
			checkObserver(
				t.Context(),
				c,
				request{Namespace: "lab", OperatorNamespace: "system", OperatorDeployment: "observer"},
				&out,
			)
			if len(out.Checks) != 2 {
				t.Fatal(out)
			}
			want := mode == "ready" || mode == "default-replicas"
			if (out.Checks[1].Status == checkPass) != want || out.Checks[1].Name != "observer-rollout" {
				t.Fatal(out)
			}
			if mode != "deleting" && out.Checks[0].Status != checkPass {
				t.Fatal("scope changed with rollout", out)
			}
		})
	}
}

// Test both inclusive boundaries without wall-clock races.
func TestFreshnessClockSkewBounds(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		sample time.Time
		want   bool
	}{
		{now.Add(5 * time.Second), true},
		{now.Add(5*time.Second + time.Nanosecond), false},
		{now.Add(-60 * time.Second), true},
		{now.Add(-60*time.Second - time.Nanosecond), false},
		{time.Time{}, false},
	} {
		if freshAt(tt.sample, now, 60) != tt.want {
			t.Fatal(tt)
		}
	}
}

// Backend SQL uses the same future allowance without extending maximum age.
func TestBackendClockSkewQueryBounds(t *testing.T) {
	before := time.Now().UTC()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		parts := strings.Split(r.Form.Get("sql"), "'")
		if len(parts) != 7 {
			t.Error(parts)
			return
		}
		lower, err := time.Parse(time.RFC3339Nano, parts[3])
		if err != nil {
			t.Error(err)
		}
		upper, err := time.Parse(time.RFC3339Nano, parts[5])
		if err != nil {
			t.Error(err)
		}
		anchor := lower.Add(60 * time.Second)
		if upper.Sub(lower) != 65*time.Second || anchor.Before(before) || anchor.After(time.Now()) {
			t.Error(lower, upper)
		}
		_, _ = fmt.Fprint(w, `{"output":[{"records":{"rows":[[123]]}}]}`)
	}))
	defer s.Close()
	out := report{Passed: true}
	checkBackend(
		t.Context(),
		request{
			NetworkUID:    networkUID,
			MaxAgeSeconds: 60,
			Backend:       backendRequest{Endpoint: s.URL, Tables: []tableRequest{{"events", "timestamp"}}},
		},
		&out,
	)
	if !out.Passed {
		t.Fatal(out)
	}
}

// The CLI must honor explicit context selection and reject plugins before execution.
func TestRunLoadsSelectedKubeconfigAndRejectsPlugins(t *testing.T) {
	calls := 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Error("unexpected mutation", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
	}))
	defer s.Close()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/sql" {
			t.Error("unexpected backend request")
		}
		_, _ = fmt.Fprint(w, `{"output":[{"records":{"rows":[[123]]}}]}`)
	}))
	defer backend.Close()
	dir := t.TempDir()
	marker := filepath.Join(dir, "plugin-executed")
	cfg := clientcmdapi.Config{
		CurrentContext: "plugin",
		Clusters:       map[string]*clientcmdapi.Cluster{"selected": {Server: s.URL, InsecureSkipTLSVerify: true}},
		Contexts: map[string]*clientcmdapi.Context{
			"static":   {Cluster: "selected", AuthInfo: "static"},
			"plugin":   {Cluster: "selected", AuthInfo: "plugin"},
			"provider": {Cluster: "selected", AuthInfo: "provider"},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"static": {Token: "test-token"},
			"plugin": {
				Exec: &clientcmdapi.ExecConfig{
					APIVersion:      "client.authentication.k8s.io/v1",
					Command:         "sh",
					Args:            []string{"-c", `touch "$1"`, "sh", marker},
					InteractiveMode: clientcmdapi.NeverExecInteractiveMode,
				},
			},
			"provider": {AuthProvider: &clientcmdapi.AuthProviderConfig{Name: "oidc"}},
		},
	}
	path := filepath.Join(dir, "config")
	if err := clientcmd.WriteToFile(cfg, path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", filepath.Join(dir, "not-selected"))
	for _, selected := range []string{"static", "plugin", "provider"} {
		r := request{
			Kubeconfig:         path,
			Context:            selected,
			Namespace:          "lab",
			Network:            "network",
			NetworkUID:         networkUID,
			Telemetry:          "capture",
			OperatorNamespace:  "system",
			OperatorDeployment: "observer",
			MaxAgeSeconds:      60,
			Backend: backendRequest{
				Endpoint:      backend.URL,
				Authorization: "Basic test",
				Tables:        []tableRequest{{"events", "timestamp"}},
			},
		}
		input, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		calls = 0
		var out bytes.Buffer
		err = run(context.Background(), bytes.NewReader(input), &out)
		if err == nil {
			t.Fatal("missing prerequisites unexpectedly passed")
		}
		if selected == "static" {
			if calls != 3 || !strings.Contains(out.String(), `"observer-rollout"`) {
				t.Fatal(calls, err, out.String())
			}
		} else if calls != 0 || !strings.Contains(err.Error(), "auth plugins") || out.Len() != 0 {
			t.Fatal(calls, err, out.String())
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("credential plugin executed", err)
		}
	}
}
