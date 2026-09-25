package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const networkUID = "12345678-1234-1234-1234-123456789abc"

func TestReadOnlyPreflightChecksIdentityFreshnessAndEnrollment(t *testing.T) {
	for _, mode := range []string{
		"healthy", "wrong-uid", "stale-heartbeat", "stale-condition",
		"scope", "chaos-annotation", "denied", "source", "stale-recording", "sidecar-scope",
	} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				if r.Method != "GET" {
					t.Error("Kubernetes mutation")
					w.WriteHeader(500)
					return
				}
				now := time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
				switch {
				case strings.Contains(r.URL.Path, "stacksnetworks/"):
					uid := networkUID
					if mode == "wrong-uid" {
						uid = "other"
					}
					_, _ = fmt.Fprintf(
						w,
						`{"apiVersion":"network.stacks.org/v1alpha2","kind":"StacksNetwork","metadata":{"name":"network","uid":%q}}`,
						uid,
					)
				case strings.Contains(r.URL.Path, "networktelemetries/"):
					if mode == "denied" {
						w.WriteHeader(403)
						_, _ = fmt.Fprint(w, `{"message":"PRIVATE"}`)
						return
					}
					heartbeat := now
					if mode == "stale-heartbeat" {
						heartbeat = "2020-01-01T00:00:00Z"
					}
					recordingGen := 2
					if mode == "stale-recording" {
						recordingGen = 1
					}
					gen := 2
					if mode == "stale-condition" {
						gen = 1
					}
					_, _ = fmt.Fprintf(
						w,
						`{"apiVersion":"observation.stacks.org/v1alpha2",
"kind":"NetworkTelemetry",
"metadata":{"generation":2},
"spec":{"networkName":"network",
"networkUID":%q},
"status":{"admitted":true,
"conditions":[{"type":"WorkloadsReady",
"status":"True",
"observedGeneration":%d}],
"recording":{"observedGeneration":%d,
"backendReady":true,
"heartbeatAt":%q,
"sources":[{"name":"stacksnetworks",
"available":%t,
"observedAt":%q}]}}}`,
						networkUID,
						gen,
						recordingGen,
						heartbeat,
						mode != "source",
						now,
					)
				case strings.Contains(r.URL.Path, "deployments/"):
					ns := "lab"
					if mode == "scope" {
						ns = "other"
					}
					_, _ = fmt.Fprintf(
						w,
						`{"apiVersion":"apps/v1",
"kind":"Deployment",
"metadata":{"generation":2},
"status":{"observedGeneration":2,"replicas":1,"updatedReplicas":1,"readyReplicas":1,"availableReplicas":1},
"spec":{"replicas":1,"template":{"spec":{"containers":[{"name":%q,"args":["--watch-namespace=%s"],
"env":[{"value":"PRIVATE"}]}]}}}}`,
						func() string {
							if mode == "sidecar-scope" {
								return "sidecar"
							}
							return "manager"
						}(),
						ns,
					)
				case r.URL.Path == "/api/v1/namespaces/lab":
					annotation := "enabled"
					if mode == "chaos-annotation" {
						annotation = ""
					}
					_, _ = fmt.Fprintf(
						w,
						`{"apiVersion":"v1",
"kind":"Namespace",
"metadata":{"annotations":{"chaos-mesh.org/inject":%q}}}`,
						annotation,
					)
				default:
					t.Error("unexpected read", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer s.Close()
			client, err := dynamic.NewForConfig(&rest.Config{Host: s.URL})
			if err != nil {
				t.Fatal(err)
			}
			result := inspect(
				context.Background(),
				client,
				request{
					Namespace:          "lab",
					Network:            "network",
					NetworkUID:         networkUID,
					Telemetry:          "capture",
					OperatorNamespace:  "system",
					OperatorDeployment: "observer",
					MaxAgeSeconds:      60,
					Chaos:              true,
					RequiredSources:    []string{"stacksnetworks"},
				},
			)
			if mode == "stale-recording" {
				for _, c := range result.Checks {
					if strings.HasPrefix(c.Name, "source/") && c.Status == checkPass {
						t.Fatal("stale source passed")
					}
				}
			}
			if result.Passed != (mode == "healthy") || requests != 4 {
				t.Fatalf("mode %s requests=%d result=%+v", mode, requests, result)
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "PRIVATE") {
				t.Fatal("private data leaked")
			}
		})
	}
}

func TestBackendBoundsIdentityAndDoesNotFollowRedirects(t *testing.T) {
	for _, mode := range []string{"healthy", "empty", "denied", "oversized", "malformed", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path != "/v1/sql" || r.Method != "POST" {
					t.Error("unexpected backend request")
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				q := r.Form.Get("sql")
				if !strings.HasPrefix(q, "SELECT timestamp FROM events WHERE network_uid = '"+networkUID+"'") ||
					!strings.HasSuffix(q, "DESC LIMIT 1") {
					t.Error(q)
				}
				switch mode {
				case "healthy":
					_, _ = fmt.Fprint(w, `{"output":[{"records":{"rows":[[123]]}}]}`)
				case "empty":
					_, _ = fmt.Fprint(w, `{"output":[{"records":{"rows":[]}}]}`)
				case "denied":
					w.WriteHeader(401)
					_, _ = fmt.Fprint(w, "PRIVATE")
				case "oversized":
					_, _ = fmt.Fprint(w, strings.Repeat("x", 65537))
				case "malformed":
					_, _ = fmt.Fprint(w, `{"output":null,"error":"PRIVATE"}`)
				case "redirect":
					w.Header().Set("Location", "/credential-trap")
					w.WriteHeader(302)
				}
			}))
			defer s.Close()
			r := request{
				NetworkUID:    networkUID,
				MaxAgeSeconds: 60,
				Backend: backendRequest{
					Endpoint:      s.URL,
					Authorization: "Basic PRIVATE",
					Tables:        []tableRequest{{"events", "timestamp"}},
				},
			}
			out := report{Passed: true}
			checkBackend(context.Background(), r, &out)
			if out.Passed != (mode == "healthy") || requests != 1 {
				t.Fatalf("%+v requests=%d", out, requests)
			}
			b, _ := json.Marshal(out)
			if strings.Contains(string(b), "PRIVATE") {
				t.Fatal("leaked response or credential")
			}
		})
	}
}

func TestPreflightRejectsUnsafeInputsAndFutureObservations(t *testing.T) {
	if fresh(time.Now().Add(time.Minute), 60) || fresh(time.Time{}, 60) || fresh(time.Now().Add(-time.Hour), 60) {
		t.Fatal("invalid freshness")
	}
	b := backendRequest{
		Endpoint:      "http://localhost:4000",
		Authorization: "Basic value",
		Tables:        []tableRequest{{"events;DROP TABLE secret", "timestamp"}},
	}
	if b.validate() == nil {
		t.Fatal("SQL identifier accepted")
	}
}

func TestWatchScopeUsesEffectiveManagerArguments(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{[]string{"--watch-namespace=lab", "--watch-namespace=other"}, false},
		{[]string{"--watch-namespace=other", "--watch-namespace", " lab,other "}, true},
		{[]string{"--", "--watch-namespace=lab"}, false},
		{[]string{"position", "--watch-namespace=lab"}, false},
		{[]string{"--watch-namespace=lab", "--watch-namespace"}, false},
		{[]string{"-watch-namespace=lab"}, true},
		{[]string{"--metrics-bind-address", "--watch-namespace=lab"}, false},
		{[]string{"--metrics-bind-address", ":8080", "--watch-namespace=lab"}, false},
		{[]string{"--metrics-bind-address=:8080", "--watch-namespace=lab"}, true},
	} {
		if got := watchScope(tt.args, "lab"); got != tt.want {
			t.Errorf("%v: %v", tt.args, got)
		}
	}
}

func TestPreflightRejectsAuthPluginsAndOversizedRequests(t *testing.T) {
	if validateAuth(&rest.Config{}) != nil {
		t.Fatal("static auth rejected")
	}
	if validateAuth(&rest.Config{ExecProvider: &clientcmdapi.ExecConfig{Command: "never-run"}}) == nil {
		t.Fatal("exec accepted")
	}
	if validateAuth(&rest.Config{AuthProvider: &clientcmdapi.AuthProviderConfig{Name: "oidc"}}) == nil {
		t.Fatal("provider accepted")
	}
	var out bytes.Buffer
	input := "{}" + strings.Repeat(" ", 65535) + "{}"
	if err := run(
		context.Background(),
		strings.NewReader(input),
		&out,
	); err == nil ||
		!strings.Contains(err.Error(), "byte bound") {
		t.Fatal(err)
	}
}
