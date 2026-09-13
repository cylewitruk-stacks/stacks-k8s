// Package localcluster tests the Make helpers with isolated command substitutes.
package localcluster

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLifecycle verifies command targeting and error propagation without a cluster.
func TestLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, target, nodes, fail, metricsOwner string
		args                                    []string
		wantError                               bool
		want, reject                            []string
	}{
		{
			name:   "create",
			target: "create",
			want: []string{"kind create cluster --name stacks-k8s --config kind.yaml --kubeconfig " +
				"kubeconfig --wait 120s", "upgrade --install metrics-server", "upgrade --install headlamp"},
			reject: []string{"docker ", "delete"},
		},
		{
			name:   "create without addons",
			target: "create",
			args:   []string{"HEADLAMP=false", "METRICS_SERVER=false"},
			want:   []string{"kind create"},
			reject: []string{"helm ", "kubectl "},
		},
		{
			name:   "create only metrics",
			target: "create",
			args:   []string{"HEADLAMP=false"},
			want:   []string{"upgrade --install metrics-server"},
			reject: []string{"upgrade --install headlamp"},
		},
		{
			name:      "invalid option",
			target:    "create",
			args:      []string{"HEADLAMP=no"},
			wantError: true,
			reject:    []string{"kind ", "helm ", "kubectl "},
		},
		{
			name:      "existing create fails",
			target:    "create",
			fail:      "kind",
			wantError: true,
			reject:    []string{"delete", "helm ", "kubectl "},
		},
		{
			name:   "start",
			target: "start",
			nodes:  "a123\nb456\nc789",
			want: []string{
				"docker start a123 b456 c789",
				"kind export kubeconfig --name stacks-k8s --kubeconfig kubeconfig",
				"--context kind-stacks-k8s",
				"node/stacks-k8s-worker2",
			},
			reject: []string{"create cluster", "delete", "docker stop", "helm "},
		},
		{
			name:      "absent start",
			target:    "start",
			wantError: true,
			reject:    []string{"docker start", "kind ", "kubectl "},
		},
		{
			name:      "docker read failure",
			target:    "stop",
			fail:      "docker",
			wantError: true,
			reject:    []string{"docker stop", "kind "},
		},
		{
			name:   "stop",
			target: "stop",
			nodes:  "a123\nb456",
			want:   []string{"docker stop --timeout 30 a123 b456"},
			reject: []string{"kind ", "kubectl "},
		},
		{name: "absent stop", target: "stop", reject: []string{"docker stop", "kind ", "kubectl "}},
		{
			name:      "export failure",
			target:    "start",
			nodes:     "a123",
			fail:      "kind",
			wantError: true,
			reject:    []string{"kubectl "},
		},
		{
			name:   "chaos install",
			target: "chaos-install",
			want: []string{
				"helm pull chaos-mesh --repo https://charts.chaos-mesh.org --version 2.8.4",
				"helm upgrade --install chaos-mesh",
				"--kubeconfig kubeconfig --kube-context kind-stacks-k8s",
				"--namespace chaos-mesh --create-namespace --reset-values",
				"--values ../../examples/chaos/upstream-values.yaml --wait --timeout 5m",
			},
			reject: []string{"docker ", "kind ", "kubectl "},
		},
		{
			name:      "chart download failure",
			target:    "chaos-install",
			fail:      "helm",
			wantError: true,
			reject:    []string{"helm upgrade"},
		},
		{
			name:      "chart checksum failure",
			target:    "chaos-install",
			fail:      "checksum",
			wantError: true,
			reject:    []string{"helm upgrade"},
		},
		{
			name:   "headlamp install",
			target: "headlamp-install",
			want: []string{
				"upgrade --install metrics-server",
				"upgrade --install headlamp",
				"--namespace headlamp --create-namespace --reset-values --values " +
					"headlamp-values.yaml",
				"wait --for=condition=Available",
				"top nodes",
			},
		},
		{
			name:   "headlamp without metrics",
			target: "headlamp-install",
			args:   []string{"METRICS_SERVER=false"},
			want:   []string{"upgrade --install headlamp"},
			reject: []string{"metrics-server", "kubectl "},
		},
		{
			name:      "headlamp checksum failure",
			target:    "headlamp-install",
			args:      []string{"METRICS_SERVER=false"},
			fail:      "checksum",
			wantError: true,
			reject:    []string{"upgrade --install"},
		},
		{
			name:      "headlamp download failure",
			target:    "headlamp-install",
			args:      []string{"METRICS_SERVER=false"},
			fail:      "helm",
			wantError: true,
			reject:    []string{"upgrade --install"},
		},
		{
			name:      "headlamp readiness failure",
			target:    "headlamp-install",
			args:      []string{"METRICS_SERVER=false"},
			fail:      "upgrade",
			wantError: true,
		},
		{
			name:      "create addon failure retains cluster",
			target:    "create",
			fail:      "upgrade",
			wantError: true,
			want:      []string{"kind create"},
			reject:    []string{"kind delete", "upgrade --install headlamp"},
		},
		{
			name:         "metrics owned upgrade",
			target:       "metrics-install",
			metricsOwner: "metrics-server/kube-system",
			want: []string{
				"upgrade --install metrics-server",
				"--values metrics-server-values.yaml",
				"top nodes",
			},
		},
		{
			name:         "metrics external reuse",
			target:       "metrics-install",
			metricsOwner: "other/monitoring",
			want:         []string{"wait --for=condition=Available", "top nodes"},
			reject:       []string{"helm "},
		},
		{
			name:         "metrics unmanaged reuse",
			target:       "metrics-install",
			metricsOwner: "/",
			want:         []string{"top nodes"},
			reject:       []string{"helm "},
		},
		{
			name:      "metrics read failure",
			target:    "metrics-install",
			fail:      "kubectl",
			wantError: true,
			reject:    []string{"helm "},
		},
		{
			name:      "metrics checksum failure",
			target:    "metrics-install",
			fail:      "checksum",
			wantError: true,
			reject:    []string{"upgrade --install", "top nodes"},
		},
		{
			name:         "metrics unavailable",
			target:       "headlamp-install",
			metricsOwner: "other/monitoring",
			fail:         "wait",
			wantError:    true,
			reject:       []string{"helm ", "top nodes"},
		},
		{
			name:         "metrics collection failure",
			target:       "headlamp-install",
			metricsOwner: "other/monitoring",
			fail:         "top",
			wantError:    true,
			reject:       []string{"helm "},
		},
		{
			name:   "headlamp uninstall",
			target: "headlamp-uninstall",
			want:   []string{"uninstall headlamp --namespace headlamp --ignore-not-found --wait"},
			reject: []string{"metrics-server", "kind ", "delete namespace"},
		},
		{
			name:   "headlamp forward",
			target: "headlamp",
			args:   []string{"HEADLAMP_PORT=8888"},
			want:   []string{"--namespace headlamp port-forward --address 127.0.0.1 service/headlamp 8888:80"},
			reject: []string{"helm ", "create token"},
		},
		{
			name:   "headlamp token",
			target: "headlamp-token",
			want:   []string{"--namespace headlamp create token headlamp --duration=1h"},
			reject: []string{"helm ", "port-forward"},
		},

		{
			name:   "destroy",
			target: "destroy",
			want:   []string{"kind delete cluster --name stacks-k8s --kubeconfig kubeconfig"},
			reject: []string{"docker ", "create cluster"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source, err := os.ReadFile("Makefile")
			if err != nil {
				t.Fatal(err)
			}
			// Replace only the archive digest to exercise real shasum with a tiny fixture.
			for _, digest := range []string{
				"ae4abd385649771300e4d33a44627c0df3618be0780c385bf30cc2fdf2ad93fa",
				"38c619e0ed5db4164b02c353bcbb24e8157c1d98d961b292f99d68cc9316b8eb",
				"c2ca1185c01e6e7f53dd1b7d131f0c9b3fa50e003ed068b784563a1b5a3422a1",
			} {
				source = []byte(
					strings.ReplaceAll(string(source), digest, fmt.Sprintf("%x", sha256.Sum256([]byte("test chart")))),
				)
			}
			// #nosec G703 -- Path is derived from repository fixtures or a private test directory, not a remote request.
			if err := os.WriteFile(filepath.Join(dir, "Makefile"), source, 0o600); err != nil {
				t.Fatal(err)
			}
			// Substitutes record commands and supply a chart fixture without external services.
			fake := `#!/bin/sh
set -eu
command=${0##*/}
printf '%s %s\n' "$command" "$*" >> "$CALL_LOG"
test "$KIND_EXPERIMENTAL_PROVIDER" = docker
test "$DOCKER_CONTEXT" = test-engine
if test "$command" = "$FAIL_COMMAND"; then exit 1; fi
case " $* " in *" $FAIL_COMMAND "*) if test -n "$FAIL_COMMAND"; then exit 1; fi ;; esac
if test "$command" = kubectl; then
 case " $* " in *" get apiservice "*) printf '%s' "$METRICS_OWNER" ;; esac
fi
if test "$command" = helm && test "$1" = pull; then
 chart=$2
 while test "$1" != --version; do shift; done
 version=$2
 while test "$1" != --destination; do shift; done
 if test "$FAIL_COMMAND" = checksum; then
  printf tampered > "$2/$chart-$version.tgz"
 else
  printf 'test chart' > "$2/$chart-$version.tgz"
 fi
fi
if test "$command" = docker && test "$1" = ps; then
 test "$*" = "ps -a --filter label=io.x-k8s.kind.cluster=stacks-k8s --format {{.ID}}"
 printf '%s\n' "$TEST_NODES"
fi
`
			for _, name := range []string{"docker", "kind", "kubectl", "helm"} {
				// #nosec G306 -- Private test stub must be executable; only its owner can read or execute it.
				if err := os.WriteFile(filepath.Join(dir, name), []byte(fake), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			log := filepath.Join(dir, "calls")
			// #nosec G204 -- Fixed executable and separate arguments from the test harness; no shell evaluation.
			cmd := exec.CommandContext(t.Context(),
				"make",
				append(
					[]string{"--no-print-directory", "-C", dir, tc.target, "HEADLAMP=true", "METRICS_SERVER=true"},
					tc.args...)...)
			cmd.Env = append(
				os.Environ(),
				"PATH="+dir+":"+os.Getenv("PATH"),
				"CALL_LOG="+log,
				"TEST_NODES="+tc.nodes,
				"METRICS_OWNER="+tc.metricsOwner,
				"FAIL_COMMAND="+tc.fail,
				"DOCKER_CONTEXT=test-engine",
				"KIND_EXPERIMENTAL_PROVIDER=podman",
				"MAKEFLAGS=",
				"MFLAGS=",
			)
			out, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantError {
				t.Fatalf("exit: %v\n%s", err, out)
			}
			// #nosec G304 -- Path is derived from repository fixtures or a private test directory, not a remote request.
			calls, err := os.ReadFile(log)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			for _, line := range strings.Split(string(calls), "\n") {
				if !strings.HasPrefix(line, "helm pull ") {
					continue
				}
				fields := strings.Fields(line)
				archiveDir := fields[len(fields)-1]
				// #nosec G703 -- Path is derived from repository fixtures or a private test directory, not a remote request.
				if _, err := os.Stat(archiveDir); !os.IsNotExist(err) {
					t.Errorf("temporary chart directory was not removed: %v", err)
				}
			}
			for _, line := range strings.Split(string(calls), "\n") {
				if strings.HasPrefix(line, "kubectl ") &&
					!strings.Contains(line, "--kubeconfig kubeconfig --context kind-stacks-k8s") {
					t.Errorf("implicit Kubernetes context: %s", line)
				}
				if strings.HasPrefix(line, "helm ") && !strings.HasPrefix(line, "helm pull ") &&
					!strings.Contains(line, "--kubeconfig kubeconfig --kube-context kind-stacks-k8s") {
					t.Errorf("implicit Helm context: %s", line)
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(string(calls), want) {
					t.Errorf("missing %q in %s", want, calls)
				}
			}
			for _, reject := range tc.reject {
				if strings.Contains(string(calls), reject) {
					t.Errorf("unexpected %q in %s", reject, calls)
				}
			}
		})
	}
}
