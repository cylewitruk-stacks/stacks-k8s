package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
)

// observationErrorWriter fails only after submissions, at the first observation-error event.
type observationErrorWriter struct{ output io.Writer }

func (w observationErrorWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"ObservationUnavailable"`)) {
		return 0, io.ErrClosedPipe
	}
	return w.output.Write(p)
}

func TestSweepPreservesRPCErrorAndEvidenceFailure(t *testing.T) {
	for _, outputFails := range []bool{false, true} {
		name := "rpc error"
		if outputFails {
			name = "rpc and evidence errors"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cause := errors.New("native RPC observation unavailable (HTTP 500)")
				client := &fakeWorkloadRPC{readErr: cause}
				var output bytes.Buffer
				var writer io.Writer = &output
				if outputFails {
					writer = observationErrorWriter{&output}
				}
				err := execute(t.Context(), client, executionRequest(), 0, json.NewEncoder(writer))
				if !errors.Is(err, cause) || !strings.Contains(err.Error(), "3 inclusion observations unavailable") ||
					strings.Count(err.Error(), cause.Error()) != 1 {
					t.Fatalf("lost or repeated observation cause: %v", err)
				}
				if len(client.sent) != 3 || client.readers != 0 {
					t.Fatal("sweep did not stop sends or join readers")
				}
				if outputFails {
					if !strings.Contains(err.Error(), "write workload evidence") {
						t.Fatal("evidence failure was masked")
					}
					return
				}
				events := decodeEvents(t, output.Bytes())
				unavailable := 0
				for _, event := range events {
					if event.Type == "ObservationUnavailable" {
						unavailable++
					}
				}
				last := events[len(events)-1]
				if unavailable != 3 || last.Pending != 3 || last.Unsent != 3 || *last.Completed {
					t.Fatalf("pending outcomes lost: %+v", last)
				}
			})
		})
	}
}

func TestCancellationDuringSweepPreservesContextAndPending(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "canceled"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := executionRequest()
				r.Count, r.MaxOutstanding, r.IntervalMilliseconds = 8, 5, 0
				ctx, cancel := context.WithCancel(t.Context())
				want := context.Canceled
				if deadline {
					cancel()
					ctx, cancel = context.WithTimeout(t.Context(), 4*time.Second)
					want = context.DeadlineExceeded
				} else {
					go func() { time.Sleep(4 * time.Second); cancel() }()
				}
				defer cancel()
				client := &fakeWorkloadRPC{delay: 10 * time.Second}
				var output bytes.Buffer
				err := execute(ctx, client, r, 0, json.NewEncoder(&output))
				if !errors.Is(err, want) || !strings.Contains(err.Error(), "5 inclusion observations unavailable") ||
					strings.Count(err.Error(), want.Error()) != 1 {
					t.Fatalf("cancellation identity lost or repeated: %v", err)
				}
				if len(client.sent) != 5 || client.peakReaders != 2 || client.readers != 0 {
					t.Fatal("cancellation did not interrupt active bounded reads")
				}
				events := decodeEvents(t, output.Bytes())
				unavailable := 0
				for _, event := range events {
					if event.Type == "ObservationUnavailable" {
						unavailable++
					}
				}
				last := events[len(events)-1]
				if unavailable != 5 || last.Pending != 5 || last.Unsent != 3 || *last.Completed {
					t.Fatalf("canceled pending outcomes lost: %+v", last)
				}
			})
		})
	}
}

// sanitizedCancellationRPC models a transport that deliberately hides raw network errors.
type sanitizedCancellationRPC struct {
	*fakeWorkloadRPC
	failure error
}

func (c sanitizedCancellationRPC) Inclusion(ctx context.Context, id string) (rpc.Inclusion, error) {
	value, err := c.fakeWorkloadRPC.Inclusion(ctx, id)
	if err != nil && ctx.Err() != nil {
		return value, c.failure
	}
	return value, err
}

func TestSweepRetainsContextAlongsideSanitizedRPCError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()
		cause := errors.New("native RPC transport unavailable")
		client := sanitizedCancellationRPC{&fakeWorkloadRPC{delay: 10 * time.Second}, cause}
		var output bytes.Buffer
		err := execute(ctx, client, executionRequest(), 0, json.NewEncoder(&output))
		if !errors.Is(err, cause) || !errors.Is(err, context.DeadlineExceeded) ||
			client.peakReaders != 2 || client.readers != 0 || len(client.sent) != 3 {
			t.Fatalf("sanitized failure masked cancellation or reader lifetime: %v", err)
		}
	})
}
