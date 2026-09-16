package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

// fakeWorkloadRPC tracks physical requests independently of emitted evidence.
type fakeWorkloadRPC struct {
	mu                                                 sync.Mutex
	sent                                               []transaction.Transaction
	starts                                             []time.Time
	outstanding, peakOutstanding, readers, peakReaders int
	delay                                              time.Duration
	found                                              bool
	submitErr, readErr                                 error
	delays                                             map[string]time.Duration
}

func (f *fakeWorkloadRPC) Submit(_ context.Context, tx transaction.Transaction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, tx)
	f.starts = append(f.starts, time.Now())
	f.outstanding++
	f.peakOutstanding = max(f.peakOutstanding, f.outstanding)
	return f.submitErr
}

func (f *fakeWorkloadRPC) Inclusion(ctx context.Context, id string) (rpc.Inclusion, error) {
	f.mu.Lock()
	f.readers++
	f.peakReaders = max(f.readers, f.peakReaders)
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.readers--; f.mu.Unlock() }()
	delay := f.delay
	if d, ok := f.delays[id]; ok {
		delay = d
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return rpc.Inclusion{}, ctx.Err()
	case <-timer.C:
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.found {
		f.outstanding--
	}
	return rpc.Inclusion{Found: f.found, Success: true, BlockID: strings.Repeat("a", 64)}, f.readErr
}

// executionRequest returns bounded public fixture inputs and a disposable test key.
func executionRequest() submitRequest {
	return submitRequest{
		PrivateKey: strings.Repeat("0", 63) + "1", Contract: "ST000000000000000000002AMW42H.load-driver",
		Function: "run2", Count: 6, Writes: 1, Reads: 2, PayloadBytes: 16, FeeMicroSTX: 1000, TimeoutSeconds: 60,
		MaxOutstanding: 3, ObservationConcurrency: 2, IntervalMilliseconds: 100,
	}
}

func decodeEvents(t *testing.T, data []byte) []experimentEvent {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	var events []experimentEvent
	for {
		var event experimentEvent
		err := dec.Decode(&event)
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
}

func TestBoundedPipelinePreservesNonceOrderAndEvidence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := executionRequest()
		client := &fakeWorkloadRPC{found: true, delay: time.Second}
		var output bytes.Buffer
		if err := execute(t.Context(), client, r, 42, json.NewEncoder(&output)); err != nil {
			t.Fatal(err)
		}
		if len(client.sent) != 6 || client.peakOutstanding != 3 || client.peakReaders != 2 || client.readers != 0 {
			t.Fatalf(
				"limits not exercised: sends=%d outstanding=%d readers=%d",
				len(client.sent),
				client.peakOutstanding,
				client.peakReaders,
			)
		}
		for i, tx := range client.sent {
			expected, err := buildTransaction(r, 42+uint64(i), i)
			if err != nil || tx.TxID != expected.TxID {
				t.Fatal("nonce/input order changed")
			}
			if i > 0 && client.starts[i].Sub(client.starts[i-1]) < 100*time.Millisecond {
				t.Fatal("rate burst")
			}
		}
		events := decodeEvents(t, output.Bytes())
		if events[0].Type != "Started" || events[len(events)-1].Type != "Finished" ||
			events[len(events)-1].Included != 6 {
			t.Fatal("incomplete event boundaries")
		}
		authorized, submitted, included := 0, 0, 0
		for _, e := range events {
			switch string(e.Type) {
			case "Authorized":
				authorized++
				if e.Shape == nil {
					t.Fatal("missing shape")
				}
			case "Submitted":
				submitted++
			case "Included":
				included++
			}
		}
		if authorized != 6 || submitted != 6 || included != 6 || bytes.Contains(output.Bytes(), []byte(r.PrivateKey)) {
			t.Fatal("missing receipts or leaked private key")
		}
	})
}

func TestDurationStopsNewSendsAndDrainsExisting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := executionRequest()
		r.DurationSeconds = 1
		r.IntervalMilliseconds = 600
		r.MaxOutstanding = 100
		client := &fakeWorkloadRPC{found: true}
		var output bytes.Buffer
		if err := execute(t.Context(), client, r, 0, json.NewEncoder(&output)); err != nil {
			t.Fatal(err)
		}
		events := decodeEvents(t, output.Bytes())
		summary := events[len(events)-1]
		if len(client.sent) != 2 || summary.Unsent != 4 || summary.Included != 2 || summary.Pending != 0 ||
			!*summary.Completed {
			t.Fatalf("cutoff/drain: %+v", summary)
		}
	})
}

func TestUncertainSendStopsWithoutReplayOrNonceReuse(t *testing.T) {
	r := executionRequest()
	client := &fakeWorkloadRPC{submitErr: errors.New("lost acknowledgement")}
	var output bytes.Buffer
	if err := execute(t.Context(), client, r, 17, json.NewEncoder(&output)); err == nil {
		t.Fatal("uncertainty succeeded")
	}
	events := decodeEvents(t, output.Bytes())
	summary := events[len(events)-1]
	if len(client.sent) != 1 || events[2].Type != "SubmissionUnconfirmed" || summary.Pending != 1 ||
		summary.Unsent != 5 ||
		*summary.Completed {
		t.Fatalf("uncertain mutation not preserved: %+v", events)
	}
}

func TestCancellationAndNoInclusionRespectOutstandingBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := executionRequest()
		r.MaxOutstanding = 2
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		client := &fakeWorkloadRPC{}
		var output bytes.Buffer
		if err := execute(ctx, client, r, 0, json.NewEncoder(&output)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		events := decodeEvents(t, output.Bytes())
		summary := events[len(events)-1]
		if len(client.sent) != 2 || summary.Pending != 2 || summary.Unsent != 4 || client.readers != 0 {
			t.Fatal("backpressure or cancellation failed")
		}
	})
}

func TestSlowObservationsCannotCreateCatchUpBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := executionRequest()
		r.MaxOutstanding = 2
		r.IntervalMilliseconds = 500
		client := &fakeWorkloadRPC{found: true, delay: 10 * time.Second}
		if err := execute(t.Context(), client, r, 0, json.NewEncoder(io.Discard)); err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(client.starts); i++ {
			if client.starts[i].Sub(client.starts[i-1]) < 500*time.Millisecond {
				t.Fatal("catch-up burst")
			}
		}
	})
}

// cancelWriter interrupts execution at the authorization boundary, before mutation.
type cancelWriter struct{ cancel context.CancelFunc }

func (w cancelWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"Authorized"`)) {
		w.cancel()
	}
	return len(p), nil
}

func TestCancellationAfterAuthorizationPreventsSend(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := &fakeWorkloadRPC{}
	if err := execute(
		ctx,
		client,
		executionRequest(),
		0,
		json.NewEncoder(cancelWriter{cancel}),
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatal(err)
	}
	if len(client.sent) != 0 {
		t.Fatal("sent after cancellation")
	}
}

func TestNonceOverflowAndEvidenceFailurePreventMutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		nonce  uint64
		writer io.Writer
	}{
		{"nonce overflow", math.MaxUint64, io.Discard}, {"output failure", 0, errorWriter{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeWorkloadRPC{}
			if execute(t.Context(), client, executionRequest(), tc.nonce, json.NewEncoder(tc.writer)) == nil ||
				len(client.sent) != 0 {
				t.Fatal("unsafe dispatch")
			}
		})
	}
}

// errorWriter makes evidence persistence fail before any authorization.
type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("disk unavailable") }

func TestObservationFailureStopsFurtherSendsWithPendingEvidence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := executionRequest()
		r.MaxOutstanding = 1
		client := &fakeWorkloadRPC{readErr: errors.New("unavailable")}
		var output bytes.Buffer
		if execute(t.Context(), client, r, 0, json.NewEncoder(&output)) == nil {
			t.Fatal("observation error succeeded")
		}
		events := decodeEvents(t, output.Bytes())
		if len(client.sent) != 1 || events[len(events)-1].Pending != 1 ||
			events[len(events)-2].Type != "ObservationUnavailable" {
			t.Fatal("unknown receipt lost")
		}
	})
}

// An empty receipt queue must not keep waking on an expired observation timer.
func TestLongPacingSleepsWhenNothingIsOutstanding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := executionRequest()
		r.Count = 2
		r.IntervalMilliseconds = 60000
		client := &fakeWorkloadRPC{found: true}
		started := time.Now()
		if err := execute(t.Context(), client, r, 0, json.NewEncoder(io.Discard)); err != nil {
			t.Fatal(err)
		}
		if len(client.sent) != 2 || client.starts[1].Sub(started) != time.Minute {
			t.Fatal("pacing changed")
		}
	})
}

func TestReadCompletionTimesSurviveBatchJoin(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := executionRequest()
				r.Count = 2
				r.IntervalMilliseconds = 0
				second, err := buildTransaction(r, 1, 1)
				if err != nil {
					t.Fatal(err)
				}
				client := &fakeWorkloadRPC{
					found:  !failed,
					delays: map[string]time.Duration{second.TxID: 5 * time.Second},
				}
				if failed {
					client.readErr = errors.New("unavailable")
				}
				var output bytes.Buffer
				start := time.Now()
				err = execute(t.Context(), client, r, 0, json.NewEncoder(&output))
				if (err != nil) != failed {
					t.Fatal(err)
				}
				var times []time.Time
				for _, e := range decodeEvents(t, output.Bytes()) {
					if e.Type == "Included" || e.Type == "ObservationUnavailable" {
						times = append(times, e.ObservedAt)
					}
				}
				if len(times) != 2 || !times[0].Equal(start.Add(3*time.Second)) ||
					!times[1].Equal(start.Add(8*time.Second)) {
					t.Fatalf("read completion timestamps lost: %v", times)
				}
			})
		})
	}
}
