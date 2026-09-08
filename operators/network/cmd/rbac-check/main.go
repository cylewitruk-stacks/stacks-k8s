// Command rbac-check validates rendered chart permissions from standard input.
package main

import (
	"fmt"
	"os"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/rbac"
)

func main() {
	if err := rbac.Validate(os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
