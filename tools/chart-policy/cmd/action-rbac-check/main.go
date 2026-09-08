package main

import (
	"fmt"
	"os"

	"github.com/cylewitruk-stacks/stacks-k8s/tools/chart-policy/internal/actionrbac"
)

func main() {
	if err := actionrbac.Validate(os.Stdin); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
