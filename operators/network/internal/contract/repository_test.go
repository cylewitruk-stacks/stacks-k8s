// Package contract verifies repository-level product boundaries and examples.
package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/network"
)

func TestExamplesCompile(t *testing.T) {
	root := repositoryRoot(t)
	entries, err := filepath.Glob(filepath.Join(root, "examples", "network", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("example count = %d", len(entries))
	}
	for _, path := range entries {
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			value := &networkv1alpha1.StacksNetwork{}
			if err := yaml.UnmarshalStrict(content, value); err != nil {
				t.Fatal(err)
			}
			if value.APIVersion != networkv1alpha1.GroupVersion.String() || value.Kind != "StacksNetwork" {
				t.Fatalf("unexpected type %s %s", value.APIVersion, value.Kind)
			}
			if _, err := network.Compile(value); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMarkdownLinksResolve(t *testing.T) {
	root := repositoryRoot(t)
	link := regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			// Installed npm packages are external artifacts, not repository documentation.
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range link.FindAllStringSubmatch(string(content), -1) {
			target := strings.Split(match[1], "#")[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), filepath.FromSlash(target))); err != nil {
				t.Errorf("%s: unresolved link %q", path, match[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLeafCRDSchemasDoNotDeclareDefaults(t *testing.T) {
	root := repositoryRoot(t)
	files := []string{
		"network.stacks.org_bitcoinnodes.yaml",
		"network.stacks.org_stacksnodes.yaml",
		"network.stacks.org_stackssigners.yaml",
	}
	checkedSchemas := 0
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(root, "charts", "stacks-network-operator", "crds", name))
			if err != nil {
				t.Fatal(err)
			}
			crd := &apiextensionsv1.CustomResourceDefinition{}
			if err := yaml.UnmarshalStrict(content, crd); err != nil {
				t.Fatal(err)
			}
			if crd.Kind != "CustomResourceDefinition" || len(crd.Spec.Versions) == 0 {
				t.Fatalf("%s is not a versioned CustomResourceDefinition", name)
			}
			for _, version := range crd.Spec.Versions {
				if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
					t.Fatalf("%s/%s has no structural schema", name, version.Name)
				}
				checkedSchemas++
				encoded, err := json.Marshal(version.Schema.OpenAPIV3Schema)
				if err != nil {
					t.Fatal(err)
				}
				var schema any
				if err := json.Unmarshal(encoded, &schema); err != nil {
					t.Fatal(err)
				}
				assertNoSchemaDefault(t, schema, "$")
			}
		})
	}
	if checkedSchemas != len(files) {
		t.Fatalf("checked %d schemas, want %d", checkedSchemas, len(files))
	}
}

func assertNoSchemaDefault(t *testing.T, value any, path string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "default" {
				t.Fatalf("leaf CRD schema declares an API-server default at %s.default", path)
			}
			assertNoSchemaDefault(t, child, path+"."+key)
		}
	case []any:
		for index, child := range typed {
			assertNoSchemaDefault(t, child, path+"["+strconv.Itoa(index)+"]")
		}
	}
}

func TestRepositoryLayout(t *testing.T) {
	root := repositoryRoot(t)
	required := []string{
		"apis/network/go.mod",
		"apis/network/tools/go.mod",
		"charts/stacks-network-operator/Chart.yaml",
		"charts/stacks-observability-operator/Chart.yaml",
		"contracts/action-lifecycle-v1.json",
		"contracts/actor-ports-v1.json",
		"contracts/bitcoin-actions-v1.json",
		"contracts/bitcoin-block-production-v1.json",
		"contracts/bitcoin-reservation-v1.json",
		"contracts/image-id-v1.json",
		"contracts/inventory-v1.json",
		"contracts/leaf-spec-v1.json",
		"contracts/steady-state-operation-v1.json",
		"docs/design/steady-state-operation.md",
		"docs/design/history/bitcoin-lifecycle-m0.4.md",
		"operators/network/go.mod",
		"operators/observability/go.mod",
	}
	for _, name := range required {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Errorf("required repository path %s: %v", name, err)
		}
	}
	retired := []string{
		"operators/network/api",
		"operators/network/tools",
		"stacks-network-operator",
		"stacks-observability-operator",
	}
	for _, name := range retired {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("retired root-level component path still exists: %s", name)
		}
	}
}

func TestActionLifecycleDesignContract(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "contracts", "action-lifecycle-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		APIGroup             string   `json:"apiGroup"`
		APIVersion           string   `json:"apiVersion"`
		ConditionTypes       []string `json:"conditionTypes"`
		Contract             string   `json:"contract"`
		CoreSpecFields       []string `json:"coreSpecFields"`
		CoreStatusFields     []string `json:"coreStatusFields"`
		CorrelationLabel     string   `json:"correlationLabel"`
		Finalizer            string   `json:"finalizer"`
		ForbiddenSpecFields  []string `json:"forbiddenSpecFields"`
		Phases               []string `json:"phases"`
		ReasonValues         []string `json:"reasonValues"`
		SpecImmutabilityRule string   `json:"specImmutabilityRule"`
		TerminalPhases       []string `json:"terminalPhases"`
		TimeoutField         struct {
			FiniteMaximumRequired bool   `json:"finiteMaximumRequired"`
			OpenAPIFormatAllowed  bool   `json:"openAPIFormatAllowed"`
			PositiveCEL           string `json:"positiveCEL"`
			WireFormat            string `json:"wireFormat"`
			WireType              string `json:"wireType"`
		} `json:"timeoutField"`
		TimestampFields []string `json:"timestampFields"`
	}
	if err := json.Unmarshal(content, &contract); err != nil {
		t.Fatal(err)
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(content, &rawFields); err != nil {
		t.Fatal(err)
	}
	fieldNames := make([]string, 0, len(rawFields))
	for name := range rawFields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	assertStringsEqual(t, "contract fields", fieldNames, []string{"apiGroup", "apiVersion", "conditionTypes", "contract", "coreSpecFields", "coreStatusFields", "correlationLabel", "finalizer", "forbiddenSpecFields", "phases", "reasonValues", "specImmutabilityRule", "terminalPhases", "timeoutField", "timestampFields"})
	assertStringsEqual(t, "phases", contract.Phases, []string{"Pending", "Admitted", "Active", "Recovering", "Completed", "Recovered", "Failed", "Inconclusive"})
	assertStringsEqual(t, "terminal phases", contract.TerminalPhases, []string{"Completed", "Recovered", "Failed", "Inconclusive"})
	assertStringsEqual(t, "condition types", contract.ConditionTypes, []string{"Admitted", "Progressing", "EffectObserved", "CleanupComplete"})
	assertStringsEqual(t, "core spec fields", contract.CoreSpecFields, []string{"networkRef", "timeout"})
	assertStringsEqual(t, "core status fields", contract.CoreStatusFields, []string{"observedGeneration", "phase", "admittedAt", "startedAt", "expiresAt", "finishedAt", "correlationID", "admittedNetwork", "admittedTarget", "admittedPolicy", "conditions"})
	assertStringsEqual(t, "forbidden spec fields", contract.ForbiddenSpecFields, []string{"actions", "dependsOn", "executionPlan", "reduction", "replay", "scenario", "schedule", "stages", "steps", "workflow"})
	assertStringsEqual(t, "reason values", contract.ReasonValues, []string{"TargetNotReady", "TargetBusy", "PolicyUnavailable", "AdmissionSucceeded", "ActionApplying", "EffectConfirmed", "RecoveryInProgress", "ActionCompleted", "ActionRecovered", "RequestInvalid", "IdentityDiverged", "DeadlineExceeded", "MechanismFailed", "EffectUncertain", "CleanupUncertain"})
	assertStringsEqual(t, "timestamp fields", contract.TimestampFields, []string{"admittedAt", "startedAt", "expiresAt", "finishedAt"})
	if contract.Contract != "actions.stacks.org/lifecycle/v1alpha1" || contract.APIGroup != "actions.stacks.org" || contract.APIVersion != "v1alpha1" {
		t.Fatalf("unexpected action API contract identity: %#v", contract)
	}
	if contract.CorrelationLabel != "actions.stacks.org/correlation-id" || contract.Finalizer != "actions.stacks.org/action-cleanup" || contract.SpecImmutabilityRule != "self == oldSelf" {
		t.Fatalf("unexpected action lifecycle convention: %#v", contract)
	}
	if contract.TimeoutField.WireType != "string" || contract.TimeoutField.WireFormat != "kubernetes-duration" || contract.TimeoutField.PositiveCEL != "duration(self) > duration('0s')" || !contract.TimeoutField.FiniteMaximumRequired || contract.TimeoutField.OpenAPIFormatAllowed {
		t.Fatalf("unexpected timeout field contract: %#v", contract.TimeoutField)
	}

	documentBytes, err := os.ReadFile(filepath.Join(root, "docs", "design", "actions.md"))
	if err != nil {
		t.Fatal(err)
	}
	document := string(documentBytes)
	documentedValues := make([]string, 0, len(contract.Phases)+len(contract.ConditionTypes)+len(contract.CoreSpecFields)+len(contract.CoreStatusFields)+len(contract.ForbiddenSpecFields)+len(contract.ReasonValues)+len(contract.TimestampFields))
	documentedValues = append(documentedValues, contract.Phases...)
	documentedValues = append(documentedValues, contract.ConditionTypes...)
	documentedValues = append(documentedValues, contract.CoreSpecFields...)
	documentedValues = append(documentedValues, contract.CoreStatusFields...)
	documentedValues = append(documentedValues, contract.ForbiddenSpecFields...)
	documentedValues = append(documentedValues, contract.ReasonValues...)
	documentedValues = append(documentedValues, contract.TimestampFields...)
	for _, value := range documentedValues {
		if !strings.Contains(document, "`"+value+"`") {
			t.Errorf("normative action document does not represent %q", value)
		}
	}
	for _, value := range []string{contract.Contract, contract.CorrelationLabel, contract.Finalizer, contract.SpecImmutabilityRule, contract.TimeoutField.WireType, contract.TimeoutField.WireFormat, contract.TimeoutField.PositiveCEL} {
		if !strings.Contains(document, value) {
			t.Errorf("normative action document does not represent %q", value)
		}
	}
	for _, convention := range []string{"metav1.Duration", "finite upper bound", "No lifecycle fact may exist only in `phase`"} {
		if !strings.Contains(document, convention) {
			t.Errorf("normative action document does not represent %q", convention)
		}
	}

	for _, name := range []string{"actions.md", "bitcoin-lifecycle.md", "protocol-actions.md", "steady-state-operation.md"} {
		value, err := os.ReadFile(filepath.Join(root, "docs", "design", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, stale := range []string{"immutable after admission", "become immutable after", "becomes immutable after"} {
			if strings.Contains(strings.ToLower(string(value)), stale) {
				t.Errorf("%s retains superseded mutability phrase %q", name, stale)
			}
		}
	}

	bitcoinBytes, err := os.ReadFile(filepath.Join(root, "docs", "design", "bitcoin-lifecycle.md"))
	if err != nil {
		t.Fatal(err)
	}
	bitcoinDocument := strings.Join(strings.Fields(string(bitcoinBytes)), " ")
	for _, stale := range []string{
		"rpcProfileRef", "RPC RPC",
		"require a complete, current admitted inventory and a Ready `BitcoinNode`",
		"require the leaf role to be `miner`",
	} {
		if strings.Contains(bitcoinDocument, stale) {
			t.Errorf("bitcoin action design retains superseded text %q", stale)
		}
	}
}

func TestHistoricalBitcoinActionDesignContract(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "contracts", "bitcoin-actions-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	type kindContract struct {
		Kind         string   `json:"kind"`
		RPCMethods   []string `json:"rpcMethods"`
		SpecFields   []string `json:"specFields"`
		StatusFields []string `json:"statusFields"`
	}
	var contract struct {
		AmbiguousMutationOutcome   string   `json:"ambiguousMutationOutcome"`
		APIGroup                   string   `json:"apiGroup"`
		APIVersion                 string   `json:"apiVersion"`
		AttributionValues          []string `json:"attributionValues"`
		AttemptOutcomes            []string `json:"attemptOutcomeValues"`
		BoundaryRange              string   `json:"boundaryAssessmentRange"`
		BoundaryFields             []string `json:"boundaryPolicyFields"`
		ChainFields                []string `json:"chainIdentityFields"`
		CompletedAttributionSource string   `json:"completedAttributionSource"`
		Contract                   string   `json:"contract"`
		Credential                 struct {
			AuthenticationMode      string   `json:"authenticationMode"`
			ExpectedDigestAlgorithm string   `json:"expectedDigestAlgorithm"`
			ExpectedDigestFormat    string   `json:"expectedDigestFormat"`
			Key                     string   `json:"key"`
			PasswordRandomBytes     int      `json:"passwordRandomBytes"`
			RPCAuthSaltRandomBytes  int      `json:"rpcauthSaltRandomBytes"`
			SecretName              string   `json:"secretName"`
			ServerConfigFields      []string `json:"serverConfigFields"`
			SecretRequiredImmutable bool     `json:"secretRequiredImmutable"`
			ValueEncoding           string   `json:"valueEncoding"`
			UsernameRandomBytes     int      `json:"usernameRandomBytes"`
		} `json:"credential"`
		DestinationUniqueness string `json:"destinationUniqueness"`
		GenerationCadence     struct {
			DelayAppliesBefore            string   `json:"delayAppliesBefore"`
			FixedFields                   []string `json:"fixedFields"`
			ImmediateBatchWithinBlocksCEL string   `json:"immediateBatchWithinBlocksCEL"`
			ImmediateFields               []string `json:"immediateFields"`
			IntervalCEL                   string   `json:"intervalCEL"`
			Modes                         []string `json:"modes"`
			OneOfCEL                      string   `json:"oneOfCEL"`
			ScheduledBatchSize            int      `json:"scheduledBatchSize"`
			SequenceFields                []string `json:"sequenceFields"`
			SequenceLengthCEL             string   `json:"sequenceLengthCEL"`
			SequenceItemsMaximum          int      `json:"sequenceItemsMaximum"`
			StatusFields                  []string `json:"statusFields"`
			UniformFields                 []string `json:"uniformFields"`
			UniformOrderCEL               string   `json:"uniformOrderCEL"`
			UniformReplayClaim            bool     `json:"uniformReplayClaim"`
			UniformSelection              string   `json:"uniformSelection"`
		} `json:"generationCadence"`
		GetBlockVerbosity int            `json:"getBlockVerbosity"`
		Kinds             []kindContract `json:"kinds"`
		Limits            struct {
			BatchSizeMaximum                int    `json:"batchSizeMaximum"`
			BlockGenerationMaximum          int    `json:"blockGenerationMaximum"`
			DestinationAddressMaximumLength int    `json:"destinationAddressMaximumLength"`
			DestinationAddressMinimumLength int    `json:"destinationAddressMinimumLength"`
			IntervalMaximum                 string `json:"intervalMaximum"`
			IntervalMinimum                 string `json:"intervalMinimum"`
			ReorganizationDepthMaximum      int    `json:"reorganizationDepthMaximum"`
			ReplacementBlocksMaximum        int    `json:"replacementBlocksMaximum"`
			RPCAttemptsMaximum              int    `json:"rpcAttemptsMaximum"`
			TimeoutMaximum                  string `json:"timeoutMaximum"`
		} `json:"limits"`
		ReservationContract string `json:"reservationContract"`
		ProvenAbsentScope   string `json:"provenAbsentScope"`
		ProtocolSchedule    struct {
			UnknownRequiresAllBoundaryOptIns bool   `json:"unknownRequiresAllBoundaryOptIns"`
			V1Source                         string `json:"v1Source"`
		} `json:"protocolSchedule"`
		RPCAttemptScope      string   `json:"rpcAttemptScope"`
		RPCAttemptFields     []string `json:"rpcAttemptFields"`
		SpecImmutabilityRule string   `json:"specImmutabilityRule"`
		TopologyDependency   struct {
			AffectedContracts      []string `json:"affectedContracts"`
			AggregateField         string   `json:"aggregateField"`
			CompiledLeafField      string   `json:"compiledLeafField"`
			FieldType              string   `json:"fieldType"`
			GeneratedProfiles      []string `json:"generatedProfiles"`
			Key                    string   `json:"key"`
			Name                   string   `json:"name"`
			RequiredExpectedDigest bool     `json:"requiredExpectedDigest"`
		} `json:"topologyDependency"`
	}
	if err := json.Unmarshal(content, &contract); err != nil {
		t.Fatal(err)
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(content, &rawFields); err != nil {
		t.Fatal(err)
	}
	assertSupersededBitcoinMetadata(t, rawFields)
	fieldNames := make([]string, 0, len(rawFields))
	for name := range rawFields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	assertStringsEqual(t, "Bitcoin contract fields", fieldNames, []string{"ambiguousMutationOutcome", "apiGroup", "apiVersion", "attemptOutcomeValues", "attributionValues", "boundaryAssessmentRange", "boundaryPolicyFields", "chainIdentityFields", "completedAttributionSource", "contract", "credential", "destinationUniqueness", "generationCadence", "getBlockVerbosity", "kinds", "limits", "protocolSchedule", "provenAbsentScope", "reservationContract", "rpcAttemptFields", "rpcAttemptScope", "specImmutabilityRule", "topologyDependency"})
	if contract.Contract != "actions.stacks.org/bitcoin-actions/v1alpha1" || contract.APIGroup != "actions.stacks.org" || contract.APIVersion != "v1alpha1" || contract.SpecImmutabilityRule != "self == oldSelf" {
		t.Fatalf("unexpected Bitcoin action contract identity: %#v", contract)
	}
	assertStringsEqual(t, "attribution values", contract.AttributionValues, []string{"RPCResponse", "Uncertain"})
	assertStringsEqual(t, "attempt outcomes", contract.AttemptOutcomes, []string{"IntentRecorded", "Acknowledged", "ProvenAbsent", "Ambiguous"})
	assertStringsEqual(t, "boundary fields", contract.BoundaryFields, []string{"allowEpochBoundaryCrossing", "allowRewardCycleBoundaryCrossing", "allowPreparePhaseBoundaryCrossing"})
	assertStringsEqual(t, "chain fields", contract.ChainFields, []string{"height", "bestBlockHash", "chainwork"})
	if contract.BoundaryRange != "inclusive:[originalHeight-depth+1,originalHeight-depth+replacementBlocks]" || contract.RPCAttemptScope != "mutation-methods-only" {
		t.Fatalf("unexpected Bitcoin action boundary or RPC attempt scope: %#v", contract)
	}
	if contract.AmbiguousMutationOutcome != "Inconclusive" || contract.ProvenAbsentScope != "all-known-tips" ||
		contract.CompletedAttributionSource != "rpc-response-block-hashes-only" ||
		contract.DestinationUniqueness != "advisory" || contract.GetBlockVerbosity != 2 {
		t.Fatalf("unexpected ambiguous-effect contract: %#v", contract)
	}
	if contract.ProtocolSchedule.V1Source != "none" || !contract.ProtocolSchedule.UnknownRequiresAllBoundaryOptIns {
		t.Fatalf("unexpected protocol schedule contract: %#v", contract.ProtocolSchedule)
	}
	assertStringsEqual(t, "RPC attempt fields", contract.RPCAttemptFields, []string{"sequence", "method", "expectedHeight", "expectedTip", "requestedBlocks", "destinationAddress", "acquisitionToken", "startedAt", "deadline", "outcome", "blockHashes"})
	if contract.ReservationContract != "bitcoin.stacks.org/reservation/v1alpha1" ||
		contract.GenerationCadence.DelayAppliesBefore != "every-block-including-first" ||
		contract.GenerationCadence.ScheduledBatchSize != 1 ||
		contract.GenerationCadence.OneOfCEL != "[has(self.immediate), has(self.fixed), has(self.uniform), has(self.sequence)].filter(x, x).size() == 1" ||
		contract.GenerationCadence.ImmediateBatchWithinBlocksCEL != "!has(self.cadence.immediate) || self.cadence.immediate.batchSize <= self.blocks" ||
		contract.GenerationCadence.IntervalCEL != "duration(self) >= duration('100ms') && duration(self) <= duration('5m')" ||
		contract.GenerationCadence.SequenceLengthCEL != "!has(self.cadence.sequence) || self.cadence.sequence.delays.size() == self.blocks" ||
		contract.GenerationCadence.SequenceItemsMaximum != 288 ||
		contract.GenerationCadence.UniformOrderCEL != "duration(self.minimumInterval) <= duration(self.maximumInterval)" ||
		contract.GenerationCadence.UniformReplayClaim ||
		contract.GenerationCadence.UniformSelection != "crypto-rand-unbiased-inclusive-integer-nanoseconds" {
		t.Fatalf("unexpected generation cadence contract: %#v", contract.GenerationCadence)
	}
	assertStringsEqual(t, "generation cadence modes", contract.GenerationCadence.Modes, []string{"immediate", "fixed", "uniform", "sequence"})
	assertStringsEqual(t, "immediate cadence fields", contract.GenerationCadence.ImmediateFields, []string{"batchSize"})
	assertStringsEqual(t, "fixed cadence fields", contract.GenerationCadence.FixedFields, []string{"interval"})
	assertStringsEqual(t, "uniform cadence fields", contract.GenerationCadence.UniformFields, []string{"minimumInterval", "maximumInterval"})
	assertStringsEqual(t, "sequence cadence fields", contract.GenerationCadence.SequenceFields, []string{"delays"})
	assertStringsEqual(t, "generation cadence status fields", contract.GenerationCadence.StatusFields, []string{"mode", "selectedInterval", "scheduledForBlock", "dueAt"})
	if len(contract.Kinds) != 2 {
		t.Fatalf("Bitcoin action kind count = %d, want 2", len(contract.Kinds))
	}
	assertBitcoinKindContract(t, contract.Kinds[0], "BitcoinBlockGeneration",
		[]string{"networkRef", "bitcoinNodeRef", "blocks", "cadence", "destinationAddress", "timeout"},
		[]string{"requestedBlocks", "generatedBlocks", "generatedBlockHashes", "cadence", "startingChain", "observedChain", "reservation", "rpcAttempts", "attribution"},
		[]string{"getblockchaininfo", "validateaddress", "generatetoaddress", "getblockhash", "getblockheader", "getblock", "getchaintips"})
	assertBitcoinKindContract(t, contract.Kinds[1], "BitcoinReorganization",
		[]string{"networkRef", "bitcoinNodeRef", "depth", "replacementBlocks", "replacementInterval", "destinationAddress", "boundaryPolicy", "timeout"},
		[]string{"originalChain", "forkParent", "originalBlockHashes", "replacementBlockHashes", "finalChain", "boundaryAssessment", "reservation", "rpcAttempts", "attribution"},
		[]string{"getblockchaininfo", "validateaddress", "getblockhash", "getblockheader", "getblock", "getchaintips", "invalidateblock", "generatetoaddress", "reconsiderblock"})
	if contract.Limits.BatchSizeMaximum != 16 || contract.Limits.BlockGenerationMaximum != 288 ||
		contract.Limits.DestinationAddressMinimumLength != 14 || contract.Limits.DestinationAddressMaximumLength != 90 ||
		contract.Limits.IntervalMinimum != "100ms" || contract.Limits.IntervalMaximum != "5m" ||
		contract.Limits.ReorganizationDepthMaximum != 144 || contract.Limits.ReplacementBlocksMaximum != 288 ||
		contract.Limits.RPCAttemptsMaximum != 320 || contract.Limits.TimeoutMaximum != "24h" {
		t.Fatalf("unexpected Bitcoin action limits: %#v", contract.Limits)
	}
	if contract.Credential.AuthenticationMode != "rpcauth" || contract.Credential.SecretName != "stacks-bitcoin-rpc" ||
		contract.Credential.Key != "bitcoin-rpc.conf" || contract.Credential.ExpectedDigestAlgorithm != "sha256" ||
		contract.Credential.ExpectedDigestFormat != "sha256:<64-lowercase-hex>" ||
		contract.Credential.UsernameRandomBytes != 24 || contract.Credential.PasswordRandomBytes != 32 ||
		contract.Credential.RPCAuthSaltRandomBytes != 16 || contract.Credential.ValueEncoding != "base64url-without-padding" ||
		!contract.Credential.SecretRequiredImmutable {
		t.Fatalf("unexpected Bitcoin credential contract: %#v", contract.Credential)
	}
	assertStringsEqual(t, "server config fields", contract.Credential.ServerConfigFields, []string{"rpcauth"})
	if contract.TopologyDependency.AggregateField != "spec.bitcoinRPCAuth" ||
		contract.TopologyDependency.CompiledLeafField != "spec.bitcoinRPCAuth" ||
		contract.TopologyDependency.FieldType != "ConfigObjectRef" ||
		contract.TopologyDependency.Name != "stacks-bitcoin-rpc" ||
		contract.TopologyDependency.Key != "bitcoin-rpc.conf" ||
		!contract.TopologyDependency.RequiredExpectedDigest {
		t.Fatalf("unexpected Bitcoin topology dependency: %#v", contract.TopologyDependency)
	}
	assertStringsEqual(t, "generated profiles", contract.TopologyDependency.GeneratedProfiles, []string{"bitcoin-regtest/v2", "nakamoto-regtest-node/v2"})
	assertStringsEqual(t, "affected contracts", contract.TopologyDependency.AffectedContracts, []string{"leaf-spec-v1.json", "inventory-v1.json"})
	reservation := loadBitcoinReservationContract(t, root)
	if contract.ReservationContract != reservation.Contract {
		t.Fatalf("action reservation contract = %q, want %q", contract.ReservationContract, reservation.Contract)
	}

	documentBytes, err := os.ReadFile(filepath.Join(root, "docs", "design", "history", "bitcoin-lifecycle-m0.4.md"))
	if err != nil {
		t.Fatal(err)
	}
	document := string(documentBytes)
	for _, value := range []string{
		contract.Contract, contract.Credential.AuthenticationMode, contract.Credential.SecretName, contract.Credential.Key,
		reservation.Contract, reservation.NameAlgorithm, reservation.HolderIdentity,
		reservation.ManagerLifecycle, reservation.ObservedExpiry, reservation.RPCContextParent,
		reservation.WriteOwner, contract.TopologyDependency.AggregateField, contract.TopologyDependency.CompiledLeafField,
		contract.TopologyDependency.FieldType,
		contract.TopologyDependency.GeneratedProfiles[0], contract.TopologyDependency.GeneratedProfiles[1],
		contract.TopologyDependency.AffectedContracts[0], contract.TopologyDependency.AffectedContracts[1],
		strconv.Itoa(reservation.LeaseDurationSeconds) + " seconds",
		strconv.Itoa(reservation.RenewEverySeconds) + " seconds",
		strconv.Itoa(reservation.MaximumRPCSeconds) + " seconds",
	} {
		if !strings.Contains(document, value) {
			t.Errorf("Bitcoin lifecycle document does not represent %q", value)
		}
	}
	for _, kind := range contract.Kinds {
		values := append(append(append([]string{kind.Kind}, kind.SpecFields...), kind.StatusFields...), kind.RPCMethods...)
		for _, value := range values {
			if !strings.Contains(document, "`"+value+"`") {
				t.Errorf("Bitcoin lifecycle document does not represent %s value %q", kind.Kind, value)
			}
		}
	}
	for _, value := range contract.AttributionValues {
		if !strings.Contains(document, "`"+value+"`") {
			t.Errorf("Bitcoin lifecycle document does not represent attribution %q", value)
		}
	}
	for _, values := range [][]string{contract.AttemptOutcomes, contract.BoundaryFields, contract.ChainFields, reservation.StatusFields, contract.RPCAttemptFields} {
		for _, value := range values {
			if !strings.Contains(document, "`"+value+"`") {
				t.Errorf("Bitcoin lifecycle document does not represent contract value %q", value)
			}
		}
	}
	for _, values := range [][]string{contract.GenerationCadence.Modes, contract.GenerationCadence.ImmediateFields, contract.GenerationCadence.FixedFields, contract.GenerationCadence.UniformFields, contract.GenerationCadence.SequenceFields, contract.GenerationCadence.StatusFields, reservation.Annotations, reservation.HolderKinds} {
		for _, value := range values {
			if !strings.Contains(document, "`"+value+"`") {
				t.Errorf("Bitcoin lifecycle document does not represent cadence or reservation value %q", value)
			}
		}
	}
	for _, value := range []string{"`DestinationRecovered`", "| `Recovered` | A lost response"} {
		if strings.Contains(document, value) {
			t.Errorf("Bitcoin lifecycle document retains removed recovery contract %q", value)
		}
	}
	for _, value := range []string{
		"V1 has no trusted protocol-schedule source",
		"NeedLeaderElection",
		"every known tip",
		"verbosity `2`",
	} {
		if !strings.Contains(document, value) {
			t.Errorf("Bitcoin lifecycle document does not represent remediated invariant %q", value)
		}
	}
}

type bitcoinReservationContract struct {
	AcquisitionTokenBytes int      `json:"acquisitionTokenBytes"`
	Annotations           []string `json:"annotations"`
	APIVersion            string   `json:"apiVersion"`
	Contract              string   `json:"contract"`
	HolderIdentity        string   `json:"holderIdentity"`
	HolderKinds           []string `json:"holderKinds"`
	Kind                  string   `json:"kind"`
	LeaseDurationSeconds  int      `json:"leaseDurationSeconds"`
	ManagerLifecycle      string   `json:"managerLifecycle"`
	ManagedByLabel        string   `json:"managedByLabel"`
	MaximumRPCSeconds     int      `json:"maximumRPCSeconds"`
	NameAlgorithm         string   `json:"nameAlgorithm"`
	ObservedExpiry        string   `json:"observedExpiry"`
	OwnerReference        string   `json:"ownerReference"`
	RPCContextParent      string   `json:"rpcContextParent"`
	StatusFields          []string `json:"statusFields"`
	WriteOwner            string   `json:"writeOwner"`
	RenewEverySeconds     int      `json:"renewEverySeconds"`
}

func loadBitcoinReservationContract(t *testing.T, root string) bitcoinReservationContract {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "contracts", "bitcoin-reservation-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract bitcoinReservationContract
	if err := json.Unmarshal(content, &contract); err != nil {
		t.Fatal(err)
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(content, &rawFields); err != nil {
		t.Fatal(err)
	}
	assertSupersededBitcoinMetadata(t, rawFields)
	fieldNames := make([]string, 0, len(rawFields))
	for name := range rawFields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	assertStringsEqual(t, "Bitcoin reservation fields", fieldNames, []string{"acquisitionTokenBytes", "annotations", "apiVersion", "contract", "holderIdentity", "holderKinds", "kind", "leaseDurationSeconds", "managedByLabel", "managerLifecycle", "maximumRPCSeconds", "nameAlgorithm", "observedExpiry", "ownerReference", "renewEverySeconds", "rpcContextParent", "statusFields", "writeOwner"})
	if contract.Contract != "bitcoin.stacks.org/reservation/v1alpha1" ||
		contract.APIVersion != "coordination.k8s.io/v1" || contract.Kind != "Lease" ||
		contract.AcquisitionTokenBytes != 16 || contract.LeaseDurationSeconds != 30 ||
		contract.RenewEverySeconds != 10 || contract.MaximumRPCSeconds != 10 ||
		contract.HolderIdentity != "<holder-uid>/<32-lowercase-hex-acquisition-token>" ||
		contract.NameAlgorithm != "bitcoin-mutation-<first-32-hex-sha256(namespace-NUL-targetUID)>" ||
		contract.ManagerLifecycle != "leader-gated-manager-runnable" ||
		contract.ObservedExpiry != "local-observation-of-unchanged-record" ||
		contract.RPCContextParent != "reservation-handle" || contract.WriteOwner != "reservation-handle" ||
		contract.ManagedByLabel != "app.kubernetes.io/managed-by=stacks-action-operator" ||
		contract.OwnerReference != "BitcoinNode UID, controller=false, blockOwnerDeletion=false" {
		t.Fatalf("unexpected Bitcoin reservation contract: %#v", contract)
	}
	assertStringsEqual(t, "reservation holder kinds", contract.HolderKinds, []string{"BitcoinBlockGeneration", "BitcoinBlockProduction", "BitcoinReorganization"})
	assertStringsEqual(t, "reservation annotations", contract.Annotations, []string{"bitcoin.stacks.org/holder-kind", "bitcoin.stacks.org/holder-uid", "bitcoin.stacks.org/acquisition-token", "bitcoin.stacks.org/rpc-not-after", "bitcoin.stacks.org/target-uid"})
	assertStringsEqual(t, "reservation status fields", contract.StatusFields, []string{"leaseName", "holderIdentity", "acquisitionToken", "acquiredAt", "renewedAt", "rpcNotAfter", "releasedAt"})
	return contract
}

func TestHistoricalBitcoinBlockProductionDesignContract(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "contracts", "bitcoin-block-production-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		AmbiguityBehavior string `json:"ambiguityBehavior"`
		APIGroup          string `json:"apiGroup"`
		APIVersion        string `json:"apiVersion"`
		Cadence           struct {
			DelayAppliesBefore string   `json:"delayAppliesBefore"`
			FixedFields        []string `json:"fixedFields"`
			IntervalCEL        string   `json:"intervalCEL"`
			Modes              []string `json:"modes"`
			OneOfCEL           string   `json:"oneOfCEL"`
			UniformFields      []string `json:"uniformFields"`
			UniformOrderCEL    string   `json:"uniformOrderCEL"`
			UniformReplayClaim bool     `json:"uniformReplayClaim"`
			UniformSelection   string   `json:"uniformSelection"`
		} `json:"cadence"`
		ConditionTypes                      []string `json:"conditionTypes"`
		ConditionsRequireObservedGeneration bool     `json:"conditionsRequireObservedGeneration"`
		CompletionClaim                     bool     `json:"completionClaim"`
		Contract                            string   `json:"contract"`
		Finalizer                           string   `json:"finalizer"`
		HistoryContract                     string   `json:"historyContract"`
		ImmutableSpecFields                 []string `json:"immutableSpecFields"`
		ImmutableSpecFieldsCEL              string   `json:"immutableSpecFieldsCEL"`
		Kind                                string   `json:"kind"`
		Limits                              struct {
			IntervalMaximum                              string `json:"intervalMaximum"`
			IntervalMinimum                              string `json:"intervalMinimum"`
			MaximumAcknowledgedBlocksBetweenStatusWrites int    `json:"maximumAcknowledgedBlocksBetweenStatusWrites"`
			MaximumStatusWriteInterval                   string `json:"maximumStatusWriteInterval"`
			RecentBlockHashesMaximum                     int    `json:"recentBlockHashesMaximum"`
		} `json:"limits"`
		MutableSpecFields     []string `json:"mutableSpecFields"`
		MutationBatchSize     int      `json:"mutationBatchSize"`
		NameMatchesTargetCEL  string   `json:"nameMatchesTargetCEL"`
		PausedDefault         bool     `json:"pausedDefault"`
		PerBlockIntentJournal bool     `json:"perBlockIntentJournal"`
		PhaseSemantics        struct {
			Degraded                   string   `json:"degraded"`
			IdentityReplacementReasons []string `json:"identityReplacementReasons"`
			Pending                    string   `json:"pending"`
		} `json:"phaseSemantics"`
		Phases                 []string `json:"phases"`
		ReservationContract    string   `json:"reservationContract"`
		RPCMethods             []string `json:"rpcMethods"`
		SpecFields             []string `json:"specFields"`
		StatusFields           []string `json:"statusFields"`
		StatusSubresource      bool     `json:"statusSubresource"`
		TargetIdentityContract string   `json:"targetIdentityContract"`
		Timer                  struct {
			Implementation         string   `json:"implementation"`
			KeyFields              []string `json:"keyFields"`
			RestartBehavior        string   `json:"restartBehavior"`
			StaleReconcileBehavior string   `json:"staleReconcileBehavior"`
		} `json:"timer"`
		Yield struct {
			ActionPhases           []string `json:"actionPhases"`
			LeaseNotification      string   `json:"leaseNotification"`
			Lookup                 string   `json:"lookup"`
			MinimumGapAfterRelease string   `json:"minimumGapAfterRelease"`
			PendingReason          string   `json:"pendingReason"`
			PendingRequirement     string   `json:"pendingRequirement"`
			Scope                  string   `json:"scope"`
		} `json:"yield"`
	}
	if err := json.Unmarshal(content, &contract); err != nil {
		t.Fatal(err)
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(content, &rawFields); err != nil {
		t.Fatal(err)
	}
	assertSupersededBitcoinMetadata(t, rawFields)
	fieldNames := make([]string, 0, len(rawFields))
	for name := range rawFields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	assertStringsEqual(t, "Bitcoin production fields", fieldNames, []string{"ambiguityBehavior", "apiGroup", "apiVersion", "cadence", "completionClaim", "conditionTypes", "conditionsRequireObservedGeneration", "contract", "finalizer", "historyContract", "immutableSpecFields", "immutableSpecFieldsCEL", "kind", "limits", "mutableSpecFields", "mutationBatchSize", "nameMatchesTargetCEL", "pausedDefault", "perBlockIntentJournal", "phaseSemantics", "phases", "reservationContract", "rpcMethods", "specFields", "statusFields", "statusSubresource", "targetIdentityContract", "timer", "yield"})
	if contract.Contract != "bitcoin.stacks.org/block-production/v1alpha1" ||
		contract.APIGroup != "bitcoin.stacks.org" || contract.APIVersion != "v1alpha1" ||
		contract.Kind != "BitcoinBlockProduction" || contract.Finalizer != "bitcoin.stacks.org/block-production" ||
		contract.NameMatchesTargetCEL != "self.metadata.name == self.spec.bitcoinNodeRef.name" ||
		contract.ImmutableSpecFieldsCEL != "self.networkRef == oldSelf.networkRef && self.bitcoinNodeRef == oldSelf.bitcoinNodeRef && self.destinationAddress == oldSelf.destinationAddress" ||
		!contract.ConditionsRequireObservedGeneration || !contract.StatusSubresource || contract.CompletionClaim || contract.PerBlockIntentJournal ||
		contract.PausedDefault || contract.MutationBatchSize != 1 ||
		contract.AmbiguityBehavior != "hold-until-rpc-not-after-release-no-retry-no-completion-claim" ||
		contract.HistoryContract != "bounded-status-structured-logs-passive-observation" ||
		contract.TargetIdentityContract != "bind-object-uids-refresh-admitted-runtime-identity" {
		t.Fatalf("unexpected Bitcoin production identity or behavior: %#v", contract)
	}
	assertStringsEqual(t, "production spec fields", contract.SpecFields, []string{"networkRef", "bitcoinNodeRef", "destinationAddress", "cadence", "paused"})
	assertStringsEqual(t, "production immutable fields", contract.ImmutableSpecFields, []string{"networkRef", "bitcoinNodeRef", "destinationAddress"})
	assertStringsEqual(t, "production mutable fields", contract.MutableSpecFields, []string{"cadence", "paused"})
	assertStringsEqual(t, "production phases", contract.Phases, []string{"Pending", "Running", "Paused", "Degraded", "Terminating"})
	if contract.PhaseSemantics.Pending != "before-first-successful-identity-bind" ||
		contract.PhaseSemantics.Degraded != "previously-bound-target-not-currently-mutable" {
		t.Fatalf("unexpected production phase semantics: %#v", contract.PhaseSemantics)
	}
	assertStringsEqual(t, "production identity replacement reasons", contract.PhaseSemantics.IdentityReplacementReasons, []string{"NetworkIdentityChanged", "TargetIdentityChanged"})
	assertStringsEqual(t, "production conditions", contract.ConditionTypes, []string{"TargetResolved", "CredentialsReady", "CadenceAccepted", "Ready"})
	assertStringsEqual(t, "production RPC methods", contract.RPCMethods, []string{"getblockchaininfo", "validateaddress", "generatetoaddress"})
	assertStringsEqual(t, "production status fields", contract.StatusFields, []string{"observedGeneration", "phase", "conditions", "admittedNetwork", "admittedTarget", "activeCadenceGeneration", "acknowledgedBlocks", "ambiguousAttempts", "lastSelectedInterval", "lastAcknowledgedAt", "lastAmbiguousAt", "recentBlockHashes"})
	if contract.Cadence.DelayAppliesBefore != "every-block-including-first" ||
		contract.Cadence.OneOfCEL != "[has(self.fixed), has(self.uniform)].filter(x, x).size() == 1" ||
		contract.Cadence.IntervalCEL != "duration(self) >= duration('1s') && duration(self) <= duration('1h')" ||
		contract.Cadence.UniformOrderCEL != "duration(self.minimumInterval) <= duration(self.maximumInterval)" ||
		contract.Cadence.UniformReplayClaim ||
		contract.Cadence.UniformSelection != "crypto-rand-unbiased-inclusive-integer-nanoseconds" {
		t.Fatalf("unexpected production cadence: %#v", contract.Cadence)
	}
	assertStringsEqual(t, "production cadence modes", contract.Cadence.Modes, []string{"fixed", "uniform"})
	assertStringsEqual(t, "production fixed fields", contract.Cadence.FixedFields, []string{"interval"})
	assertStringsEqual(t, "production uniform fields", contract.Cadence.UniformFields, []string{"minimumInterval", "maximumInterval"})
	if contract.Limits.IntervalMinimum != "1s" || contract.Limits.IntervalMaximum != "1h" ||
		contract.Limits.RecentBlockHashesMaximum != 8 ||
		contract.Limits.MaximumStatusWriteInterval != "30s" ||
		contract.Limits.MaximumAcknowledgedBlocksBetweenStatusWrites != 16 {
		t.Fatalf("unexpected production limits: %#v", contract.Limits)
	}
	if contract.Timer.Implementation != "requeue-after-with-process-memory" ||
		contract.Timer.RestartBehavior != "select-fresh-full-delay" ||
		contract.Timer.StaleReconcileBehavior != "evaluate-current-key-and-due-time" {
		t.Fatalf("unexpected production timer contract: %#v", contract.Timer)
	}
	assertStringsEqual(t, "production timer key fields", contract.Timer.KeyFields, []string{"namespace", "name", "objectUID", "activeCadenceGeneration"})
	assertStringsEqual(t, "production yield phases", contract.Yield.ActionPhases, []string{"Admitted", "Active", "Recovering"})
	if contract.Yield.Lookup != "uncached-list-before-every-acquisition" ||
		contract.Yield.Scope != "same-namespace-and-bitcoin-node-reference" ||
		contract.Yield.MinimumGapAfterRelease != "configured-selected-interval" ||
		contract.Yield.PendingReason != "TargetBusy" ||
		contract.Yield.PendingRequirement != "admission-complete-waiting-only-for-reservation" ||
		contract.Yield.LeaseNotification != "managed-label-filtered-watch-target-uid-index" {
		t.Fatalf("unexpected production yield contract: %#v", contract.Yield)
	}
	reservation := loadBitcoinReservationContract(t, root)
	if contract.ReservationContract != reservation.Contract {
		t.Fatalf("production reservation contract = %q, want %q", contract.ReservationContract, reservation.Contract)
	}
	documentBytes, err := os.ReadFile(filepath.Join(root, "docs", "design", "history", "bitcoin-lifecycle-m0.4.md"))
	if err != nil {
		t.Fatal(err)
	}
	document := string(documentBytes)
	documentValues := append(append(append(append(append(append(append([]string{contract.Contract, contract.Kind, contract.Finalizer, contract.ReservationContract}, contract.SpecFields...), contract.StatusFields...), contract.Phases...), contract.ConditionTypes...), contract.RPCMethods...), contract.Cadence.Modes...), contract.Cadence.FixedFields...)
	documentValues = append(documentValues, contract.Cadence.UniformFields...)
	for _, value := range documentValues {
		if !strings.Contains(document, "`"+value+"`") {
			t.Errorf("Bitcoin lifecycle document does not represent production value %q", value)
		}
	}
	for _, forbidden := range []string{"`BitcoinBlockProduction` follows the atomic action contract", "`BitcoinBlockProduction` is a bounded action"} {
		if strings.Contains(document, forbidden) {
			t.Errorf("Bitcoin production design retains forbidden contract %q", forbidden)
		}
	}
	for _, required := range []string{
		"The due time is process memory, not CRD status.",
		"`Degraded/NetworkIdentityChanged`",
		"`Degraded/TargetIdentityChanged`",
		"Invalid, not-Ready, or otherwise stuck `Pending`",
		"get/list/watch/create/update/patch Leases",
		"Kubernetes RBAC cannot constrain `list` or `watch` by label.",
	} {
		if !strings.Contains(document, required) {
			t.Errorf("Bitcoin production design does not represent refinement %q", required)
		}
	}

	planBytes, err := os.ReadFile(filepath.Join(root, "docs", "design", "m0-remediation-plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(planBytes), "otherwise stuck `Pending` actions do not stall baseline production") {
		t.Error("M0 plan does not require the stuck-Pending starvation regression")
	}
}

func assertBitcoinKindContract(t *testing.T, actual struct {
	Kind         string   `json:"kind"`
	RPCMethods   []string `json:"rpcMethods"`
	SpecFields   []string `json:"specFields"`
	StatusFields []string `json:"statusFields"`
}, kind string, specFields, statusFields, rpcMethods []string) {
	t.Helper()
	if actual.Kind != kind {
		t.Fatalf("Bitcoin action kind = %q, want %q", actual.Kind, kind)
	}
	assertStringsEqual(t, kind+" spec fields", actual.SpecFields, specFields)
	assertStringsEqual(t, kind+" status fields", actual.StatusFields, statusFields)
	assertStringsEqual(t, kind+" RPC methods", actual.RPCMethods, rpcMethods)
}

func assertStringsEqual(t *testing.T, name string, actual, expected []string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("%s = %v, want %v", name, actual, expected)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("%s = %v, want %v", name, actual, expected)
		}
	}
}

func TestDockerContextIsDefaultDeny(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	rules := make([]string, 0)
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rules = append(rules, line)
	}
	if len(rules) == 0 || rules[0] != "**" {
		t.Fatal("root Docker context must begin with a default-deny ** rule")
	}
	required := []string{
		"!apis/network/**",
		"!operators/network/**",
		"!operators/observability/**",
		"**/.claude",
		"**/.env",
		"**/.env.*",
	}
	lastNegation := -1
	positions := make(map[string]int, len(rules))
	for index, rule := range rules {
		positions[rule] = index
		if strings.HasPrefix(rule, "!") {
			lastNegation = index
		}
	}
	for _, expected := range required {
		if _, found := positions[expected]; !found {
			t.Errorf("root .dockerignore is missing required rule %q", expected)
		}
	}
	for _, sensitive := range []string{"**/.claude", "**/.env", "**/.env.*"} {
		if position, found := positions[sensitive]; found && position <= lastNegation {
			t.Errorf("sensitive exclusion %q must follow every allow rule", sensitive)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate contract test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
}
