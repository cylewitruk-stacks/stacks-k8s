package integration

import (
	"encoding/json"
	"errors"
	"testing"
)

// poxContext projects public cycle telemetry without retaining unrelated response data.
type poxContext struct {
	// Height is the node-reported burn height, which may lag Core.
	Height *int64 `json:"current_burnchain_block_height"`
	// PrepareLength counts prepare-phase burn blocks.
	PrepareLength *int64 `json:"prepare_phase_block_length"`
	// RewardLength counts reward-phase burn blocks.
	RewardLength *int64 `json:"reward_phase_block_length"`
	// CycleLength is the reported full cycle length.
	CycleLength *int64 `json:"reward_cycle_length"`
	// Current contains the node-reported current reward cycle.
	Current struct {
		// ID identifies the current cycle.
		ID *int64 `json:"id"`
		// Active reports whether PoX is active for this cycle.
		Active *bool `json:"is_pox_active"`
	} `json:"current_cycle"`
	// Next records the node-reported next-cycle boundaries and countdowns.
	Next struct {
		// ID identifies the next cycle.
		ID *int64 `json:"id"`
		// PrepareStart is its prepare-phase boundary.
		PrepareStart *int64 `json:"prepare_phase_start_block_height"`
		// RewardStart is its reward-phase boundary.
		RewardStart *int64 `json:"reward_phase_start_block_height"`
		// UntilPrepare preserves the API countdown to preparation.
		UntilPrepare *int64 `json:"blocks_until_prepare_phase"`
		// UntilReward preserves the API countdown to reward start.
		UntilReward *int64 `json:"blocks_until_reward_phase"`
	} `json:"next_cycle"`
}

// decodePoX refuses missing cycle context rather than interpreting absent values as zero.
func decodePoX(data []byte) (poxContext, error) {
	var p poxContext
	if json.Unmarshal(data, &p) != nil {
		return poxContext{}, errors.New("invalid PoX response")
	}
	for _, value := range []*int64{
		p.Height,
		p.PrepareLength,
		p.RewardLength,
		p.CycleLength,
		p.Current.ID,
		p.Next.ID,
		p.Next.PrepareStart,
		p.Next.RewardStart,
		p.Next.UntilPrepare,
		p.Next.UntilReward,
	} {
		if value == nil {
			return poxContext{}, errors.New("incomplete PoX cycle context")
		}
	}
	if p.Current.Active == nil {
		return poxContext{}, errors.New("missing PoX activity state")
	}
	return p, nil
}

// phase labels the observed height against the node's reported next-cycle boundaries.
// Invalid boundary order or an out-of-range height remains unknown; this is not a consensus-phase oracle.
func (p poxContext) phase() string {
	if *p.Height < 0 || *p.Next.PrepareStart < 0 || *p.Next.RewardStart <= *p.Next.PrepareStart {
		return "unknown"
	}
	if *p.Height < *p.Next.PrepareStart {
		return "reward"
	}
	if *p.Height < *p.Next.RewardStart {
		return "prepare"
	}
	return "unknown"
}

// TestPoXContextPreservesPhaseBoundariesAndGaps prevents fabricated zero-valued diagnostics.
func TestPoXContextPreservesPhaseBoundariesAndGaps(t *testing.T) {
	const raw = `{"current_burnchain_block_height":374,"prepare_phase_block_length":5,` +
		`"reward_phase_block_length":15,"reward_cycle_length":20,` +
		`"current_cycle":{"id":18,"is_pox_active":false},"next_cycle":{"id":19,` +
		`"prepare_phase_start_block_height":375,"reward_phase_start_block_height":380,` +
		`"blocks_until_prepare_phase":1,"blocks_until_reward_phase":6},"unrelated":"not ` +
		`retained"}`
	p, err := decodePoX([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		height int64
		phase  string
	}{{374, "reward"}, {375, "prepare"}, {379, "prepare"}, {380, "unknown"}, {-1, "unknown"}} {
		*p.Height = test.height
		if got := p.phase(); got != test.phase {
			t.Fatalf("height %d: %s", test.height, got)
		}
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var projected map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &projected); err != nil {
		t.Fatal(err)
	}
	if _, found := projected["unrelated"]; found {
		t.Fatal("unrelated response data retained")
	}
	for _, field := range []string{
		"current_burnchain_block_height",
		"prepare_phase_block_length",
		"reward_phase_block_length",
		"reward_cycle_length",
		"current_cycle",
		"next_cycle",
	} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, field)
		data, _ := json.Marshal(fields)
		if _, err := decodePoX(data); err == nil {
			t.Fatalf("missing %s accepted", field)
		}
	}
	for _, bad := range []string{
		`null`,
		`{}`,
		`{"error":"server detail"}`,
		`{"current_burnchain_block_height":"invalid"}`,
	} {
		if _, err := decodePoX([]byte(bad)); err == nil {
			t.Fatal("invalid context accepted")
		}
	}
}
