package rpc

// FailureKind classifies Call failures without exposing transport or server-controlled details.
type FailureKind string

const (
	// FailureRequest means the client could not construct the request.
	FailureRequest FailureKind = "request"
	// FailureTransport means no complete HTTP response was received.
	FailureTransport FailureKind = "transport"
	// FailureResponse means the response was not a matching, decodable success receipt.
	FailureResponse FailureKind = "response"
	// FailureBound means the response exceeded the transport's byte limit.
	FailureBound FailureKind = "bound"
)

// CallError preserves a redacted diagnostic and a bounded machine-readable classification.
// It does not retain raw errors, response bodies or credentials. It does not authorize retries.
type CallError struct {
	// Kind identifies the failed part of a call; context deadlines are observed separately.
	Kind    FailureKind
	message string
}

// Error returns only the client-controlled diagnostic.
func (e *CallError) Error() string { return e.message }
