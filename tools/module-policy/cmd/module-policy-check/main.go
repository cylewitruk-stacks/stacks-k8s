// Command module-policy-check rejects forbidden dependencies from a Go module graph.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/tools/module-policy/internal/policy"
)

func main() {
	module := flag.String("module", "", "module directory to inspect")
	portable := flag.Bool("portable", false, "also reject Kubernetes API and operator dependencies")
	flag.Parse()
	if *module == "" {
		fmt.Fprintln(os.Stderr, "--module is required")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	command := exec.CommandContext(ctx, "go", "list", "-mod=readonly", "-m", "all")
	command.Dir = *module
	command.Env = append(withoutVariable(os.Environ(), "GOWORK="), "GOWORK=off")
	output, err := command.CombinedOutput()
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "list module graph: %v: %s\n", err, output)
		os.Exit(1)
	}
	forbidden := []string{"k8s.io/client-go", "sigs.k8s.io/controller-runtime"}
	if *portable {
		forbidden = append(
			forbidden,
			"k8s.io",
			"github.com/cylewitruk-stacks/stacks-k8s/apis",
			"github.com/cylewitruk-stacks/stacks-k8s/operators",
		)
	}
	if err := policy.Validate(bytes.NewReader(output), forbidden); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func withoutVariable(environment []string, prefix string) []string {
	filtered := make([]string, 0, len(environment))
	for _, value := range environment {
		if len(value) >= len(prefix) && value[:len(prefix)] == prefix {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}
