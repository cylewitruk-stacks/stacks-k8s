package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

// eventType describes an evidence boundary, not transaction finality.
type eventType string

const (
	eventStarted                eventType = "Started"
	eventAuthorized             eventType = "Authorized"
	eventSubmitted              eventType = "Submitted"
	eventIncluded               eventType = "Included"
	eventUnconfirmed            eventType = "SubmissionUnconfirmed"
	eventRejected               eventType = "Rejected"
	eventObservationUnavailable eventType = "ObservationUnavailable"
	eventFinished               eventType = "Finished"
	observationInterval                   = 3 * time.Second
)

// experimentEvent contains public inputs and outcomes; credentials are never copied into it.
type experimentEvent struct {
	Type                   eventType     `json:"type"`
	Inputs                 *publicInputs `json:"inputs,omitempty"`
	ObservedAt             time.Time     `json:"observedAt"`
	TxID                   string        `json:"txid,omitempty"`
	Nonce                  *uint64       `json:"nonce,omitempty"`
	Ordinal                *int          `json:"ordinal,omitempty"`
	Bytes                  int           `json:"bytes,omitempty"`
	BlockID                string        `json:"blockID,omitempty"`
	Success                *bool         `json:"success,omitempty"`
	Shape                  *callShape    `json:"shape,omitempty"`
	PlanVersion            string        `json:"planVersion,omitempty"`
	Variation              *variation    `json:"variation,omitempty"`
	Count                  int           `json:"count,omitempty"`
	IntervalMilliseconds   int           `json:"intervalMilliseconds,omitempty"`
	MaxOutstanding         int           `json:"maxOutstanding,omitempty"`
	ObservationConcurrency int           `json:"observationConcurrency,omitempty"`
	DurationSeconds        int           `json:"durationSeconds,omitempty"`
	// Attempted includes a submission whose acknowledgement was lost.
	Attempted int   `json:"attempted,omitempty"`
	Included  int   `json:"included,omitempty"`
	Pending   int   `json:"pending,omitempty"`
	Unsent    int   `json:"unsent,omitempty"`
	Completed *bool `json:"completed,omitempty"`
}

// workloadRPC separates ordered mutations from concurrently safe, read-only observations.
type workloadRPC interface {
	Submit(context.Context, transaction.Transaction) error
	Inclusion(context.Context, string) (rpc.Inclusion, error)
}

// pendingTransaction retains only public identity after submission.
type pendingTransaction struct {
	event experimentEvent
}

// execute sends in nonce order and drains observations within the caller's total deadline.
func execute(
	ctx context.Context,
	client workloadRPC,
	r submitRequest,
	nonce uint64,
	output *json.Encoder,
) (result error) {
	count := r.Count
	if r.ContractSource != "" {
		count = 1
	}
	// #nosec G115 -- Count is positive and bounded by request validation.
	if nonce > math.MaxUint64-uint64(count-1) {
		return errors.New("nonce sequence overflows")
	}
	if r.MaxOutstanding == 0 {
		r.MaxOutstanding = 100
	}
	if r.ObservationConcurrency == 0 {
		r.ObservationConcurrency = 1
	}
	emit := func(event experimentEvent) error {
		if event.ObservedAt.IsZero() {
			event.ObservedAt = time.Now().UTC()
		}
		if err := output.Encode(event); err != nil {
			return errors.New("write workload evidence")
		}
		return nil
	}
	if err := emit(experimentEvent{
		Type: eventStarted, Nonce: &nonce, Count: count, PlanVersion: planVersion, Inputs: publicInputEvidence(r),
		Variation: r.Variation, IntervalMilliseconds: r.IntervalMilliseconds, MaxOutstanding: r.MaxOutstanding,
		ObservationConcurrency: r.ObservationConcurrency, DurationSeconds: r.DurationSeconds,
	}); err != nil {
		return err
	}
	pending := make([]pendingTransaction, 0, r.MaxOutstanding)
	attempted, included := 0, 0
	defer func() {
		completed := result == nil
		result = errors.Join(result, emit(experimentEvent{
			Type: eventFinished, Attempted: attempted, Included: included,
			Pending: len(pending), Unsent: count - attempted, Completed: &completed,
		}))
	}()
	start := time.Now()
	nextSend, nextPoll := start, start.Add(observationInterval)
	var sendUntil time.Time
	if r.DurationSeconds > 0 {
		sendUntil = start.Add(time.Duration(r.DurationSeconds) * time.Second)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := time.Now()
		canSend := attempted < count && (sendUntil.IsZero() || now.Before(sendUntil))
		if !canSend && len(pending) == 0 {
			return nil
		}
		if len(pending) > 0 && !now.Before(nextPoll) {
			results := observeBatch(ctx, client, pending, r.ObservationConcurrency)
			retained := pending[:0]
			var observeErr, firstObservationErr error
			failedObservations := 0
			for i, observation := range results {
				item := pending[i]
				item.event.ObservedAt = observation.observedAt
				if observation.err != nil {
					item.event.Type = eventObservationUnavailable
					failedObservations++
					if firstObservationErr == nil {
						firstObservationErr = observation.err
					}
					observeErr = errors.Join(observeErr, emit(item.event))
				} else if observation.inclusion.Found {
					item.event.Type = eventIncluded
					item.event.BlockID = observation.inclusion.BlockID
					item.event.Success = &observation.inclusion.Success
					if err := emit(item.event); err != nil {
						observeErr = errors.Join(observeErr, err)
					} else {
						included++
						continue
					}
				}
				retained = append(retained, item)
			}
			pending = retained
			if firstObservationErr != nil {
				observeErr = errors.Join(observeErr,
					fmt.Errorf("%d inclusion observations unavailable: %w", failedObservations, firstObservationErr))
			}
			if ctxErr := ctx.Err(); ctxErr != nil && !errors.Is(observeErr, ctxErr) {
				observeErr = errors.Join(observeErr, ctxErr)
			}
			if observeErr != nil {
				return observeErr
			}
			nextPoll = time.Now().Add(observationInterval)
			continue
		}
		if canSend && len(pending) < r.MaxOutstanding && !now.Before(nextSend) {
			selected := variedRequest(r, attempted)
			// #nosec G115 -- Attempted is a nonnegative ordinal bounded by count.
			currentNonce := nonce + uint64(attempted)
			tx, err := buildTransaction(selected, currentNonce, attempted)
			if err != nil {
				return err
			}
			ordinal := attempted
			event := experimentEvent{
				Type:    eventAuthorized,
				TxID:    tx.TxID,
				Nonce:   &currentNonce,
				Ordinal: &ordinal,
				Bytes:   len(tx.Bytes),
			}
			if r.ContractSource == "" {
				// #nosec G115 -- The nonnegative ordinal and key sum were bounded before execution.
				event.Shape = &callShape{
					selected.Function, selected.Writes, selected.Reads, selected.PayloadBytes,
					r.KeyBase + uint64(attempted)*64,
				}
			}
			if err := emit(event); err != nil {
				return err
			}
			// Evidence/signing may block: recheck cancellation and submission cutoff before mutation.
			if err := ctx.Err(); err != nil {
				return err
			}
			if !sendUntil.IsZero() && !time.Now().Before(sendUntil) {
				continue
			}
			nextSend = time.Now().Add(time.Duration(r.IntervalMilliseconds) * time.Millisecond)
			pending = append(pending, pendingTransaction{event: event})
			attempted++
			if err := client.Submit(ctx, tx); err != nil {
				event.Type = eventUnconfirmed
				var rejected *rpc.SubmissionRejection
				if errors.As(err, &rejected) {
					event.Type = eventRejected
				}
				return errors.Join(fmt.Errorf("transaction %s submission outcome: %w", tx.TxID, err), emit(event))
			}
			event.Type = eventSubmitted
			if err := emit(event); err != nil {
				return err
			}
			continue
		}
		wake := nextPoll
		if len(pending) == 0 {
			wake = nextSend
		}
		if canSend && len(pending) < r.MaxOutstanding && nextSend.Before(wake) {
			wake = nextSend
		}
		if canSend && !sendUntil.IsZero() && sendUntil.Before(wake) {
			wake = sendUntil
		}
		timer := time.NewTimer(time.Until(wake))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// observationResult binds a read to its pending ordinal; output order ignores goroutine completion order.
type observationResult struct {
	inclusion  rpc.Inclusion
	observedAt time.Time
	err        error
}

// observeBatch bounds active read-only requests and joins all readers before returning.
func observeBatch(
	ctx context.Context,
	client workloadRPC,
	pending []pendingTransaction,
	concurrency int,
) []observationResult {
	results := make([]observationResult, len(pending))
	jobs := make(chan int)
	var group sync.WaitGroup
	for range min(concurrency, len(pending)) {
		group.Go(func() {
			for index := range jobs {
				if ctx.Err() != nil {
					results[index].err = ctx.Err()
					continue
				}
				results[index].inclusion, results[index].err = client.Inclusion(ctx, pending[index].event.TxID)
				results[index].observedAt = time.Now().UTC()
			}
		})
	}
	for index := range pending {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	return results
}
