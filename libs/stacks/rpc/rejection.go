package rpc

import (
	"encoding/json"
	"net/http"
	"strings"
)

// SubmissionRejection is a matched native rejection, not proof of permanent non-execution.
// It retains only the exact transaction ID and a bounded reason classification.
type SubmissionRejection struct {
	// reason excludes raw server-provided detail.
	reason string
	// txid binds the observation to the submitted transaction.
	txid string
}

// Error reports classification without exposing native response bodies.
func (r *SubmissionRejection) Error() string { return "ingress rejected submission: " + r.reason }

// Reason returns an allowlisted validation reason or Other.
func (r *SubmissionRejection) Reason() string { return r.reason }

// TxID returns the exact submitted transaction identity.
func (r *SubmissionRejection) TxID() string { return r.txid }

// ClassifySubmissionRejection accepts only an exact native HTTP 400 rejection envelope.
// Nil means unconfirmed. The response bound matches the maximum supported client bound.
func ClassifySubmissionRejection(status int, data []byte, txid string) *SubmissionRejection {
	if status != http.StatusBadRequest || len(txid) != 64 || len(data) > 64<<20 {
		return nil
	}
	var rejection struct {
		TxID   string `json:"txid"`
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(data, &rejection) != nil || strings.TrimPrefix(rejection.TxID, "0x") != txid || rejection.Error != nativeRejectionEnvelope || rejection.Reason == "" {
		return nil
	}
	reason := RejectionOther
	if ValidationRejectionReason(rejection.Reason) {
		reason = rejection.Reason
	}
	return &SubmissionRejection{reason: reason, txid: txid}
}

// Definite reports an allowlisted refusal of this ingress attempt, not global non-execution.
func (r *SubmissionRejection) Definite() bool { return ValidationRejectionReason(r.reason) }

// ValidationRejectionReason excludes unknown reasons and server/database failures.
func ValidationRejectionReason(reason string) bool {
	switch reason {
	case RejectionFeeTooLow, RejectionBadNonce, RejectionConflictingNonceInMempool, RejectionNotEnoughFunds:
		return true
	}
	return false
}

// Native rejection names accepted by the bounded ingress classifier.
const (
	// RejectionFeeTooLow identifies the FeeTooLow ingress classification.
	RejectionFeeTooLow = "FeeTooLow"
	// RejectionBadNonce identifies the BadNonce ingress classification.
	RejectionBadNonce = "BadNonce"
	// RejectionConflictingNonceInMempool identifies the ConflictingNonceInMempool ingress classification.
	RejectionConflictingNonceInMempool = "ConflictingNonceInMempool"
	// RejectionNotEnoughFunds identifies the NotEnoughFunds ingress classification.
	RejectionNotEnoughFunds = "NotEnoughFunds"
	// RejectionOther identifies the Other ingress classification.
	RejectionOther = "Other"
)

// nativeRejectionEnvelope distinguishes the native mempool refusal from generic HTTP failures.
const nativeRejectionEnvelope = "transaction rejected"
