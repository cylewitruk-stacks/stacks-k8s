package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/environment"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// evidenceManifest creates a valid bootstrap binding without invoking the SDK or a cluster.
func evidenceManifest(t *testing.T) string {
	t.Helper()
	genesis := profiles.DefaultGenesis()
	genesis.Balances = []network.GenesisBalance{{Name: "stacker", Address: "STTEST", Amount: 1000}}
	digest, err := environment.GenesisDigest(&genesis)
	if err != nil {
		t.Fatal(err)
	}
	parent := &network.StacksNetwork{TypeMeta: meta.TypeMeta{Kind: "StacksNetwork"}, ObjectMeta: meta.ObjectMeta{Name: "test", Namespace: "test"}, Spec: network.StacksNetworkSpec{Genesis: &genesis}}
	doc := environment.Document{Items: []any{parent}, Bootstrap: environment.Bootstrap{NetworkName: "test", Namespace: "test", GenesisDigest: digest, SignerAccount: "stacker", Signer: environment.SignerAccount{Address: "STTEST"}}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEvidenceDefaultsAndClaimsBelongToEntrypoints(t *testing.T) {
	for _, operation := range []string{"bootstrap", "renewal"} {
		t.Run(operation, func(t *testing.T) {
			// No cluster/SDK executable is available. Existing evidence must be rejected first.
			t.Setenv("PATH", t.TempDir())
			options := Options{Manifest: evidenceManifest(t), Kubeconfig: "unused", Context: "unused", BitcoinPort: 18443, StacksPort: 20443}
			invoke := func() error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if operation == "bootstrap" {
					return Run(ctx, options)
				}
				return Maintain(ctx, options, time.Second)
			}
			path := options.Manifest + "." + operation + "-evidence.json"
			// With cancellation, only the library's local evidence claim can succeed.
			if err := invoke(); err == nil {
				t.Fatal("cancelled operation succeeded")
			}
			original, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(original), `"EvidenceStarted"`) {
				t.Fatalf("library default missing: %v", err)
			}
			if operation == "renewal" {
				if _, err = os.Stat(options.Manifest + ".bootstrap-evidence.json"); !os.IsNotExist(err) {
					t.Fatal("maintenance claimed bootstrap path")
				}
			}
			err = invoke()
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "claim evidence") {
				t.Fatalf("existing evidence not rejected before external work: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(original) {
				t.Fatal("existing evidence overwritten")
			}
			options.Evidence = filepath.Join(filepath.Dir(path), "missing-directory", "evidence.json")
			if err = invoke(); err == nil || !strings.Contains(err.Error(), options.Evidence) || !strings.Contains(err.Error(), "claim evidence") {
				t.Fatalf("claim failure did not stop invocation: %v", err)
			}
		})
	}
}

func TestEvidenceSnapshotsRetainEventsAndRejectOtherOwners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	s := &session{options: Options{Evidence: path}, namespace: "test"}
	if err := s.claimEvidence("bootstrap"); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"Authorized", "Submitted"} {
		if err := s.record(event, map[string]any{"txid": "public"}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Events []map[string]any `json:"events"`
	}
	if err = json.Unmarshal(data, &snapshot); err != nil || len(snapshot.Events) != 3 {
		t.Fatalf("incomplete snapshot: %v", err)
	}
	if snapshot.Events[1]["event"] != "Authorized" || snapshot.Events[2]["event"] != "Submitted" {
		t.Fatal("event order changed")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("evidence is not private")
	}
	other := &session{options: Options{Evidence: path}}
	if err = other.claimEvidence("renewal"); err == nil {
		t.Fatal("second invocation acquired evidence")
	}
	if err = other.record("Overwrite", nil); err == nil {
		t.Fatal("unclaimed writer acquired evidence")
	}
	// Replacement by another local operation must not be overwritten on the next record.
	replacement := filepath.Join(filepath.Dir(path), "replacement")
	if err = os.WriteFile(replacement, []byte("other owner"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err = s.record("Late", nil); err == nil {
		t.Fatal("replaced evidence overwritten")
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "other owner" {
		t.Fatal("replacement contents changed")
	}
}
