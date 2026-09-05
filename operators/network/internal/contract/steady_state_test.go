package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertSupersededBitcoinMetadata validates authority before checking the historical payload.
func assertSupersededBitcoinMetadata(t *testing.T, fields map[string]json.RawMessage) {
	t.Helper()
	for key, want := range map[string]string{
		"designStatus":   "superseded-design-baseline",
		"sourceDocument": "docs/design/history/bitcoin-lifecycle-m0.4.md",
		"supersededBy":   "contracts/steady-state-operation-v1.json",
	} {
		var got string
		if err := json.Unmarshal(fields[key], &got); err != nil {
			t.Fatalf("historical Bitcoin metadata %s: %v", key, err)
		}
		if got != want {
			t.Fatalf("historical Bitcoin metadata %s = %q, want %q", key, got, want)
		}
		delete(fields, key)
	}
}

// TestSteadyStateDesignAuthority keeps reopened designs distinct from implementation contracts.
func TestSteadyStateDesignAuthority(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "contracts", "steady-state-operation-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"contract":                `"network.stacks.org/steady-state-operation/v1"`,
		"designStatus":            `"direction-agreed"`,
		"designDocument":          `"docs/design/steady-state-operation.md"`,
		"schemaFrozen":            "false",
		"implementationReady":     "false",
		"baselineOwner":           `"StacksNetwork"`,
		"bitcoinNodeMiningRole":   "false",
		"stacksNodeMiningRole":    "true",
		"globalReadinessRequired": "false",
		"overridesResume":         `"latest-baseline"`,
	} {
		if got := string(fields[key]); got != want {
			t.Errorf("current design %s = %s, want %s", key, got, want)
		}
	}
	for name, expected := range map[string]map[string]string{
		"bitcoinProduction": {
			"resource":                         `"BitcoinBlockProduction"`,
			"timingAndTargetSelectionSeparate": "true",
			"multipleReferencedTargets":        "true",
			"silentWeightRedistribution":       "false",
			"centralControlPathRequired":       "true",
			"autonomousLocalFallback":          "false",
		},
		"transactionProduction": {
			"workingKind":                       `"StacksTransactionProduction"`,
			"offeredRateNotConfirmedThroughput": "true",
			"nonceOwnershipRequired":            "true",
			"signingAuthorityIsolationRequired": "true",
		},
	} {
		var actual map[string]json.RawMessage
		if err := json.Unmarshal(fields[name], &actual); err != nil {
			t.Fatalf("current design %s: %v", name, err)
		}
		if len(actual) != len(expected) {
			t.Errorf("current design %s has %d fields, want %d", name, len(actual), len(expected))
		}
		for key, want := range expected {
			if got := string(actual[key]); got != want {
				t.Errorf("current design %s.%s = %s, want %s", name, key, got, want)
			}
		}
	}
	var historical []string
	if err := json.Unmarshal(fields["supersededFixtures"], &historical); err != nil {
		t.Fatal(err)
	}
	assertStringsEqual(t, "superseded fixtures", historical, []string{
		"bitcoin-actions-v1.json", "bitcoin-block-production-v1.json", "bitcoin-reservation-v1.json",
	})
	for _, name := range historical {
		content, err := os.ReadFile(filepath.Join(root, "contracts", name))
		if err != nil {
			t.Fatal(err)
		}
		var previous map[string]json.RawMessage
		if err := json.Unmarshal(content, &previous); err != nil {
			t.Fatal(err)
		}
		assertSupersededBitcoinMetadata(t, previous)
	}
	var gates []string
	if err := json.Unmarshal(fields["openReviewGates"], &gates); err != nil {
		t.Fatal(err)
	}
	assertStringsEqual(t, "open review gates", gates, []string{"R1", "R2", "R3", "R4", "R5", "R6", "R7", "R8"})
	document, err := os.ReadFile(filepath.Join(root, "docs", "design", "steady-state-operation.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range gates {
		if !strings.Contains(string(document), "| "+gate+" |") {
			t.Errorf("current design omits review gate %s", gate)
		}
	}
	plan, err := os.ReadFile(filepath.Join(root, "docs", "design", "m0-remediation-plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, slice := range []string{"M0.3", "M0.4"} {
		if !strings.Contains(string(plan), "| "+slice+" | Reopened |") {
			t.Errorf("M0 plan does not mark %s reopened", slice)
		}
	}
}
