package bitcoinobserver

import (
	"context"
	"errors"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoinrpc"
)

// FailureReason is the closed vocabulary emitted for unsuccessful collection attempts.
type FailureReason string

const (
	// FailureRequest means the client could not construct the RPC request.
	FailureRequest FailureReason = "request"
	// FailureUnknown means the error has no recognized collection classification.
	FailureUnknown FailureReason = "unknown"
	// FailureDeadline means the total polling deadline expired at the named step.
	FailureDeadline FailureReason = "deadline"
	// FailureTransport means the HTTP exchange did not complete.
	FailureTransport FailureReason = "transport"
	// FailureResponse means HTTP or RPC did not provide a matching success response.
	FailureResponse FailureReason = "response"
	// FailureValidation means a required fact is missing, malformed or unsupported.
	FailureValidation FailureReason = "validation"
	// FailureBound means the response bytes or branch count exceed collection limits.
	FailureBound FailureReason = "bound"
)

// callFailure maps only typed errors; unknown transport implementations retain a bounded fallback.
func callFailure(ctx context.Context, err error) FailureReason {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return FailureDeadline
	}
	var failure *bitcoinrpc.CallError
	if errors.As(err, &failure) {
		switch failure.Kind {
		case bitcoinrpc.FailureRequest:
			return FailureRequest
		case bitcoinrpc.FailureTransport:
			return FailureTransport
		case bitcoinrpc.FailureResponse:
			return FailureResponse
		case bitcoinrpc.FailureBound:
			return FailureBound
		}
	}
	return FailureUnknown
}
