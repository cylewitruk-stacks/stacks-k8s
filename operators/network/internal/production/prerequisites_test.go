package production

import (
	"errors"
	"strings"
	"testing"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestActionPrerequisitesRequireOnlyEnabledServedVersions enforces the selected API versions and flags.
func TestActionPrerequisitesRequireOnlyEnabledServedVersions(t *testing.T) {
	for _, tc := range []struct {
		name, missing              string
		generation, reorganization bool
		installed                  []string
	}{
		{name: "baseline has no dependency"},
		{name: "generation missing", generation: true, missing: "bitcoin-generation-enabled"},
		{name: "reorganization missing", reorganization: true, missing: "bitcoin-reorganization-enabled"},
		{name: "generation only", generation: true, installed: []string{"BitcoinBlockGeneration"}},
		{name: "reorganization only", reorganization: true, installed: []string{"BitcoinReorganization"}},
		{name: "both require both", generation: true, reorganization: true, installed: []string{"BitcoinBlockGeneration"}, missing: "bitcoin-reorganization-enabled"},
		{name: "both available", generation: true, reorganization: true, installed: []string{"BitcoinBlockGeneration", "BitcoinReorganization"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{actionv1.GroupVersion})
			// A different served version must not satisfy this consumer's contract.
			mapper.Add(schema.GroupVersionKind{Group: actionv1.GroupVersion.Group, Version: "v1beta1", Kind: "BitcoinBlockGeneration"}, meta.RESTScopeNamespace)
			for _, kind := range tc.installed {
				mapper.Add(actionv1.GroupVersion.WithKind(kind), meta.RESTScopeNamespace)
			}
			err := (&Reconciler{ActionsEnabled: tc.generation, ReorganizationEnabled: tc.reorganization}).checkActionAPIs(mapper)
			if tc.missing == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !meta.IsNoMatchError(err) || !strings.Contains(err.Error(), tc.missing) || !strings.Contains(err.Error(), "stacks-action-operator chart CRDs") {
				t.Fatalf("missing actionable prerequisite error: %v", err)
			}
		})
	}
}

// failedDiscovery preserves a discovery failure distinct from an absent API.
type failedDiscovery struct {
	meta.RESTMapper
	err error
}

// RESTMapping returns the injected discovery failure.
func (m failedDiscovery) RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error) {
	return nil, m.err
}

// TestActionPrerequisitesPreserveDiscoveryFailure keeps transport/discovery errors distinguishable.
func TestActionPrerequisitesPreserveDiscoveryFailure(t *testing.T) {
	cause := errors.New("discovery unavailable")
	mapper := failedDiscovery{err: cause}
	if err := (&Reconciler{}).checkActionAPIs(mapper); err != nil {
		t.Fatalf("disabled actions performed discovery: %v", err)
	}
	err := (&Reconciler{ActionsEnabled: true}).checkActionAPIs(mapper)
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "install the") {
		t.Fatalf("discovery failure misreported as missing CRD: %v", err)
	}
}
