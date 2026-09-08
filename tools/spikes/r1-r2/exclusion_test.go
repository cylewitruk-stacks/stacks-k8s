// Package r1r2 checks selected serial interleavings of the proposed record transitions.
package r1r2

import "testing"

// dispatchState distinguishes possible execution from an unaccounted receipt.
type dispatchState uint8

const (
	// idle permits another RPC within the current reservation.
	idle dispatchState = iota
	// armed excludes takeover while execution remains possible.
	armed
	// received retains the result until durable accounting.
	received
)

// dispatchID identifies one request and its issuing process.
type dispatchID struct {
	reservation string // Owner UID, stable across dependent RPCs.
	process     string // Fresh process-start nonce, never a reusable Pod name.
	epoch       uint64 // Executor generation.
	sequence    uint64 // Unique request sequence.
}

// executionSlot models atomic transitions, not Kubernetes persistence or RPC fencing.
type executionSlot struct {
	revision    uint64        // Version compared by callers performing CAS.
	epoch       uint64        // Last allocated executor generation.
	sequence    uint64        // Last allocated request sequence.
	reservation string        // Current reservation owner UID.
	process     string        // Current executor process nonce.
	request     dispatchID    // Outstanding or last accounted request.
	state       dispatchState // Dispatch/accounting state.
	clean       bool          // Explicit acknowledgement of satisfied cleanup.
}

// acquire reserves an empty slot or replaces the executor of the same idle reservation.
// Admission and replacement eligibility are supplied premises, not modeled here.
func (s *executionSlot) acquire(reservation, process string, revision uint64) bool {
	if reservation == "" || process == "" || s.revision != revision || s.state != idle {
		return false
	}
	if s.reservation != "" && s.reservation != reservation {
		return false
	}
	s.reservation, s.process = reservation, process
	s.epoch++
	s.revision++
	return true
}

// arm authorizes a new RPC without changing reservation ownership.
func (s *executionSlot) arm(process string, revision uint64) (dispatchID, bool) {
	if s.reservation == "" || s.process != process || s.revision != revision || s.state != idle {
		return dispatchID{}, false
	}
	s.sequence++
	s.request = dispatchID{s.reservation, process, s.epoch, s.sequence}
	s.state, s.clean = armed, false
	s.revision++
	return s.request, true
}

// finish records an exact receipt without releasing or accounting for it.
func (s *executionSlot) finish(request dispatchID) bool {
	if s.state != armed || s.request != request {
		return false
	}
	s.state = received
	s.revision++
	return true
}

// account acknowledges durable accounting, accepting duplicates only while the request is current.
// Older request identities are rejected after a subsequent arm or reservation release.
func (s *executionSlot) account(request dispatchID) bool {
	if request == (dispatchID{}) || s.request != request || s.state == armed {
		return false
	}
	if s.state == received {
		s.state = idle
		s.revision++
	}
	return true
}

// cleanup acknowledges satisfied obligations; it does not model actual R4 cleanup.
func (s *executionSlot) cleanup(process string, revision uint64) bool {
	if s.reservation == "" || s.process != process || s.revision != revision || s.state != idle {
		return false
	}
	s.clean = true
	s.revision++
	return true
}

// release explicitly relinquishes a clean reservation with no unaccounted RPC.
func (s *executionSlot) release(process string, revision uint64) bool {
	if s.reservation == "" || s.process != process || s.revision != revision || s.state != idle || !s.clean {
		return false
	}
	s.reservation, s.process = "", ""
	s.request, s.clean = dispatchID{}, false
	s.revision++
	return true
}

// TestStaleCallerCannotArmAfterTakeover checks an interleaving across executor replacement.
func TestStaleCallerCannotArmAfterTakeover(t *testing.T) {
	slot := &executionSlot{}
	if !slot.acquire("action-uid", "process-start-1", 0) {
		t.Fatal("initial reservation failed")
	}
	staleRevision := slot.revision
	if !slot.acquire("action-uid", "process-start-2", staleRevision) {
		t.Fatal("same reservation could not replace its idle executor")
	}
	if _, ok := slot.arm("process-start-1", staleRevision); ok {
		t.Fatal("stale revision retained dispatch authority")
	}
	if _, ok := slot.arm("process-start-1", slot.revision); ok {
		t.Fatal("reading the new revision restored the old process's authority")
	}
	if _, ok := slot.arm("process-start-2", slot.revision); !ok {
		t.Fatal("current executor could not arm")
	}
}

// TestArmedRequestCannotBeAdoptedByRestart checks process identity after possible dispatch.
func TestArmedRequestCannotBeAdoptedByRestart(t *testing.T) {
	slot := &executionSlot{}
	if !slot.acquire("action-uid", "same-pod-start-1", 0) {
		t.Fatal("initial reservation failed")
	}
	request, ok := slot.arm("same-pod-start-1", slot.revision)
	if !ok {
		t.Fatal("could not arm")
	}
	if slot.acquire("action-uid", "same-pod-start-2", slot.revision) {
		t.Fatal("a restarted process adopted possibly dispatched work")
	}
	if _, ok := slot.arm("same-pod-start-1", slot.revision); ok {
		t.Fatal("an existing Armed record authorized a second request")
	}
	foreignReceipt := request
	foreignReceipt.process = "same-pod-start-2"
	if slot.finish(foreignReceipt) || slot.cleanup("same-pod-start-1", slot.revision) ||
		slot.release("same-pod-start-1", slot.revision) {
		t.Fatal("unresolved work was cleared without an exact receipt")
	}
	if !slot.finish(request) {
		t.Fatal("exact late receipt was rejected")
	}
	if slot.acquire("action-uid", "same-pod-start-2", slot.revision) {
		t.Fatal("an unaccounted receipt allowed executor replacement")
	}
}

// TestDependentRPCsRetainReservation checks accounting, takeover, cleanup, and explicit release.
func TestDependentRPCsRetainReservation(t *testing.T) {
	slot := &executionSlot{}
	if !slot.acquire("reorg-uid", "process-1", 0) {
		t.Fatal("initial reservation failed")
	}
	first, ok := slot.arm("process-1", slot.revision)
	if !ok || !slot.finish(first) {
		t.Fatal("first RPC did not reach receipt state")
	}
	if slot.cleanup("process-1", slot.revision) || slot.release("process-1", slot.revision) {
		t.Fatal("unaccounted receipt permitted cleanup or release")
	}
	if _, ok := slot.arm("process-1", slot.revision); ok {
		t.Fatal("next RPC overwrote an unaccounted receipt")
	}
	if !slot.account(first) {
		t.Fatal("receipt accounting failed")
	}
	accountedRevision := slot.revision
	if !slot.account(first) || slot.revision != accountedRevision {
		t.Fatal("duplicate accounting changed state")
	}
	if slot.acquire("baseline-uid", "producer", slot.revision) || slot.release("process-1", slot.revision) {
		t.Fatal("accounting released the reservation before cleanup")
	}
	if !slot.acquire("reorg-uid", "process-2", slot.revision) {
		t.Fatal("same reservation could not replace its executor between RPCs")
	}
	second, ok := slot.arm("process-2", slot.revision)
	if !ok || second == first {
		t.Fatal("dependent RPC did not get a distinct identity")
	}
	if slot.finish(first) || slot.account(first) {
		t.Fatal("old receipt affected the next RPC")
	}
	if !slot.finish(second) || !slot.account(second) || !slot.cleanup("process-2", slot.revision) {
		t.Fatal("second RPC accounting or cleanup failed")
	}
	if slot.release("process-1", slot.revision) || slot.release("process-2", accountedRevision) {
		t.Fatal("stale executor or revision released the reservation")
	}
	if !slot.release("process-2", slot.revision) || !slot.acquire("baseline-uid", "producer", slot.revision) {
		t.Fatal("explicit clean release did not allow baseline admission")
	}
}

// TestCompetingReservationsCompareRevision checks both orders of competing initial claims.
func TestCompetingReservationsCompareRevision(t *testing.T) {
	for _, first := range []string{"baseline", "action"} {
		t.Run(first, func(t *testing.T) {
			slot := &executionSlot{}
			other := "action"
			if first == other {
				other = "baseline"
			}
			if !slot.acquire(first, "winner", 0) {
				t.Fatal("first claim failed")
			}
			if slot.acquire(other, "loser", 0) || slot.acquire(other, "loser", slot.revision) {
				t.Fatal("competing owner bypassed CAS or reservation ownership")
			}
		})
	}
}
