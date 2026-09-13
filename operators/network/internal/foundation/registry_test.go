package foundation

import (
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
)

func TestRegistryInitializationMatchesPinnedQuorum(t *testing.T) {
	for _, test := range []struct {
		count, threshold int
		valid            bool
	}{{1, 1, false}, {2, 1, false}, {2, 2, true}, {3, 2, true}, {4, 2, false}, {4, 3, true}, {2, 3, false}} {
		in := &stacks.RegistryInitialization{
			Mode:              "ExplicitTestRegistry",
			SignerAccountRefs: make([]common.NameRef, test.count),
			// #nosec G115 -- Small deterministic fixture counters/values are bounded by the test setup.
			Threshold: int32(test.threshold),
		}
		if err := validateRegistryInitialization(in); (err == nil) != test.valid {
			t.Fatalf("%d-of-%d: %v", test.threshold, test.count, err)
		}
	}
}
