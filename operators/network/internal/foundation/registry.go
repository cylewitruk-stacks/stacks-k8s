package foundation

import (
	"fmt"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
)

// validateRegistryInitialization matches the pinned bootstrap wrapper's key-count and quorum bounds.
func validateRegistryInitialization(in *stacks.RegistryInitialization) error {
	if in == nil || in.Mode != "ExplicitTestRegistry" || len(in.SignerAccountRefs) < 2 || len(in.SignerAccountRefs) > 100 || int(in.Threshold) <= len(in.SignerAccountRefs)/2 || int(in.Threshold) > len(in.SignerAccountRefs) {
		return fmt.Errorf("explicit registry requires at least two signers and a strict majority threshold")
	}
	return nil
}
