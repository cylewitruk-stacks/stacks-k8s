package main

import (
	"context"
	"errors"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
)

// inspectStacks pins dependent reads to the exact initial index block ID.
func inspectStacks(ctx context.Context, r request, w *writer) error {
	c, err := stacks.New(
		stacks.Config{
			Endpoint:         r.Endpoint,
			Timeout:          time.Duration(r.TimeoutSeconds) * time.Second,
			MaxResponseBytes: 1 << 20,
		},
	)
	if err != nil {
		return err
	}
	start := time.Now().UTC()
	view, err := c.ChainView(ctx)
	w.emit(recordStacksTip, start, nil, publicTip(view), err)
	if err != nil || w.err != nil {
		return errors.Join(err, w.err)
	}
	start = time.Now().UTC()
	pox, err := c.PoXAt(ctx, view.IndexBlockID)
	var poxOutput any
	if err == nil {
		poxOutput = publicPoX(view.IndexBlockID, pox)
	}
	w.emit(recordPoX, start, &subject{IndexBlockID: view.IndexBlockID}, poxOutput, err)
	result := err
	cycles := r.Cycles
	if len(cycles) == 0 && err == nil {
		if pox.RewardCycle >= 9999999999 {
			return errors.New("inferred reward cycles exceed supported bound")
		}
		cycles = []uint64{pox.RewardCycle, pox.RewardCycle + 1}
	}
	for _, cycle := range cycles {
		if ctx.Err() != nil || w.err != nil {
			return errors.Join(result, ctx.Err(), w.err)
		}
		start = time.Now().UTC()
		set, e := c.StackerSet(ctx, cycle, view.IndexBlockID)
		var setOutput any
		if e == nil {
			setOutput = publicRewardSet(view.IndexBlockID, cycle, set)
		}
		w.emit(recordRewardSet, start, &subject{IndexBlockID: view.IndexBlockID, Cycle: &cycle}, setOutput, e)
		result = errors.Join(result, e)
	}
	return result
}
