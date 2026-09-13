//go:build live

package publicintegration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	stacksidentity "github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// faucetSelection pins public request inputs and one surviving worker session.
type faucetSelection struct {
	participant, worker                      identity
	logicalName, processNonce, profileDigest string
	destination, source, target              stacks.FaucetBinding
	address                                  string
	amount                                   common.Amount
	offered, included, completed             uint64
}

// selectedFaucet requires a unique selected faucet with its exact allocated participant and worker.
func selectedFaucet(root *api.StacksNetwork, participants []participantEvidence) (participantEvidence, *api.WorkerSession, error) {
	var name string
	for _, entry := range root.Spec.Participants {
		if entry.Kind == "StacksFaucet" {
			if name != "" {
				return participantEvidence{}, nil, fmt.Errorf("faucet qualification requires one selected faucet")
			}
			name = entry.Name
		}
	}
	for _, entry := range root.Status.Identities {
		if entry.Name != name || entry.Removing || entry.Worker == nil || entry.Worker.Shutdown != nil {
			continue
		}
		for _, p := range participants {
			if p.Identity.UID == entry.UID && p.Name == name && p.Kind == "StacksFaucet" {
				return p, entry.Worker, nil
			}
		}
	}
	return participantEvidence{}, nil, fmt.Errorf("selected faucet worker identity unavailable")
}

// faucetInputs rechecks public identities immediately before creating independent requests.
func (h *harness) faucetInputs(ctx context.Context, s snapshot) (faucetSelection, error) {
	var selection faucetSelection
	var root api.StacksNetwork
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: "network"}, &root); err != nil {
		return selection, err
	}
	if root.UID != h.rootUID || root.DeletionTimestamp != nil || root.Spec.Operation != "Running" || !condition(snapshot{Root: objectIdentity(&root), Status: root.Status}, "Initialized", metav1.ConditionTrue) || !condition(snapshot{Root: objectIdentity(&root), Status: root.Status}, "Operational", metav1.ConditionTrue) || s.Root.Generation != root.Generation {
		return selection, fmt.Errorf("faucet qualification requires current initialized operation")
	}
	found, worker, err := selectedFaucet(&root, s.Participants)
	if err != nil {
		return selection, err
	}
	var p api.StacksNetworkParticipant
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: found.Identity.Name}, &p); err != nil {
		return selection, err
	}
	if p.UID != found.Identity.UID || p.DeletionTimestamp != nil || !metav1.IsControlledBy(&p, &root) || p.Spec.NetworkUID != root.UID || p.Spec.ParticipantName != found.Name || p.Spec.Kind != "StacksFaucet" || p.Status.Admission == nil || p.Status.Admission.Configuration.StacksFaucet == nil {
		return selection, fmt.Errorf("faucet participant changed")
	}
	execution := p.Status.Execution
	if execution == nil || execution.PodUID != worker.Pod.UID || execution.ProfileDigest != worker.ProfileDigest || execution.ProcessNonce == "" || execution.Pending != 0 || execution.Phase != "Active" {
		return selection, fmt.Errorf("faucet worker is not an idle active process")
	}
	var pod corev1.Pod
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: worker.Pod.Name}, &pod); err != nil {
		return selection, err
	}
	if pod.UID != worker.Pod.UID || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return selection, fmt.Errorf("faucet worker Pod changed")
	}
	var originalUID types.UID
	for _, declared := range h.declared.reusable {
		if declared.GetKind() == "StacksAccount" && declared.GetName() == "traffic-recipient" {
			originalUID = declared.GetUID()
		}
	}
	var recipient stacks.StacksAccount
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "traffic-recipient"}, &recipient); err != nil {
		return selection, err
	}
	if originalUID == "" || recipient.UID != originalUID || recipient.DeletionTimestamp != nil || recipient.Status.Identity == nil || recipient.Status.Digest == "" {
		return selection, fmt.Errorf("declared public faucet recipient changed or unresolved")
	}
	resolved := meta.FindStatusCondition(recipient.Status.Conditions, "Resolved")
	if resolved == nil || resolved.Status != metav1.ConditionTrue || resolved.ObservedGeneration != recipient.Generation {
		return selection, fmt.Errorf("public faucet recipient resolution is not current")
	}
	if version, _, err := stacksidentity.DecodeAddress(recipient.Status.Identity.Address); err != nil || version != 26 {
		return selection, fmt.Errorf("invalid public recipient address")
	}
	amount := uint64(1)
	policy := p.Status.Admission.Configuration.StacksFaucet
	if policy.AccountRef == nil || policy.TargetNodeRef == nil {
		return selection, fmt.Errorf("faucet source/target policy unresolved")
	}
	var source, target stacks.FaucetBinding
	var targetUID types.UID
	for _, id := range root.Status.Identities {
		if id.Name == policy.TargetNodeRef.Name && !id.Removing {
			targetUID = id.UID
		}
	}
	for _, binding := range p.Status.Admission.Dependencies {
		if binding.Kind == "StacksAccount" && binding.Name == policy.AccountRef.Name {
			source = stacks.FaucetBinding{Kind: binding.Kind, Name: binding.Name, UID: binding.UID, Fingerprint: binding.Fingerprint}
		}
		if binding.Kind == "StacksNetworkParticipant" && binding.UID == targetUID {
			target = stacks.FaucetBinding{Kind: binding.Kind, Name: binding.Name, UID: binding.UID, Fingerprint: binding.Fingerprint}
		}
	}
	if source.UID == "" || target.UID == "" {
		return selection, fmt.Errorf("faucet source/target identities unavailable")
	}
	if policy.MaxRequestMicroSTX != nil {
		limit, err := strconv.ParseUint(string(*policy.MaxRequestMicroSTX), 10, 64)
		if err != nil || limit < amount {
			return selection, fmt.Errorf("faucet request maximum is below qualification amount")
		}
	}
	selection = faucetSelection{source: source, target: target, participant: objectIdentity(&p), worker: objectIdentity(&pod), logicalName: p.Spec.ParticipantName, processNonce: execution.ProcessNonce, profileDigest: worker.ProfileDigest, destination: stacks.FaucetBinding{Kind: "StacksAccount", Name: recipient.Name, UID: recipient.UID, Fingerprint: recipient.Status.Digest}, address: recipient.Status.Identity.Address, amount: common.Amount(strconv.FormatUint(amount, 10))}
	if execution.Transactions != nil {
		selection.offered = execution.Transactions.Offered
		selection.included = execution.Transactions.Included
	}
	if execution.Faucet != nil {
		selection.completed = execution.Faucet.Completed
		if execution.Faucet.Active != 0 || execution.Faucet.Orphaned != 0 {
			return selection, fmt.Errorf("faucet already has active requests")
		}
	}
	if selection.offered > math.MaxUint64-2 || selection.included > math.MaxUint64-2 || selection.completed > math.MaxUint64-2 {
		return selection, fmt.Errorf("faucet counters cannot represent two requests")
	}
	return selection, nil
}

// faucetRequest builds only public API fields; generated identity is assigned by the server.
func faucetRequest(namespace string, networkUID types.UID, selection faucetSelection, timeout time.Duration) *stacks.StacksFaucetRequest {
	return &stacks.StacksFaucetRequest{TypeMeta: metav1.TypeMeta{APIVersion: stacks.GroupVersion.String(), Kind: "StacksFaucetRequest"}, ObjectMeta: metav1.ObjectMeta{Namespace: namespace, GenerateName: "qualification-faucet-"}, Spec: stacks.StacksFaucetRequestSpec{NetworkUID: networkUID, FaucetRef: common.NameRef{Name: selection.logicalName}, Destination: stacks.Recipient{AccountRef: &common.NameRef{Name: selection.destination.Name}}, AmountMicroSTX: selection.amount, Timeout: common.Duration(timeout.String())}}
}

// includedRequest checks native worker evidence rather than trusting the controller's phase projection.
func includedRequest(request *stacks.StacksFaucetRequest, expectedUID types.UID, networkUID types.UID, s faucetSelection) (bool, error) {
	if request.UID != expectedUID || request.DeletionTimestamp != nil || request.Spec.NetworkUID != networkUID || request.Spec.FaucetRef.Name != s.logicalName || request.Spec.AmountMicroSTX != s.amount || request.Spec.Destination.AccountRef == nil || request.Spec.Destination.AccountRef.Name != s.destination.Name {
		return false, fmt.Errorf("qualification request identity changed")
	}
	a := request.Status.Admission
	if a != nil {
		if a.Decision == "Rejected" || a.Decision == "Expired" {
			return false, fmt.Errorf("faucet admission %s: %s", a.Decision, a.Reason)
		}
		if a.Decision == "Admitted" && (a.NetworkUID != networkUID || a.Faucet == nil || a.Faucet.UID != s.participant.UID || a.Faucet.Name != s.participant.Name || a.Worker == nil || a.Worker.UID != s.worker.UID || a.Worker.Name != s.worker.Name || a.ProfileDigest != s.profileDigest || a.SourceAccount == nil || *a.SourceAccount != s.source || a.Target == nil || *a.Target != s.target || a.DestinationAccount == nil || *a.DestinationAccount != s.destination || a.Destination != s.address || a.AmountMicroSTX != s.amount) {
			return false, fmt.Errorf("faucet admission differs from selected public identities")
		}
	}
	e := request.Status.Execution
	if e == nil {
		return false, nil
	}
	if a == nil || a.Decision != "Admitted" || e.NetworkUID != networkUID || e.FaucetUID != s.participant.UID || e.WorkerUID != s.worker.UID || e.ProcessNonce != s.processNonce || e.Destination != s.address || e.AmountMicroSTX != s.amount {
		return false, fmt.Errorf("faucet execution identity changed")
	}
	if e.Phase == "Rejected" || e.Phase == "Expired" || e.Phase == "Inconclusive" {
		return false, fmt.Errorf("faucet outcome %s: %s", e.Phase, e.Reason)
	}
	if e.Phase != "Completed" {
		return false, nil
	}
	validHash := func(value string) bool {
		decoded, err := hex.DecodeString(value)
		return err == nil && len(decoded) == 32 && len(value) == 64 && value == strings.ToLower(value)
	}
	if e.Reason != "Included" || e.NoSend || !validHash(e.TxID) || !validHash(e.InclusionBlockID) || e.ObservedAt.IsZero() || e.ObservedAt.Before(&request.CreationTimestamp) || e.ObservedAt.After(time.Now()) {
		return false, fmt.Errorf("faucet completion lacks exact native inclusion")
	}
	return true, nil
}

// recordFaucet retains bounded public request/status evidence before their normal deletion.
func (h *harness) recordFaucet(stage string, requests []*stacks.StacksFaucetRequest) error {
	data, err := json.MarshalIndent(requests, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(h.evidence, stage+"-requests.json"), data, 0600)
}

// faucetCounters observes the same process and exactly two successful original sends.
func faucetCounters(s snapshot, selection faucetSelection) (*api.WorkerExecutionStatus, bool, error) {
	for _, p := range s.Participants {
		if p.Identity.UID == selection.participant.UID {
			e := p.Status.Execution
			if e == nil || e.PodUID != selection.worker.UID || e.ProcessNonce != selection.processNonce || e.ProfileDigest != selection.profileDigest {
				return nil, false, fmt.Errorf("faucet process identity changed")
			}
			if e.Transactions == nil || e.Faucet == nil {
				return e, false, nil
			}
			if e.Transactions.Offered > selection.offered+2 || e.Transactions.Included > selection.included+2 || e.Faucet.Completed > selection.completed+2 {
				return e, false, fmt.Errorf("faucet observed more than two qualification sends")
			}
			return e, e.Transactions.Offered == selection.offered+2 && e.Transactions.Included == selection.included+2 && e.Faucet.Completed == selection.completed+2 && e.Pending == 0 && e.Faucet.Active == 0 && e.Faucet.Orphaned == 0, nil
		}
	}
	return nil, false, fmt.Errorf("selected faucet participant disappeared")
}

// qualifyFaucet checks independent native inclusions and bounded no-resubmission observations.
func (h *harness) qualifyFaucet(ctx context.Context, before snapshot) (snapshot, error) {
	selection, err := h.faucetInputs(ctx, before)
	if err != nil {
		return before, err
	}
	timeout := h.config.progressTimeout
	if timeout > time.Hour {
		timeout = time.Hour
	}
	requests := []*stacks.StacksFaucetRequest{}
	for range 2 {
		request := faucetRequest(h.config.namespace, h.rootUID, selection, timeout)
		if err := h.c.Create(ctx, request, client.DryRunAll); err != nil {
			return before, err
		}
		// A lost create acknowledgement ends the test; it must never create a replacement request.
		request = faucetRequest(h.config.namespace, h.rootUID, selection, timeout)
		if err := h.c.Create(ctx, request); err != nil {
			return before, err
		}
		requests = append(requests, request)
	}
	if requests[0].UID == "" || requests[0].UID == requests[1].UID {
		return before, fmt.Errorf("API did not assign independent request identities")
	}
	if err := h.recordFaucet("faucet-created", requests); err != nil {
		return before, err
	}
	completed, err := h.wait(ctx, "faucet-included", timeout, true, func(s snapshot) (bool, error) {
		all := true
		for i, old := range requests {
			var current stacks.StacksFaucetRequest
			if err := h.c.Get(ctx, client.ObjectKeyFromObject(old), &current); err != nil {
				if apierrors.IsNotFound(err) {
					return false, fmt.Errorf("qualification request disappeared before outcome retention")
				}
				return false, nil
			}
			ready, err := includedRequest(&current, old.UID, h.rootUID, selection)
			if err != nil {
				return false, err
			}
			all = all && ready
			requests[i] = current.DeepCopy()
		}
		if err := h.recordFaucet("faucet-latest", requests); err != nil {
			return false, err
		}
		if all && requests[0].Status.Execution.TxID == requests[1].Status.Execution.TxID {
			return false, fmt.Errorf("independent faucet requests reused a transaction")
		}
		_, counted, err := faucetCounters(s, selection)
		return all && counted, err
	})
	if err != nil {
		return completed, err
	}
	if err := h.recordFaucet("faucet-included", requests); err != nil {
		return completed, err
	}
	// Metadata updates deliver duplicate observations while immutable terminal outcomes remain.
	for _, request := range requests {
		original := request.DeepCopy()
		base := request.DeepCopy()
		if request.Annotations == nil {
			request.Annotations = map[string]string{}
		}
		request.Annotations["network.stacks.org/qualification-reobserve"] = "true"
		if err := h.c.Patch(ctx, request, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return completed, err
		}
		var current stacks.StacksFaucetRequest
		if err := h.c.Get(ctx, client.ObjectKeyFromObject(request), &current); err != nil {
			return completed, err
		}
		if current.UID != original.UID || !reflect.DeepEqual(current.Status.Admission, original.Status.Admission) || !reflect.DeepEqual(current.Status.Execution, original.Status.Execution) {
			return completed, fmt.Errorf("reobservation changed retained faucet outcome")
		}
	}
	// Capture each outcome above, then delete normally. Completed process counters must remain.
	for _, request := range requests {
		if err := h.c.Delete(ctx, request, client.Preconditions{UID: &request.UID}); err != nil {
			return completed, err
		}
	}
	observeAfter := time.Now().Add(h.config.pauseWindow)
	return h.wait(ctx, "faucet-deleted-no-resubmit", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		for _, request := range requests {
			var current stacks.StacksFaucetRequest
			err := h.c.Get(ctx, client.ObjectKeyFromObject(request), &current)
			if err == nil {
				return false, fmt.Errorf("deleted faucet request reappeared")
			}
			if !apierrors.IsNotFound(err) {
				return false, nil
			}
		}
		e, ready, err := faucetCounters(s, selection)
		if err != nil || !ready {
			return false, err
		}
		return !s.At.Before(observeAfter) && !e.ObservedAt.Before(&metav1.Time{Time: observeAfter}) && !e.ObservedAt.After(s.At), nil
	})
}
