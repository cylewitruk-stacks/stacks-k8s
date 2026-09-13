// Command rbac-check validates the observability chart permissions.
package main

import (
	"fmt"
	"os"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/rbac"
)

func main() {
	if err := rbac.Validate(os.Stdin); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
