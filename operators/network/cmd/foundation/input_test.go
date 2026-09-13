package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload/stacksconfig"
)

func TestPublicInputFileBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	for _, tc := range []struct {
		name, mode, inline, content string
		valid                       bool
	}{
		{"valid", "resolve-stacks-config", "", `{"namespace":"test"}`, true},
		{"bitcoin", "resolve-bitcoin-config", "", `{"namespace":"test"}`, true},
		{"bitcoin empty", "resolve-bitcoin-config", "", "", false},
		{"bitcoin oversized", "resolve-bitcoin-config", "", strings.Repeat("x", stacksconfig.MaximumBytes+1), false},
		{"bitcoin two sources", "resolve-bitcoin-config", `{}`, `{}`, false},
		{"empty", "resolve-stacks-config", "", "", false},
		{"oversized", "resolve-stacks-config", "", strings.Repeat("x", stacksconfig.MaximumBytes+1), false},
		{"wrong mode", "controller", "", `{}`, false},
		{"two sources", "resolve-stacks-config", `{}`, `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := publicInput(tc.mode, tc.inline, path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if tc.valid && got != tc.content {
				t.Fatal("request bytes changed")
			}
			if err != nil && len(got) != 0 {
				t.Fatal("rejected input returned data")
			}
		})
	}
	if _, err := publicInput("resolve-stacks-config", "", filepath.Dir(path)); err == nil {
		t.Fatal("accepted directory")
	}
}

// TestBitcoinMountedPublicInput preserves the typed request beyond the OS argument-size limit.
func TestBitcoinMountedPublicInput(t *testing.T) {
	overrides, err := json.Marshal(map[string]any{"debug": strings.Repeat("public-value-", 16384)})
	if err != nil {
		t.Fatal(err)
	}
	original := participantworkload.BitcoinConfigInput{
		Namespace:          "test",
		ParticipantUID:     "participant-uid",
		PolicyDigest:       "sha256:policy",
		Config:             common.Binding{Kind: "Secret", Name: "output", UID: "output-uid"},
		ControlCredentials: common.Binding{Kind: "Secret", Name: "control", UID: "control-uid"},
		ActorCredentials:   common.Binding{Kind: "Secret", Name: "actor", UID: "actor-uid"},
		Report:             common.Binding{Kind: "ConfigMap", Name: "report", UID: "report-uid"},
		Seeds:              []string{"peer.test.svc"},
		Customization:      &common.Config{Overrides: &runtime.RawExtension{Raw: overrides}},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) <= 128*1024 {
		t.Fatal("fixture no longer exercises mounted input size")
	}
	path := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := publicInput("resolve-bitcoin-config", "", path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded participantworkload.BitcoinConfigInput
	if err := json.Unmarshal([]byte(request), &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Fatal("mounted Bitcoin resolver request changed its exact public bindings")
	}
}
