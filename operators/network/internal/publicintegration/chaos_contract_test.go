//go:build live

package publicintegration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestChaosQualificationPinsBothNativeSelectorIdentities(t *testing.T) {
	pair := [2]chaosActor{{LogicalName: "source", Participant: identity{UID: "source-uid"}}, {LogicalName: "target", Participant: identity{UID: "target-uid"}}}
	for _, action := range []string{"delay", "partition"} {
		t.Run(action, func(t *testing.T) {
			object := chaosRequest("fixture", "root-uid", pair, action)
			for i, path := range [][]string{{"spec", "selector"}, {"spec", "target", "selector"}} {
				selector, found, err := unstructured.NestedMap(object.Object, path...)
				if err != nil || !found || !reflect.DeepEqual(selector, chaosSelectors("fixture", "root-uid", pair[i])) {
					t.Fatalf("selector=%v error=%v", selector, err)
				}
				labels := selector["labelSelectors"].(map[string]any)
				if len(labels) != 5 || labels["network.stacks.org/network-uid"] != "root-uid" || labels["network.stacks.org/participant-uid"] != string(pair[i].Participant.UID) || labels["network.stacks.org/role"] != "actor" {
					t.Fatalf("unbounded selector %v", selector)
				}
			}
			if object.GetLabels()["network.stacks.org/network-uid"] != "root-uid" || len(object.GetOwnerReferences()) != 0 || len(object.GetFinalizers()) != 0 {
				t.Fatal("unexpected request lifecycle fields")
			}
			if _, found := object.Object["status"]; found {
				t.Fatal("request fabricated native status")
			}
			direction, _, _ := unstructured.NestedString(object.Object, "spec", "direction")
			if (action == "partition" && direction != "both") || (action == "delay" && direction != "to") {
				t.Fatal(direction)
			}
			replacement := pair
			replacement[1].Participant.UID = "replacement"
			if reflect.DeepEqual(chaosRequest("fixture", "root-uid", replacement, action).Object, object.Object) {
				t.Fatal("replacement target reused old selector")
			}
		})
	}
}

func TestChaosQualificationSeparatesTransportAndExecFailures(t *testing.T) {
	for _, tc := range []struct {
		name, stdout, stderr string
		commandErr           bool
		reachable, valid     bool
	}{
		{"native", `{"stacks_tip":"tip","stacks_tip_height":2}` + "\n0.501", "", false, true, true},
		{"timeout", "", "curl: (28) Operation timed out", true, false, true},
		{"refused", "", "curl: (7) Failed to connect", true, false, true},
		{"missing-curl", "", "curl: executable file not found", true, false, false},
		{"kubectl-error", "", "Unauthorized", true, false, false},
		{"http-error", "", "curl: (22) HTTP error 500", true, false, false},
		{"wrong-body", "{}\n0.1", "", false, false, false},
		{"missing-time", `{"stacks_tip":"tip","stacks_tip_height":2}`, "", false, false, false},
		{"bad-time", `{"stacks_tip":"tip","stacks_tip_height":2}` + "\ninvalid", "", false, false, false},
		{"nan-time", `{"stacks_tip":"tip","stacks_tip_height":2}` + "\nNaN", "", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commandErr error
			if tc.commandErr {
				commandErr = errors.New("command failed")
			}
			probe, err := parseChaosProbe([]byte(tc.stdout), []byte(tc.stderr), commandErr)
			if (err == nil) != tc.valid || err == nil && probe.Reachable != tc.reachable {
				t.Fatalf("probe=%+v error=%v", probe, err)
			}
		})
	}
}

// chaosControlFixture supplies independent API heartbeats and native observations for negative checks.
func chaosControlFixture() snapshot {
	s, _, _ := actionContractFixture()
	genesis := common.Binding{Kind: "StacksGenesis", Name: "genesis", UID: "genesis"}
	s.Status.GenesisRef = &genesis
	for _, name := range []string{"one", "two"} {
		runtime := &api.ParticipantRuntimeStatus{PodRef: &common.Binding{Kind: "Pod", Name: name, UID: "pod-" + genesis.UID}, ContainerID: "actor", ConfigurationDigest: "config", Protocol: &api.StacksProtocolObservation{Available: true, ObservedAt: metav1.NewTime(s.At), PodUID: "pod-" + genesis.UID, ContainerID: "actor", ConfigurationDigest: "config", GenesisUID: genesis.UID}}
		s.Participants = append(s.Participants, participantEvidence{Identity: identity{Name: name}, Name: name, Kind: "StacksNode", Status: api.ParticipantStatus{Runtime: runtime}})
	}
	worker := &api.WorkerExecutionStatus{PodUID: "worker-pod", ProcessNonce: "process", ProfileDigest: "profile", ObservedAt: metav1.NewTime(s.At), Traffic: &api.TrafficObservation{Available: true, ObservedAt: metav1.NewTime(s.At)}}
	s.Participants = append(s.Participants, participantEvidence{Identity: identity{UID: "traffic"}, Name: "traffic", Kind: "StacksTransactionProduction", Status: api.ParticipantStatus{Execution: worker}})
	s.Status.Identities = []api.InstanceIdentity{{Name: "traffic", UID: "traffic", Worker: &api.WorkerSession{Pod: api.WorkerPodBinding{Kind: "Pod", Name: "worker", UID: "worker-pod"}, ProfileDigest: "profile"}}}
	return s
}

func TestChaosQualificationRequiresFreshIndependentControlReads(t *testing.T) {
	for _, mode := range []string{"valid", "worker-heartbeat", "worker-rpc", "actor-rpc", "bitcoin-rpc", "worker-replacement", "worker-unbound"} {
		t.Run(mode, func(t *testing.T) {
			before := chaosControlFixture()
			raw, _ := json.Marshal(before)
			var current snapshot
			if err := json.Unmarshal(raw, &current); err != nil {
				t.Fatal(err)
			}
			worker := current.Participants[len(current.Participants)-1].Status.Execution
			switch mode {
			case "worker-heartbeat":
				worker.ObservedAt = metav1.NewTime(current.At.Add(-17 * time.Second))
			case "worker-rpc":
				worker.Traffic.Available = false
			case "actor-rpc":
				current.Participants[1].Status.Runtime.Protocol.Available = false
			case "bitcoin-rpc":
				current.Executions[0].Status.Observation.ObservedAt = metav1.NewTime(current.At.Add(-17 * time.Second))
			case "worker-replacement":
				worker.ProcessNonce = "replacement"
			case "worker-unbound":
				worker.PodUID = "replacement"
			}
			_, err := chaosControl(current, before)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestChaosQualificationRequiresNativeInjectionAndRecoveryConditions(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "AllInjected", "status": "True"}, map[string]any{"type": "AllRecovered", "status": "False"}}}}}
	if !chaosCondition(object, "AllInjected") || chaosCondition(object, "AllRecovered") {
		t.Fatal("native condition interpretation differs")
	}
	unstructured.RemoveNestedField(object.Object, "status")
	if chaosCondition(object, "AllInjected") || chaosCondition(object, "AllRecovered") {
		t.Fatal("missing controller status became evidence")
	}
}

// TestChaosObservationResampling keeps gaps distinct from successful reads and identity errors.
func TestChaosObservationResampling(t *testing.T) {
	for _, mode := range []string{"recovers", "persistent", "identity-after-gap"} {
		t.Run(mode, func(t *testing.T) {
			h, _ := waitFixture(t, waitRoot())
			before := chaosControlFixture()
			attempts := 0
			bound := 100 * time.Millisecond
			if mode == "recovers" {
				bound = 3 * time.Second
			}
			_, err := h.wait(context.Background(), "control", bound, true, func(snapshot) (bool, error) {
				attempts++
				current := chaosControlFixture()
				current.Participants[1].Status.Runtime.Protocol.Available = mode == "recovers" && attempts > 1
				if mode == "identity-after-gap" {
					current.Participants[len(current.Participants)-1].Status.Execution.PodUID = "replacement"
				}
				_, ready, err := h.sampleChaosControl("control", current, before)
				return ready, err
			})
			if (err == nil) != (mode == "recovers") {
				t.Fatalf("mode=%s attempts=%d err=%v", mode, attempts, err)
			}
			if mode == "identity-after-gap" {
				if attempts != 1 || errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("identity failure was retried")
				}
			} else {
				if _, statErr := os.Stat(filepath.Join(h.evidence, "control-observation-gap.json")); statErr != nil {
					t.Fatal(statErr)
				}
				if mode == "persistent" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("persistent gap did not exhaust the bound")
				}
				if mode == "recovers" && attempts < 2 {
					t.Fatal("unavailable observation was accepted")
				}
			}
		})
	}
}
