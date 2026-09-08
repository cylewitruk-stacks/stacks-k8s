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
		name, target, nodes, fail string
		wantError                 bool
		want, reject              []string
	}{
		{name: "create", target: "create", want: []string{"kind create cluster --name stacks-k8s --config kind.yaml --kubeconfig kubeconfig --wait 120s"}, reject: []string{"docker ", "delete"}},
		{name: "existing create fails", target: "create", fail: "kind", wantError: true, reject: []string{"delete"}},
		{name: "start", target: "start", nodes: "a123\nb456\nc789", want: []string{"docker start a123 b456 c789", "kind export kubeconfig --name stacks-k8s --kubeconfig kubeconfig", "--context kind-stacks-k8s", "node/stacks-k8s-worker2"}, reject: []string{"create cluster", "delete", "docker stop"}},
		{name: "absent start", target: "start", wantError: true, reject: []string{"docker start", "kind ", "kubectl "}},
		{name: "docker read failure", target: "stop", fail: "docker", wantError: true, reject: []string{"docker stop", "kind "}},
		{name: "stop", target: "stop", nodes: "a123\nb456", want: []string{"docker stop --timeout 30 a123 b456"}, reject: []string{"kind ", "kubectl "}},
		{name: "absent stop", target: "stop", reject: []string{"docker stop", "kind ", "kubectl "}},
		{name: "export failure", target: "start", nodes: "a123", fail: "kind", wantError: true, reject: []string{"kubectl "}},
		{name: "chaos install", target: "chaos-install", want: []string{"helm pull chaos-mesh --repo https://charts.chaos-mesh.org --version 2.8.4", "helm upgrade --install chaos-mesh", "--kubeconfig kubeconfig --kube-context kind-stacks-k8s", "--namespace chaos-mesh --create-namespace --reset-values", "--values ../../examples/chaos/upstream-values.yaml --wait --timeout 5m"}, reject: []string{"docker ", "kind ", "kubectl "}},
		{name: "chart download failure", target: "chaos-install", fail: "helm", wantError: true, reject: []string{"helm upgrade"}},
		{name: "chart checksum failure", target: "chaos-install", fail: "checksum", wantError: true, reject: []string{"helm upgrade"}},
		{name: "destroy", target: "destroy", want: []string{"kind delete cluster --name stacks-k8s --kubeconfig kubeconfig"}, reject: []string{"docker ", "create cluster"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source, err := os.ReadFile("Makefile")
			if err != nil {
				t.Fatal(err)
			}
			// Replace only the archive digest to exercise real shasum with a tiny fixture.
			source = []byte(strings.ReplaceAll(string(source), "ae4abd385649771300e4d33a44627c0df3618be0780c385bf30cc2fdf2ad93fa", fmt.Sprintf("%x", sha256.Sum256([]byte("test chart")))))
			if err := os.WriteFile(filepath.Join(dir, "Makefile"), source, 0600); err != nil {
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
if test "$command" = helm && test "$1" = pull; then
 while test "$1" != --destination; do shift; done
 if test "$FAIL_COMMAND" = checksum; then
  printf tampered > "$2/chaos-mesh-2.8.4.tgz"
 else
  printf 'test chart' > "$2/chaos-mesh-2.8.4.tgz"
 fi
fi
if test "$command" = docker && test "$1" = ps; then
 test "$*" = "ps -a --filter label=io.x-k8s.kind.cluster=stacks-k8s --format {{.ID}}"
 printf '%s\n' "$TEST_NODES"
fi
`
			for _, name := range []string{"docker", "kind", "kubectl", "helm"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(fake), 0700); err != nil {
					t.Fatal(err)
				}
			}
			log := filepath.Join(dir, "calls")
			cmd := exec.Command("make", "--no-print-directory", "-C", dir, tc.target)
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "CALL_LOG="+log, "TEST_NODES="+tc.nodes, "FAIL_COMMAND="+tc.fail, "DOCKER_CONTEXT=test-engine", "KIND_EXPERIMENTAL_PROVIDER=podman", "MAKEFLAGS=", "MFLAGS=")
			out, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantError {
				t.Fatalf("exit: %v\n%s", err, out)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if tc.target == "chaos-install" {
				fields := strings.Fields(strings.SplitN(string(calls), "\n", 2)[0])
				archiveDir := fields[len(fields)-1]
				if _, err := os.Stat(archiveDir); !os.IsNotExist(err) {
					t.Errorf("temporary chart directory was not removed: %v", err)
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
