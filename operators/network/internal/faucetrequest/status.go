// Package faucetrequest admits immutable transfers to an exact surviving faucet worker.
package faucetrequest

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// AdmissionManager owns request admission and lifecycle projections.
	AdmissionManager = "stacks-network-faucet-request"
	// ExecutionManager owns only the bound worker's execution subtree.
	ExecutionManager = "stacks-network-faucet-execution"
	// Capacity bounds active requests and unpersisted outcomes in one surviving worker.
	Capacity = 1000
	// FeeMicroSTX is this release's explicit per-transfer fee, persisted at admission.
	FeeMicroSTX uint64 = 3000
)

// ApplyStatus uses minimal SSA fields and UID/revision preconditions; it cannot adopt replacements.
func ApplyStatus(
	ctx context.Context,
	c client.Client,
	request *stacks.StacksFaucetRequest,
	status stacks.FaucetRequestStatus,
	manager string,
) error {
	if request.UID == "" || request.ResourceVersion == "" ||
		manager != AdmissionManager && manager != ExecutionManager {
		return errors.New("request status requires exact persisted identity and field manager")
	}
	if manager == ExecutionManager && (status.Admission != nil || status.Phase != "" || len(status.Conditions) != 0) ||
		manager == AdmissionManager && status.Execution != nil {
		return errors.New("request status writer exceeded field ownership")
	}
	patch := &stacks.StacksFaucetRequest{
		TypeMeta: metav1.TypeMeta{APIVersion: stacks.GroupVersion.String(), Kind: stacks.KindStacksFaucetRequest},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       request.Namespace,
			Name:            request.Name,
			UID:             request.UID,
			ResourceVersion: request.ResourceVersion,
		},
		Status: *status.DeepCopy(),
	}
	if err := c.Status().
		//nolint:staticcheck // Typed minimal SSA preserves the response; generated apply configurations are not available.
		Patch(ctx, patch, client.Apply, client.FieldOwner(manager), client.ForceOwnership); err != nil {
		return err
	}
	*request = *patch
	return nil
}

// Deadline independently derives the original absolute expiry without rounding subsecond timeouts.
func Deadline(request *stacks.StacksFaucetRequest) (time.Time, error) {
	duration, err := time.ParseDuration(string(request.Spec.Timeout))
	if err != nil || duration <= 0 || duration > time.Hour || request.CreationTimestamp.IsZero() {
		return time.Time{}, errors.New("invalid request lifetime")
	}
	return request.CreationTimestamp.Add(duration).UTC(), nil
}

// ValidAddress accepts canonical standard testnet principals only.
func ValidAddress(address string) bool {
	version, _, err := identity.DecodeAddress(address)
	return err == nil && version == 26
}

// MatchingExecution rejects evidence belonging to another admission or worker incarnation.
func MatchingExecution(request *stacks.StacksFaucetRequest) bool {
	a, e := request.Status.Admission, request.Status.Execution
	return a != nil && a.Decision == stacks.FaucetDecisionAdmitted && a.Faucet != nil && a.Worker != nil && e != nil &&
		e.NetworkUID == a.NetworkUID &&
		e.FaucetUID == a.Faucet.UID &&
		e.WorkerUID == a.Worker.UID &&
		e.ProcessNonce != "" &&
		e.Destination == a.Destination &&
		e.AmountMicroSTX == a.AmountMicroSTX
}

// TerminalExecution requires no-send, classified native refusal, or exact inclusion evidence.
func TerminalExecution(execution *stacks.FaucetExecution) bool {
	if execution == nil {
		return false
	}
	//nolint:exhaustive // Only settled outcomes are terminal; submitted and uncertain outcomes remain pending.
	switch execution.Phase {
	case stacks.FaucetExecutionCompleted:
		return !execution.NoSend && len(execution.TxID) == 64 && len(execution.InclusionBlockID) == 64
	case stacks.FaucetExecutionRejected:
		return execution.NoSend && execution.TxID == "" ||
			!execution.NoSend && len(execution.TxID) == 64 &&
				(len(execution.InclusionBlockID) == 64 ||
					strings.HasPrefix(
						execution.Reason,
						stacks.RejectionReasonPrefix,
					) &&
						rpc.ValidationRejectionReason(strings.TrimPrefix(
							execution.Reason,
							stacks.RejectionReasonPrefix,
						)))
	case stacks.FaucetExecutionExpired:
		return execution.NoSend && execution.TxID == ""
	}
	return false
}

// ProjectPhase never interprets absent admitted execution as proof that no send occurred.
func ProjectPhase(request *stacks.StacksFaucetRequest, now time.Time) (stacks.FaucetPhase, string) {
	a := request.Status.Admission
	if a == nil {
		return stacks.FaucetPending, reasonAdmissionPending
	}
	if a.Decision == stacks.FaucetDecisionRejected || a.Decision == stacks.FaucetDecisionExpired {
		return stacks.FaucetPhase(a.Decision), a.Reason
	}
	if a.Decision != stacks.FaucetDecisionAdmitted {
		return stacks.FaucetPending, a.Reason
	}
	if MatchingExecution(request) {
		e := request.Status.Execution
		if TerminalExecution(e) {
			return stacks.FaucetPhase(e.Phase), e.Reason
		}
	}
	deadline, err := Deadline(request)
	if err != nil || !now.Before(deadline) {
		return stacks.FaucetInconclusive, api.ReasonDeadlineOutcomeUnknown
	}
	if MatchingExecution(request) && request.Status.Execution.Phase == stacks.FaucetExecutionSubmitted {
		return stacks.FaucetSubmitted, request.Status.Execution.Reason
	}
	if MatchingExecution(request) && request.Status.Execution.Phase == stacks.FaucetExecutionInconclusive {
		return stacks.FaucetInconclusive, request.Status.Execution.Reason
	}
	return stacks.FaucetPending, reasonAwaitingWorkerEvidence
}
