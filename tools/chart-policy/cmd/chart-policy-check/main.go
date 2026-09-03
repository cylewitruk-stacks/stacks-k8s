// Command chart-policy-check validates the rendered operator workload policy.
package main

import (
	"fmt"
	"os"

	"github.com/cylewitruk-stacks/stacks-k8s/tools/chart-policy/internal/policy"
)

func main() {
	if err := policy.Validate(os.Stdin); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
