package stacksoperation

import (
	"context"
	"fmt"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// captureInclusion transfers the original ingress without recounting a transaction.
func (r *TransferRole) captureInclusion() {
	last := r.stream.Facts().LastInclusion
	if r.pendingTarget == nil || r.stream.Pending() != 0 || last == nil {
		return
	}
	r.includedTarget = r.pendingTarget
	r.pendingTarget = nil
	observation := r.includedTarget.Target
	observation.TxID = last.TxID
	r.traffic = &observation
	r.nextObservation = time.Time{}
}

// trafficEvidence publishes only completed canonical reads; inclusion receipts are independent.
func (r *TransferRole) trafficEvidence() *api.TrafficObservation {
	if r.traffic == nil || r.traffic.ObservedAt.IsZero() {
		return nil
	}
	return r.traffic.DeepCopy()
}

// observeTraffic brackets native inclusion at one exact canonical tip on its original ingress.
func (r *TransferRole) observeTraffic(ctx context.Context, now time.Time) {
	if r.includedTarget == nil || r.traffic == nil {
		return
	}
	// #nosec G115 -- Applied traffic intervals are 1s..1h; their progress windows are positive and bounded.
	r.traffic.EffectiveIntervalSeconds = uint64(r.interval / time.Second)
	// #nosec G115 -- Applied traffic intervals are 1s..1h; their progress windows are positive and bounded.
	r.traffic.ProgressWindowSeconds = uint64(foundation.ProgressWindow(r.interval) / time.Second)
	if now.Before(r.nextObservation) {
		return
	}
	r.nextObservation = now.Add(time.Duration(foundation.ObservationPolicy().PollIntervalSeconds) * time.Second)
	r.traffic.Available = false
	ctx, cancel := context.WithTimeout(
		ctx,
		time.Duration(foundation.ObservationPolicy().RPCAllowanceSeconds)*time.Second,
	)
	defer cancel()
	node := r.includedTarget.Node
	before, err := node.ChainView(ctx)
	if err != nil || !before.FullySynced || before.NetworkID != 0x80000000 {
		return
	}
	inclusion, err := node.Inclusion(ctx, r.traffic.TxID)
	if err != nil {
		return
	}
	after, err := node.ChainView(ctx)
	if err != nil || before != after {
		return
	}
	observation := r.traffic.DeepCopy()
	observation.Available = true
	observation.Found = inclusion.Found
	observation.Success = inclusion.Found && inclusion.Success
	observation.BlockID = ""
	if inclusion.Found {
		observation.BlockID = inclusion.BlockID
	}
	observation.IndexBlockID = before.IndexBlockID
	observation.BurnHeight = before.BurnHeight
	observation.ObservedAt = metav1.NewTime(now.UTC().Truncate(time.Second))
	r.traffic = observation
}

// authorize checks the prepared ingress binding before authorizing its original signed bytes.
func (r *TransferRole) authorize(s stacksworker.Snapshot, prepared TransferInputs) func(context.Context) error {
	return func(ctx context.Context) error {
		if s.Authorize == nil {
			return fmt.Errorf("submission authorization unavailable")
		}
		if err := s.Authorize(ctx); err != nil {
			return err
		}
		current, err := r.Resolve(ctx, s)
		if err != nil {
			if s.AppliedFallback != nil {
				if fallback, ok := s.AppliedFallback(
					r.applied,
					err,
				); ok && !fallback.Paused &&
					fallback.Authorize != nil {
					return fallback.Authorize(ctx)
				}
			}
			return err
		}
		if !trafficBindingEqual(current.Target, prepared.Target) {
			r.inputs.invalidate(s, r.applied)
			return fmt.Errorf("prepared ingress identity changed")
		}
		return nil
	}
}

// trafficBindingEqual compares public ingress identity independently of cadence and observations.
func trafficBindingEqual(a, b api.TrafficObservation) bool {
	return a.TargetParticipantUID == b.TargetParticipantUID && a.TargetPodUID == b.TargetPodUID &&
		a.TargetContainerID == b.TargetContainerID &&
		a.TargetConfigurationDigest == b.TargetConfigurationDigest &&
		a.GenesisUID == b.GenesisUID
}
