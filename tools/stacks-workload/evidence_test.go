package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
)

// decodeWireEvents checks field presence independently of the production event type.
func decodeWireEvents(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var events []map[string]any
	for {
		var event map[string]any
		if err := decoder.Decode(&event); errors.Is(err, io.EOF) {
			return events
		} else if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
}

func TestStartedRecordsPublicBaseInputsAndTransactionZeros(t *testing.T) {
	for _, varied := range []bool{false, true} {
		name := "fixed"
		if varied {
			name = "varied"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := executionRequest()
				r.Sender = "ST000000000000000000002AMW42H"
				r.Endpoint = "http://endpoint-must-not-be-emitted"
				r.Count, r.MaxOutstanding, r.ObservationConcurrency, r.IntervalMilliseconds = 2, 0, 0, 0
				r.KeyBase, r.DurationSeconds = 1000, 1
				if varied {
					seed := uint64(7)
					r.Variation = &variation{Seed: &seed, Writes: &intRange{0, 0}}
				}
				var output bytes.Buffer
				if err := execute(t.Context(), &fakeWorkloadRPC{found: true}, r, 0,
					json.NewEncoder(&output)); err != nil {
					t.Fatal(err)
				}
				events := decodeWireEvents(t, output.Bytes())
				started := events[0]
				var want map[string]any
				if err := json.Unmarshal([]byte(`{
					"sender":"ST000000000000000000002AMW42H",
					"contract":"ST000000000000000000002AMW42H.load-driver",
					"feeMicroSTX":1000,"timeoutSeconds":60,
					"call":{"function":"run2","writes":1,"reads":2,"payloadBytes":16,"keyBase":1000}
				}`), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(started["inputs"], want) || started["nonce"] != float64(0) ||
					started["maxOutstanding"] != float64(100) || started["observationConcurrency"] != float64(1) ||
					started["count"] != float64(2) || started["durationSeconds"] != float64(1) ||
					started["planVersion"] != "sha256-shapes-v1" {
					t.Fatalf("incomplete effective/base inputs: %v", started)
				}
				if varied && started["variation"].(map[string]any)["seed"] != float64(7) {
					t.Fatal("variation inputs lost")
				}
				transactions := 0
				identities := map[any]int{}
				for _, event := range events {
					switch event["type"] {
					case "Started":
						if _, exists := event["ordinal"]; exists {
							t.Fatal("start has irrelevant ordinal")
						}
					case "Finished":
						for _, field := range []string{"nonce", "ordinal", "inputs"} {
							if _, exists := event[field]; exists {
								t.Fatalf("summary has irrelevant %s", field)
							}
						}
					case "Authorized", "Submitted", "Included":
						transactions++
						nonce, noncePresent := event["nonce"]
						identities[nonce]++
						ordinal, ordinalPresent := event["ordinal"]
						if !noncePresent || !ordinalPresent || nonce != ordinal ||
							(nonce != float64(0) && nonce != float64(1)) {
							t.Fatalf("transaction zero or identity lost: %v", event)
						}
					}
				}
				if transactions != 6 || identities[float64(0)] != 3 || identities[float64(1)] != 3 {
					t.Fatal("transaction evidence incomplete")
				}
				for _, excluded := range []string{r.PrivateKey, r.Endpoint, `"privateKey"`, `"endpoint"`} {
					if strings.Contains(output.String(), excluded) {
						t.Fatalf("excluded input present: %q", excluded)
					}
				}
			})
		})
	}
}

func TestStartedIdentifiesDeploymentWithoutEmbeddingSource(t *testing.T) {
	for _, version := range []byte{0, 3} {
		synctest.Test(t, func(t *testing.T) {
			r := executionRequest()
			r.Contract, r.ContractSource = "hello", "(define-public (hello) (ok true))"
			r.Count, r.Writes, r.Reads, r.PayloadBytes, r.Function = 0, 0, 0, 0, ""
			r.ClarityVersion = version
			var output bytes.Buffer
			client := &fakeWorkloadRPC{found: true}
			if err := execute(t.Context(), client, r, 0, json.NewEncoder(&output)); err != nil {
				t.Fatal(err)
			}
			started := decodeWireEvents(t, output.Bytes())[0]
			inputs := started["inputs"].(map[string]any)
			deployment := inputs["deployment"].(map[string]any)
			wantVersion := float64(version)
			if version == 0 {
				wantVersion = 4
			}
			if deployment["clarityVersion"] != wantVersion ||
				deployment["sourceDigest"] != "sha256:8b1bb0f6b23b36ddca734a2d7622f92ec442ddc6ddd2c7aa32f57cf46e0349c6" ||
				started["count"] != float64(1) || inputs["contract"] != "hello" {
				t.Fatalf("deployment identity lost: %v", started)
			}
			if _, exists := inputs["call"]; exists || strings.Contains(output.String(), r.ContractSource) ||
				strings.Contains(output.String(), r.PrivateKey) {
				t.Fatal("deployment leaked source/key or call-only metadata")
			}
			if len(client.sent) != 1 {
				t.Fatal("deployment count changed")
			}
		})
	}
}
