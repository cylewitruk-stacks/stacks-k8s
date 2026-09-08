// Package testsupport supplies synthetic artifacts for offline resource and SDK tests.
package testsupport

import (
	"crypto/sha256"
	"fmt"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
)

// ContractSources supplies named minimal sources, never a claim of sBTC protocol compatibility.
// Live qualification must use the independently checksum-verified upstream bundle.
func ContractSources() []protocolcontracts.Source {
	source := "(define-read-only (fixture) (ok true))"
	result := []protocolcontracts.Source{}
	for _, name := range []string{"sbtc-registry", "sbtc-token", "sbtc-bootstrap-signers", "sbtc-deposit", "sbtc-withdrawal"} {
		result = append(result, protocolcontracts.Source{Name: name, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source))), ClarityVersion: 3, Source: source})
	}
	return result
}
