// Package bitcoinobserver samples read-only Core RPC facts for metrics and structured logs.
package bitcoinobserver

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	bitcoinrpc "github.com/cylewitruk-stacks/stacks-k8s/libs/bitcoin/rpc"
)

const (
	// Username identifies the dedicated method-restricted observer principal.
	Username = "observer"
	// MetricsPort is the exporter Prometheus listener port.
	MetricsPort = 9332
	// ContainerName identifies the optional actor sidecar.
	ContainerName = "bitcoin-observer"
	// PollTimeout bounds a complete five-method sample.
	PollTimeout = 5 * time.Second
	// MinInterval bounds the fastest supported polling cadence.
	MinInterval = 5 * time.Second
	// MaxInterval bounds the slowest supported polling cadence.
	MaxInterval = 300 * time.Second
	// MaxTips bounds the complete branch inventory.
	MaxTips = 128
	// DefaultInterval separates completed polls; there is no catch-up loop.
	DefaultInterval = 10 * time.Second
	// EventObservation identifies structured sample records in container logs.
	EventObservation = "BitcoinObservation"
)

// Methods returns the complete read-only RPC allowlist used by the exporter.
func Methods() []string {
	return []string{
		bitcoinrpc.MethodGetBlockchainInfo, bitcoinrpc.MethodGetChainTips,
		bitcoinrpc.MethodGetNetworkInfo, bitcoinrpc.MethodGetMempoolInfo, bitcoinrpc.MethodGetNetTotals,
	}
}

// RPC is the existing bounded, non-retrying Core transport boundary.
type RPC interface {
	Call(ctx context.Context, endpoint, id, method string, params []any, result any) error
}

// Chain contains facts from one getblockchaininfo response, without floating-point chainwork conversion.
type Chain struct {
	// Chain is the native chain name.
	Chain string `json:"chain"`
	// Blocks is the selected tip height.
	Blocks int64 `json:"blocks"`
	// Headers is the best known header height.
	Headers int64 `json:"headers"`
	// BestBlockHash identifies the selected tip.
	BestBlockHash string `json:"bestblockhash"`
	// Chainwork is Core's exact hexadecimal cumulative work.
	Chainwork string `json:"chainwork"`
	// InitialBlockDownload reports Core's synchronization heuristic.
	InitialBlockDownload bool `json:"initialblockdownload"`
}

// Tip is one independently observed branch tip; it is not a reorganization conclusion.
type Tip struct {
	// Height is the branch tip height.
	Height int64 `json:"height"`
	// Hash identifies the branch tip.
	Hash string `json:"hash"`
	// BranchLength is the distance from the active branch fork point.
	BranchLength int64 `json:"branchlen"`
	// Status is Core's bounded native branch classification.
	Status string `json:"status"`
}

// Record is one observation attempt. Success requires every bounded RPC and validation to succeed.
type Record struct {
	// Event identifies this log schema.
	Event string `json:"event"`
	// StartedAt and ObservedAt bound sequential reads, not an atomic multi-RPC snapshot.
	StartedAt  time.Time `json:"startedAt"`
	ObservedAt time.Time `json:"observedAt"`
	// Success separates protocol observations from unavailable collection.
	Success bool `json:"success"`
	// FailedMethod identifies the failed step, not the cause of a shared deadline.
	FailedMethod string `json:"failedMethod,omitempty"`
	// FailedReason classifies collection failure without server-controlled text.
	FailedReason FailureReason `json:"failedReason,omitempty"`
	// Chain and Tips are present only for complete successful attempts.
	Chain *Chain `json:"chain,omitempty"`
	Tips  []Tip  `json:"tips,omitempty"`
}

// networkFacts projects only fields owned by getnetworkinfo.
type networkFacts struct {
	Connections int64 `json:"connections"`
}

// mempoolFacts projects only fields owned by getmempoolinfo.
type mempoolFacts struct {
	Size  int64 `json:"size"`
	Bytes int64 `json:"bytes"`
	Usage int64 `json:"usage"`
}

// trafficFacts projects only fields owned by getnettotals.
type trafficFacts struct {
	TotalBytesRecv uint64 `json:"totalbytesrecv"`
	TotalBytesSent uint64 `json:"totalbytessent"`
}

// numericFacts is the bounded numerical projection of a successful sample.
type numericFacts struct {
	networkFacts
	mempoolFacts
	trafficFacts
}

// Observer publishes immutable snapshots while the single polling goroutine owns RPC activity.
type Observer struct {
	rpc         RPC
	endpoint    string
	interval    time.Duration
	now         func() time.Time
	mu          sync.RWMutex
	record      Record
	facts       numericFacts
	lastSuccess time.Time
	failures    uint64
}

// New validates polling bounds and creates an observer with no Kubernetes dependencies.
func New(rpc RPC, endpoint string, interval time.Duration) (*Observer, error) {
	if rpc == nil || endpoint == "" || interval < MinInterval || interval > MaxInterval {
		return nil, fmt.Errorf("observer requires RPC, endpoint and interval between 5s and 300s")
	}
	return &Observer{rpc: rpc, endpoint: endpoint, interval: interval, now: time.Now}, nil
}

// Run polls immediately and then waits after each completed attempt; emit must be serial and bounded by its sink.
func (o *Observer) Run(ctx context.Context, emit func(Record)) {
	for {
		if ctx.Err() != nil {
			return
		}
		record, err := o.Poll(ctx)
		if err != nil || ctx.Err() != nil {
			return
		}
		emit(record)
		timer := time.NewTimer(o.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Poll collects one sample. Parent cancellation returns an error without publishing or accounting it.
// Call from one goroutine; scrapes never trigger RPC requests.
func (o *Observer) Poll(parent context.Context) (Record, error) {
	if err := parent.Err(); err != nil {
		return Record{}, err
	}
	started := o.now().UTC()
	ctx, cancel := context.WithTimeout(parent, PollTimeout)
	defer cancel()
	chain := &Chain{}
	tips := []Tip{}
	facts := numericFacts{}
	record := Record{Event: EventObservation, StartedAt: started}
	steps := []struct {
		method   string
		required []string
		target   any
	}{
		{
			bitcoinrpc.MethodGetBlockchainInfo,
			[]string{"chain", "blocks", "headers", "bestblockhash", "chainwork", "initialblockdownload"},
			chain,
		},
		{bitcoinrpc.MethodGetChainTips, nil, &tips},
		{bitcoinrpc.MethodGetNetworkInfo, []string{"connections"}, &facts.networkFacts},
		{bitcoinrpc.MethodGetMempoolInfo, []string{"size", "bytes", "usage"}, &facts.mempoolFacts},
		{bitcoinrpc.MethodGetNetTotals, []string{"totalbytesrecv", "totalbytessent"}, &facts.trafficFacts},
	}
	for _, step := range steps {
		var raw json.RawMessage
		err := o.rpc.Call(ctx, o.endpoint, step.method, step.method, nil, &raw)
		if err != nil {
			record.FailedMethod = step.method
			record.FailedReason = callFailure(ctx, err)
			break
		}
		if !requiredFields(raw, step.required) || json.Unmarshal(raw, step.target) != nil {
			record.FailedMethod, record.FailedReason = step.method, FailureValidation
			break
		}
		valid := true
		reason := FailureValidation
		switch step.method {
		case bitcoinrpc.MethodGetBlockchainInfo:
			valid = chain.Chain == bitcoinrpc.ChainRegtest && chain.Blocks >= 0 && chain.Headers >= 0 &&
				hashValid(chain.BestBlockHash) && hashValid(chain.Chainwork)
		case bitcoinrpc.MethodGetChainTips:
			if len(tips) > MaxTips {
				valid, reason = false, FailureBound
			} else {
				valid = len(tips) > 0
			}
			for _, tip := range tips {
				valid = valid && hashValid(tip.Hash) && tip.Height >= 0 && tip.BranchLength >= 0 &&
					tipStatusValid(tip.Status)
			}
		case bitcoinrpc.MethodGetNetworkInfo:
			valid = facts.Connections >= 0
		case bitcoinrpc.MethodGetMempoolInfo:
			valid = facts.Size >= 0 && facts.Bytes >= 0 && facts.Usage >= 0
		case bitcoinrpc.MethodGetNetTotals:
			// Required uint64 fields reject negative, overflowing and non-integer values during decoding.
		}
		if !valid {
			record.FailedMethod, record.FailedReason = step.method, reason
			break
		}
	}
	if err := parent.Err(); err != nil {
		return Record{}, err
	}
	record.ObservedAt = o.now().UTC()
	record.Success = record.FailedMethod == ""
	if record.Success {
		record.Chain = chain
		record.Tips = tips
	}
	o.mu.Lock()
	o.record = record
	o.facts = facts
	if record.Success {
		o.lastSuccess = record.ObservedAt
	} else {
		o.failures++
	}
	o.mu.Unlock()
	return record, nil
}

// requiredFields distinguishes absent/null protocol fields from valid zero values.
func requiredFields(raw []byte, fields []string) bool {
	if len(fields) == 0 {
		return string(raw) != "null"
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return false
	}
	for _, field := range fields {
		if len(values[field]) == 0 || string(values[field]) == "null" {
			return false
		}
	}
	return true
}

// hashValid bounds changing protocol identities and prevents arbitrary text in records.
func hashValid(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// tipStatusValid accepts Core's branch classification vocabulary only.
func tipStatusValid(s string) bool {
	switch s {
	case bitcoinrpc.ChainTipActive,
		bitcoinrpc.ChainTipInvalid,
		bitcoinrpc.ChainTipValidFork,
		bitcoinrpc.ChainTipValidHeaders,
		bitcoinrpc.ChainTipHeadersOnly,
		bitcoinrpc.ChainTipUnknown:
		return true
	}
	return false
}
