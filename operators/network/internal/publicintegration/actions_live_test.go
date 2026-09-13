//go:build live

package publicintegration

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	actions "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// actionSelection retains public identities before request creation, independently of action admission.
type actionSelection struct {
	Participant participantEvidence           `json:"participant"`
	Execution   identity                      `json:"execution"`
	Target      bitcoin.BitcoinTargetIdentity `json:"target"`
	Height      int64                         `json:"height"`
	Address     string                        `json:"address"`
}

// selectActionTarget chooses one currently bound node and an observed public payout address.
func selectActionTarget(s snapshot) (actionSelection, error) {
	return selectActionTargetUID(s, "")
}

// selectActionTargetUID keeps a chosen producer fixed while waiting for its reservation to clear.
func selectActionTargetUID(s snapshot, uid types.UID) (actionSelection, error) {
	if s.Status.Bitcoin == nil || s.Status.Initialization == nil || !s.Status.Initialization.Completed || s.Operation != "Running" || failed(s) || s.Deleting {
		return actionSelection{}, fmt.Errorf("actions require completed initialization and a running exact root")
	}
	for _, p := range s.Participants {
		if p.Kind != "BitcoinNode" || uid != "" && p.Identity.UID != uid {
			continue
		}
		for _, e := range s.Executions {
			if e.ParticipantUID != p.Identity.UID {
				continue
			}
			selected := actionSelection{Participant: p, Execution: e.Identity}
			view, err := selected.observe(s)
			if err != nil {
				continue
			}
			if e.Status.Action != nil || e.Status.Armed != nil {
				continue
			}
			for _, wallet := range view.Wallets {
				if wallet.Ready && wallet.Address != "" {
					selected.Target, selected.Height, selected.Address = view.Target, view.Height, wallet.Address
					return selected, nil
				}
			}
		}
	}
	return actionSelection{}, fmt.Errorf("no fresh exact Bitcoin actor/execution with a ready public payout wallet")
}

// observe verifies the selected record and native read against current admission and actor identity.
func (selected actionSelection) observe(s snapshot) (*bitcoin.BitcoinObservation, error) {
	if s.Status.Bitcoin == nil || s.Status.ObservationPolicy == nil {
		return nil, fmt.Errorf("Bitcoin observation policy unavailable")
	}
	bound := false
	for _, ref := range s.Status.Bitcoin.ExecutionRefs {
		if ref.UID == selected.Execution.UID && ref.Name == selected.Execution.Name {
			bound = true
		}
	}
	if !bound {
		return nil, fmt.Errorf("selected execution binding changed")
	}
	for _, p := range s.Participants {
		if p.Identity.UID != selected.Participant.Identity.UID || p.Name != selected.Participant.Name {
			continue
		}
		r := p.Status.Runtime
		if p.Status.Admission == nil || r == nil || r.PodRef == nil || r.ConfigRef == nil || r.RPCSecretRef == nil || r.Terminated || r.ContainerID == "" || r.PolicyDigest != p.Status.Admission.PolicyDigest {
			return nil, fmt.Errorf("selected actor admission/runtime unavailable")
		}
		for _, e := range s.Executions {
			if e.Identity.UID != selected.Execution.UID || e.Identity.Name != selected.Execution.Name || e.ParticipantUID != p.Identity.UID {
				continue
			}
			v := e.Status.Observation
			freshness := time.Duration(3*s.Status.ObservationPolicy.PollIntervalSeconds+s.Status.ObservationPolicy.RPCAllowanceSeconds) * time.Second
			if v == nil || v.ObservedAt.IsZero() || v.ObservedAt.After(s.At) || s.At.Sub(v.ObservedAt.Time) > freshness || v.Tip == "" {
				return nil, fmt.Errorf("selected native observation stale or missing")
			}
			if v.Target.Participant.UID != p.Identity.UID || v.Target.Participant.Name != p.Identity.Name || v.Target.Pod != *r.PodRef || v.Target.ContainerID != r.ContainerID || v.Target.Configuration != *r.ConfigRef || v.Target.Credentials != *r.RPCSecretRef || v.Target.PolicyDigest != r.PolicyDigest {
				return nil, fmt.Errorf("selected native observation identity differs")
			}
			if selected.Target.Participant.UID != "" && !reflect.DeepEqual(v.Target, selected.Target) {
				return nil, fmt.Errorf("selected actor identity changed during action")
			}
			return v, nil
		}
	}
	return nil, fmt.Errorf("selected actor or execution disappeared")
}

// qualifyActions uses only public declarations and retained native evidence; installation is external.
func (h *harness) qualifyActions(ctx context.Context, before snapshot) (snapshot, error) {
	original, schedule, producer, err := h.scheduleFixture(ctx, before)
	if err != nil {
		return before, err
	}
	selectedTarget, err := selectActionTarget(before)
	if err != nil {
		return before, err
	}
	// Reorganization has target-local exclusion; independently producing peers would invalidate its captured tip.
	targets := []bitcoin.ProductionTarget{{NodeRef: common.NameRef{Name: selectedTarget.Participant.Name}, Weight: 1}}
	trial := scheduleTrial{producer: producer, baseline: schedule, effective: schedule.Spec, targets: targets, after: time.Now()}
	if err := h.changeSchedule(ctx, original, schedule, targets); err != nil {
		return before, err
	}
	before, err = h.awaitSchedule(ctx, "actions-single-producer", trial)
	if err != nil {
		return before, err
	}
	before, err = h.wait(ctx, "actions-other-producers-drained", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		if _, ok := scheduleMatches(s, trial); !ok {
			return false, nil
		}
		for _, observed := range s.Executions {
			if observed.ParticipantUID == selectedTarget.Participant.Identity.UID {
				continue
			}
			var record bitcoin.BitcoinExecution
			if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: observed.Identity.Name}, &record); err != nil {
				return false, err
			}
			if record.UID != observed.Identity.UID {
				return false, fmt.Errorf("execution replaced while draining")
			}
			if baselineMutationPending(&record, s.At) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return before, err
	}
	cadences := []actions.GenerationCadence{{Mode: "Fixed", IntervalSeconds: 2}}
	if os.Getenv("STACKS_PUBLIC_ALL_CADENCES") == "1" {
		cadences = append(cadences, actions.GenerationCadence{Mode: "Immediate"}, actions.GenerationCadence{Mode: "Uniform", MinSeconds: 2, MaxSeconds: 4}, actions.GenerationCadence{Mode: "Explicit", DelaysSeconds: []int32{3}})
	}
	for i := 0; i <= len(cadences); i++ {
		reorganization := i == len(cadences)
		var selected actionSelection
		current, err := h.wait(ctx, "action-target-ready", 30*time.Second, true, func(s snapshot) (bool, error) {
			var selectionErr error
			selected, selectionErr = selectActionTargetUID(s, selectedTarget.Participant.Identity.UID)
			return selectionErr == nil, nil
		})
		if err != nil {
			return current, err
		}
		var request client.Object
		timeout := metav1.Duration{Duration: 3 * time.Minute}
		if reorganization {
			request = &actions.BitcoinReorganization{TypeMeta: metav1.TypeMeta{APIVersion: actions.GroupVersion.String(), Kind: "BitcoinReorganization"}, ObjectMeta: metav1.ObjectMeta{Name: "qualify-reorganization", Namespace: h.config.namespace}, Spec: actions.BitcoinReorganizationSpec{NetworkUID: h.rootUID, BitcoinNodeRef: actions.LocalReference{Name: selected.Participant.Name}, Depth: 1, Address: selected.Address, Timeout: timeout, BoundaryPolicy: actions.ReorganizationBoundaryPolicy{AllowEpochBoundaryCrossing: true, AllowRewardCycleBoundaryCrossing: true, AllowPreparePhaseBoundaryCrossing: true}}}
		} else {
			request = &actions.BitcoinBlockGeneration{TypeMeta: metav1.TypeMeta{APIVersion: actions.GroupVersion.String(), Kind: "BitcoinBlockGeneration"}, ObjectMeta: metav1.ObjectMeta{Name: "qualify-generation-" + strings.ToLower(string(cadences[i].Mode)), Namespace: h.config.namespace}, Spec: actions.BitcoinBlockGenerationSpec{NetworkUID: h.rootUID, BitcoinNodeRef: actions.LocalReference{Name: selected.Participant.Name}, Count: 2, Address: selected.Address, Timeout: timeout, Cadence: cadences[i]}}
		}
		if err := h.c.Create(ctx, request, client.DryRunAll); err != nil {
			return current, fmt.Errorf("optional actions unavailable: install the v1alpha2 action chart and enable both Bitcoin action kinds in the network operator before qualification: %w", err)
		}
		if err := h.c.Create(ctx, request); err != nil {
			return current, err
		}
		if err := h.event(request.GetName()+"-created", map[string]any{"request": request, "selected": selected}); err != nil {
			return current, err
		}
		before, err = h.awaitAction(ctx, request, selected)
		if err != nil {
			return before, err
		}
	}
	if err := h.changeSchedule(ctx, original, nil, nil); err != nil {
		return before, err
	}
	if err := h.event("actions-baseline-restored", original); err != nil {
		return before, err
	}
	// Reorganization can temporarily disturb Stacks progress; require fresh native recovery before pause.
	coherent, err := h.wait(ctx, "actions-native-coherent", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		_, ready := progress(s)
		return ready && condition(s, "Operational", metav1.ConditionTrue), nil
	})
	if err != nil {
		return coherent, err
	}
	return h.awaitProgress(ctx, "actions-native-recovered", coherent)
}

// actionStatus presents the common lifecycle without discarding reorganization evidence.
func actionStatus(request client.Object) *actions.BitcoinBlockGenerationStatus {
	switch a := request.(type) {
	case *actions.BitcoinBlockGeneration:
		return &a.Status
	case *actions.BitcoinReorganization:
		return &a.Status.BitcoinBlockGenerationStatus
	default:
		panic("unsupported qualification action")
	}
}

// validateActionResult requires exact admission, finite receipts and independently acknowledged cleanup.
func validateActionResult(request client.Object, selected actionSelection, rootUID types.UID) error {
	s := actionStatus(request)
	if s.Phase != "Completed" {
		return fmt.Errorf("action %s terminated %s: %v", request.GetName(), s.Phase, s.Conditions)
	}
	if s.ObservedGeneration != request.GetGeneration() || s.AdmittedNetwork == nil || s.AdmittedNetwork.UID != string(rootUID) || s.AdmittedTarget == nil || s.AdmittedExecution == nil || s.AdmittedExecution.UID != selected.Execution.UID || s.AdmittedExecution.Name != selected.Execution.Name {
		return fmt.Errorf("action admitted network/execution differs")
	}
	target := s.AdmittedTarget
	if target.UID != string(selected.Participant.Identity.UID) || target.Name != selected.Participant.Identity.Name || target.PodUID != string(selected.Target.Pod.UID) || target.ContainerID != selected.Target.ContainerID || target.Configuration != selected.Target.Configuration || target.Credentials != selected.Target.Credentials || target.SpecDigest != selected.Target.PolicyDigest || target.ConfigDigest != selected.Participant.Status.Runtime.ConfigurationDigest {
		return fmt.Errorf("action admitted actor differs from pre-create identity")
	}
	if s.StartedAt == nil || s.FinishedAt == nil || s.LastDispatchID == "" || s.LastBlockHash == "" || s.BlocksGenerated != 2 {
		return fmt.Errorf("action lacks exact finite receipt accounting")
	}
	for _, name := range []string{"Admitted", "EffectObserved", "CleanupComplete"} {
		c := meta.FindStatusCondition(s.Conditions, name)
		if c == nil || c.Status != metav1.ConditionTrue || c.ObservedGeneration != request.GetGeneration() {
			return fmt.Errorf("action lacks current %s evidence", name)
		}
	}
	if r, ok := request.(*actions.BitcoinReorganization); ok {
		e := r.Status
		if !e.InvalidationAcknowledged || !e.CleanupAcknowledged || e.OriginalChain == nil || e.ForkParent == nil || e.FinalChain == nil || e.InvalidatedHash != e.OriginalChain.Hash || e.OriginalChain.Height != e.ForkParent.Height+1 || e.OriginalChain.PreviousBlockHash != e.ForkParent.Hash || len(e.ReplacementBlockHashes) != 2 || e.ReplacementBlockHashes[0] == e.InvalidatedHash || e.ReplacementBlockHashes[0] == e.ReplacementBlockHashes[1] || e.FinalChain.Hash != e.ReplacementBlockHashes[1] || e.FinalChain.PreviousBlockHash != e.ReplacementBlockHashes[0] || e.FinalChain.Height != e.OriginalChain.Height+1 {
			return fmt.Errorf("depth-one reorganization lacks contiguous replacement and cleanup evidence")
		}
		original, valid := new(big.Int).SetString(e.OriginalChain.Chainwork, 16)
		final, finalValid := new(big.Int).SetString(e.FinalChain.Chainwork, 16)
		if !valid || !finalValid || original.Sign() <= 0 || final.Cmp(original) <= 0 {
			return fmt.Errorf("replacement chain does not demonstrate higher work")
		}
	}
	return nil
}

// awaitAction retains public request/status/native views, then observes release without action replay.
func (h *harness) awaitAction(ctx context.Context, request client.Object, selected actionSelection) (snapshot, error) {
	uid := request.GetUID()
	started := time.Now()
	var terminal *actions.BitcoinBlockGenerationStatus
	var releasedAt time.Time
	return h.wait(ctx, request.GetName()+"-released", 3*time.Minute+45*time.Second, true, func(s snapshot) (bool, error) {
		if err := h.c.Get(ctx, client.ObjectKeyFromObject(request), request); err != nil {
			return false, err
		}
		if request.GetUID() != uid {
			return false, fmt.Errorf("qualification action replaced")
		}
		status := actionStatus(request)
		var record *executionEvidence
		for i := range s.Executions {
			if s.Executions[i].Identity.UID == selected.Execution.UID {
				record = &s.Executions[i]
			}
		}
		if err := h.writeActionEvidence(request, s, record); err != nil {
			return false, err
		}
		if status.ObservedGeneration == 0 && time.Since(started) > 30*time.Second {
			return false, fmt.Errorf("optional action controller has not observed %s; install the enabled v1alpha2 action chart in this namespace", request.GetName())
		}
		view, err := selected.observe(s)
		if err != nil {
			return false, err
		}
		if !actions.IsTerminalPhase(status.Phase) {
			return false, nil
		}
		if err := validateActionResult(request, selected, h.rootUID); err != nil {
			return false, err
		}
		if terminal == nil {
			terminal = status.DeepCopy()
			if err := h.event(request.GetName()+"-completed", request); err != nil {
				return false, err
			}
		} else if !reflect.DeepEqual(terminal, status) {
			return false, fmt.Errorf("terminal action accounting changed after completion")
		}
		if record == nil {
			return false, fmt.Errorf("selected execution disappeared")
		}
		held := record.Status.Action != nil && record.Status.Action.Request.UID == uid || record.Status.Reservation != nil && record.Status.Reservation.UID == uid || record.Status.Armed != nil && record.Status.Armed.Action != nil && record.Status.Armed.Action.UID == uid
		if held {
			if !releasedAt.IsZero() {
				return false, fmt.Errorf("completed action reacquired execution authority")
			}
			return false, nil
		}
		minimum := selected.Height + 2
		if reorg, ok := request.(*actions.BitcoinReorganization); ok {
			minimum = reorg.Status.FinalChain.Height
			if view.Tip == reorg.Status.InvalidatedHash {
				return false, fmt.Errorf("native view returned to invalidated tip")
			}
		}
		if view.Height < minimum || view.ObservedAt.Before(terminal.FinishedAt) {
			return false, nil
		}
		if releasedAt.IsZero() {
			releasedAt = time.Now()
			if err := h.event(request.GetName()+"-reservation-released", map[string]any{"execution": record, "native": view}); err != nil {
				return false, err
			}
		}
		// Baseline receipts may advance; only reacquisition or changed action receipts constitute replay evidence.
		window := time.Duration(3*s.Status.ObservationPolicy.PollIntervalSeconds+s.Status.ObservationPolicy.RPCAllowanceSeconds) * time.Second
		return time.Since(releasedAt) >= window, nil
	})
}

// writeActionEvidence preserves bounded latest public facts without reading credentials or logs.
func (h *harness) writeActionEvidence(request client.Object, s snapshot, record *executionEvidence) error {
	data, err := json.MarshalIndent(map[string]any{"request": request, "network": s.Root, "observedAt": s.At, "execution": record}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(h.evidence, request.GetName()+"-latest.json"), data, 0600)
}

// baselineMutationPending distinguishes an in-flight mutation from an expired unsent opportunity.
func baselineMutationPending(record *bitcoin.BitcoinExecution, now time.Time) bool {
	return record.Status.Armed != nil || record.Status.Action != nil || record.Spec.Offer != nil && record.Status.CompletedOffer < record.Spec.Offer.Number && now.Before(record.Spec.Offer.ExpiresAt.Time)
}
