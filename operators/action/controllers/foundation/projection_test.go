package foundation

import (
	"testing"
	"time"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFiniteLifecycleKeepsUnknownAndWaitsForWithdrawal(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		armed  bool
		reason string
		want   string
	}{{"pending withdrawal", false, "", "Admitted"}, {"unknown after deadline", true, "", "Inconclusive"}, {"known withdrawal", false, "DeadlineExceeded", "Failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			state := &bitcoin.BitcoinActionReservation{Generation: &action.BitcoinBlockGenerationSpec{Count: 1}, ExpiresAt: metav1.NewTime(now.Add(-time.Second)), StopReason: tc.reason}
			record := &bitcoin.BitcoinExecution{}
			if tc.armed {
				record.Status.Armed = &bitcoin.BitcoinArmedRPC{Method: "Generate"}
			}
			status := &action.BitcoinBlockGenerationStatus{}
			project(status, state, record, now, nil)
			if status.Phase != tc.want {
				t.Fatalf("phase %s want%s", status.Phase, tc.want)
			}
			if tc.armed {
				state.BlocksGenerated = 1
				state.LastCompletedAt = &metav1.Time{Time: now.Add(-2 * time.Second)}
				record.Status.Armed = nil
				project(status, state, record, now, nil)
				if status.Phase != "Inconclusive" {
					t.Fatal("late evidence upgraded frozen outcome")
				}
			}
		})
	}
}

func TestReorganizationLifecycleBoundsCompensationCollection(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	state := &bitcoin.BitcoinActionReservation{Reorganization: &action.BitcoinReorganizationSpec{Depth: 1}, ExpiresAt: metav1.NewTime(now.Add(-time.Second)), InvalidationAcknowledged: true}
	record := &bitcoin.BitcoinExecution{}
	record.Status.Armed = &bitcoin.BitcoinArmedRPC{Method: "ReconsiderBlock"}
	status := &action.BitcoinBlockGenerationStatus{}
	project(status, state, record, now, nil)
	if status.Phase != "Recovering" {
		t.Fatal(status.Phase)
	}
	project(status, state, record, now.Add(30*time.Second), nil)
	if status.Phase != "Inconclusive" {
		t.Fatal("cleanup collection exceeded fixed horizon")
	}
}
