package production

import (
	"fmt"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
)

// checkActionAPIs resolves enabled action watches before any workers are registered.
func (r *Reconciler) checkActionAPIs(mapper meta.RESTMapper) error {
	for _, action := range []struct {
		enabled bool
		kind    string
		flag    string
	}{
		{r.ActionsEnabled, "BitcoinBlockGeneration", "bitcoin-generation-enabled"},
		{r.ReorganizationEnabled, "BitcoinReorganization", "bitcoin-reorganization-enabled"},
	} {
		if !action.enabled {
			continue
		}
		gvk := actionv1.GroupVersion.WithKind(action.kind)
		if _, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			if meta.IsNoMatchError(err) {
				return fmt.Errorf("--%s requires %s %s; install the stacks-action-operator chart CRDs before enabling this flag: %w", action.flag, actionv1.GroupVersion, action.kind, err)
			}
			return fmt.Errorf("resolve --%s API prerequisite %s: %w", action.flag, gvk, err)
		}
	}
	return nil
}
