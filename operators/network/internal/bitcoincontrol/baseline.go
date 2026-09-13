package bitcoincontrol

import (
	"context"
	"fmt"
	"math"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SchedulingFieldManager owns only the producer's bounded scheduling projection.
const SchedulingFieldManager = "stacks-network-domain-bitcoinblockproduction-scheduling"

// reconcileBaseline owns one durable weighted decision at a time, independently of other nodes' RPCs.
func (s *Scheduler) reconcileBaseline(ctx context.Context, root *api.StacksNetwork, record *bitcoin.BitcoinInitialization) (ctrl.Result, error) {
	before := record.DeepCopy()
	if record.Status.Baseline == nil {
		sequence := record.Status.LastAccountedOffer
		if record.Status.Offer != nil && record.Status.Offer.Number > sequence {
			sequence = record.Status.Offer.Number
		}
		record.Status.Baseline = &bitcoin.BitcoinBaselineStatus{Sequence: sequence, Scheduling: bitcoin.BitcoinSchedulingStatus{Initialization: objectref.BitcoinInitialization(record)}}
		record.Status.NextOpportunityAt = nil
		record.Status.Offer = nil
	}
	baseline := record.Status.Baseline
	var production *api.StacksNetworkParticipant
	report := func(phase bitcoin.InitializationPhase, reason string) (ctrl.Result, error) {
		record.Status.Phase, record.Status.Reason = phase, reason
		baseline.Scheduling.NextOpportunityAt = record.Status.NextOpportunityAt.DeepCopy()
		baseline.Scheduling.Reason = reason
		if !equality.Semantic.DeepEqual(before.Status, record.Status) {
			if err := s.Client.Status().Update(ctx, record); err != nil {
				return ctrl.Result{}, err
			}
		}
		if production != nil && !equality.Semantic.DeepEqual(production.Status.Scheduling, &baseline.Scheduling) {
			if err := participantstatus.Apply(ctx, s.Client, production, api.ParticipantStatus{Scheduling: baseline.Scheduling.DeepCopy()}, SchedulingFieldManager); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: schedulerDelay(record, s.Now())}, nil
	}
	// Accounting survives producer deletion and never touches frozen funding counters.
	executions := s.accountBaselineReceipts(ctx, root, record)
	if failed(root) || root.DeletionTimestamp != nil || root.Spec.Operation == api.NetworkOperationStopped {
		s.withdrawBaseline(record)
		baseline.Scheduling.EligibleTargets = 0
		if failed(root) {
			return report(bitcoin.InitializationBlocked, api.ReasonNetworkFailed)
		}
		return report(bitcoin.InitializationAbandoned, api.ReasonNetworkStopped)
	}
	if err := baselineCompleted(ctx, s.Reader, root, record); err != nil {
		s.withdrawBaseline(record)
		baseline.Scheduling.EligibleTargets = 0
		baseline.Scheduling.ObservedAt = ptr.To(metav1.NewTime(s.Now().UTC().Truncate(time.Second)))
		return report(bitcoin.InitializationBlocked, reasonInitializationCompletionUnavailable)
	}
	var err error
	production, err = s.currentProduction(ctx, root, record)
	if err != nil {
		s.withdrawBaseline(record)
		baseline.Scheduling.EligibleTargets = 0
		baseline.Scheduling.ObservedAt = ptr.To(metav1.NewTime(s.Now().UTC().Truncate(time.Second)))
		return report(bitcoin.InitializationBlocked, reasonProductionIdentityUnavailable)
	}
	inputs, err := baselineInputs(ctx, s.Reader, root, record, production)
	if err != nil {
		s.withdrawBaseline(record)
		baseline.Scheduling.EligibleTargets = 0
		baseline.Scheduling.ObservedAt = ptr.To(metav1.NewTime(s.Now().UTC().Truncate(time.Second)))
		return report(bitcoin.InitializationBlocked, reasonBaselineInputsUnavailable)
	}
	inputs.Override = record.Status.Override.DeepCopy()
	inputs.Schedule = effectiveSchedule(record, inputs.Schedule)
	_, upper, _ := cadenceBounds(inputs.Schedule)
	inputs.ProgressWindowSeconds = int64((foundation.ProgressWindow(upper) + time.Second - 1) / time.Second)
	if baseline.Scheduling.AdmissionDigest != inputs.AdmissionDigest || !equality.Semantic.DeepEqual(baseline.Scheduling.Override, inputs.Override) {
		s.withdrawBaseline(record)
		inputs.Opportunities, inputs.Assigned, inputs.Acknowledged, inputs.Skipped, inputs.Unassigned = baseline.Scheduling.Opportunities, baseline.Scheduling.Assigned, baseline.Scheduling.Acknowledged, baseline.Scheduling.Skipped, baseline.Scheduling.Unassigned
		inputs.LastAcknowledgedAt = baseline.Scheduling.LastAcknowledgedAt.DeepCopy()
		baseline.Scheduling = inputs
		baseline.SelectedTarget = nil
		baseline.Stage = ""
		current := objectref.Participant(production)
		record.Status.Production = &current
		return report(bitcoin.InitializationPreparing, reasonBaselinePolicyAdopted)
	}
	baseline.Scheduling.EligibleTargets = 0
	for i := range inputs.Targets {
		if _, reason := s.observeBaselineTarget(ctx, s.Client, true, root, record, &inputs.Targets[i].Participant, executions); reason == "" {
			baseline.Scheduling.EligibleTargets++
		}
	}
	baseline.Scheduling.ObservedAt = ptr.To(metav1.NewTime(s.Now().UTC().Truncate(time.Second)))
	if root.Spec.Operation == api.NetworkOperationPaused || production.Spec.Control != nil && ptr.Deref(production.Spec.Control.Paused, false) {
		s.withdrawBaseline(record)
		return report(bitcoin.InitializationPaused, api.ReasonDesiredPause)
	}
	now := s.Now()
	if record.Status.NextOpportunityAt == nil {
		interval, err := s.interval(inputs.Schedule)
		if err != nil {
			return report(bitcoin.InitializationBlocked, reasonCadenceUnavailable)
		}
		next := metav1.NewTime(now.Add(interval).UTC())
		record.Status.NextOpportunityAt = &next
		return report(bitcoin.InitializationPreparing, reasonBaselineCadenceArmed)
	}
	if baseline.Stage == bitcoin.BaselineSelected || baseline.Stage == bitcoin.BaselineOffered {
		if !now.Before(record.Status.NextOpportunityAt.Time) {
			baseline.Scheduling.Unassigned++
			baseline.Stage = bitcoin.BaselineUnassigned
			record.Status.Offer = nil
			return report(bitcoin.InitializationPreparing, reasonOpportunityExpiredBeforeAssignment)
		}
		target, reason := s.baselineTarget(ctx, root, record, baseline.SelectedTarget, executions)
		if reason != "" {
			baseline.Scheduling.Skipped++
			baseline.Stage = bitcoin.BaselineSkipped
			record.Status.Offer = nil
			return report(bitcoin.InitializationPreparing, reason)
		}
		if baseline.Stage == bitcoin.BaselineSelected {
			observation := target.Status.Observation
			wallet := record.Spec.PayoutWallet
			record.Status.Offer = &bitcoin.BitcoinBlockOffer{Override: overrideBinding(record), Mode: bitcoin.OfferBaseline, Target: baseline.SelectedTarget.DeepCopy(), Initialization: objectref.BitcoinInitialization(record), Production: objectref.Participant(production), PolicyDigest: inputs.PolicyDigest, Number: baseline.Sequence, Wallet: wallet.Wallet, Address: wallet.Address, ExpectedHeight: observation.Height, ExpectedTip: observation.Tip, Ceiling: math.MaxInt64, ExpiresAt: *record.Status.NextOpportunityAt}
			baseline.Stage = bitcoin.BaselineOffered
			result, err := report(bitcoin.InitializationPreparing, reasonBaselineOfferCommitted)
			result.RequeueAfter = time.Millisecond
			return result, err
		}
		if record.Status.Offer == nil || record.Status.Offer.Number != baseline.Sequence {
			return report(bitcoin.InitializationBlocked, reasonCommittedOfferUnavailable)
		}
		if !equality.Semantic.DeepEqual(target.Spec.Offer, record.Status.Offer) {
			target.Spec.Offer = record.Status.Offer.DeepCopy()
			if err := s.Client.Update(ctx, target); err != nil {
				return ctrl.Result{}, err
			}
		}
		baseline.Stage = bitcoin.BaselineAssigned
		baseline.Scheduling.Assigned++
		return report(bitcoin.InitializationPreparing, reasonBaselineOfferAssigned)
	}
	if now.Before(record.Status.NextOpportunityAt.Time) {
		return report(bitcoin.InitializationPreparing, reasonBaselineCadenceWaiting)
	}
	if baseline.Sequence == math.MaxInt64 || baseline.Scheduling.Opportunities == math.MaxInt64 {
		return report(bitcoin.InitializationBlocked, reasonOpportunityCounterExhausted)
	}
	interval, err := s.interval(inputs.Schedule)
	if err != nil {
		return report(bitcoin.InitializationBlocked, reasonCadenceUnavailable)
	}
	target, err := s.selectBaselineTarget(inputs.Targets)
	if err != nil {
		return report(bitcoin.InitializationBlocked, reasonTargetSelectionUnavailable)
	}
	baseline.Sequence++
	baseline.Scheduling.Opportunities++
	baseline.SelectedTarget = &target
	baseline.Stage = bitcoin.BaselineSelected
	next := metav1.NewTime(now.Add(interval).UTC())
	record.Status.NextOpportunityAt = &next
	record.Status.Offer = nil
	result, err := report(bitcoin.InitializationPreparing, reasonBaselineTargetSelected)
	result.RequeueAfter = time.Millisecond
	return result, err
}

// withdrawBaseline removes new-send authority without discarding previously armed RPC evidence.
func (s *Scheduler) withdrawBaseline(record *bitcoin.BitcoinInitialization) {
	baseline := record.Status.Baseline
	if baseline.Stage == bitcoin.BaselineSelected || baseline.Stage == bitcoin.BaselineOffered {
		baseline.Scheduling.Unassigned++
		baseline.Stage = bitcoin.BaselineUnassigned
	}
	record.Status.NextOpportunityAt = nil
	record.Status.Offer = nil
}

// baselineTarget inspects only the committed target; failure consumes its opportunity without redraw.
func (s *Scheduler) baselineTarget(ctx context.Context, root *api.StacksNetwork, initial *bitcoin.BitcoinInitialization, pin *common.Binding, executions map[types.UID]*bitcoin.BitcoinExecution) (*bitcoin.BitcoinExecution, string) {
	if pin == nil || executions[pin.UID] == nil {
		return nil, reasonSelectedExecutionUnavailable
	}
	cached := executions[pin.UID]
	var current bitcoin.BitcoinExecution
	if err := s.Reader.Get(ctx, client.ObjectKeyFromObject(cached), &current); err != nil || current.UID != cached.UID || current.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(&current, root) {
		return nil, reasonSelectedExecutionUnavailable
	}
	return s.observeBaselineTarget(ctx, s.Reader, false, root, initial, pin, map[types.UID]*bitcoin.BitcoinExecution{pin.UID: &current})
}

// observeBaselineTarget separates cached availability summaries from fresh selected-target authority.
func (s *Scheduler) observeBaselineTarget(ctx context.Context, reader client.Reader, publicOnly bool, root *api.StacksNetwork, initial *bitcoin.BitcoinInitialization, pin *common.Binding, executions map[types.UID]*bitcoin.BitcoinExecution) (*bitcoin.BitcoinExecution, string) {
	if pin == nil {
		return nil, reasonSelectedTargetUnavailable
	}
	var p api.StacksNetworkParticipant
	if err := reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: pin.Name}, &p); err != nil || p.UID != pin.UID || p.Spec.Kind != api.ParticipantBitcoinNode || !participantCurrent(root, &p) || p.Spec.Control != nil && ptr.Deref(p.Spec.Control.Suspended, false) {
		return nil, reasonSelectedTargetUnavailable
	}
	if err := foundation.ValidateAdmissionEligibility(ctx, reader, &p); err != nil {
		return nil, reasonSelectedAdmissionUnavailable
	}
	validate := foundation.ValidateParticipantAdmission
	if publicOnly {
		validate = foundation.ValidatePublicParticipantAdmission
	}
	if err := validate(ctx, reader, root, &p); err != nil {
		return nil, reasonSelectedDependencyUnavailable
	}
	rt := p.Status.Runtime
	ready := meta.FindStatusCondition(p.Status.Conditions, api.ConditionWorkloadReady)
	if ready == nil || ready.Status != metav1.ConditionTrue || ready.ObservedGeneration != p.Generation || rt == nil || rt.ObservedGeneration != p.Generation || rt.PolicyDigest != p.Status.Admission.PolicyDigest || rt.PodRef == nil || rt.ConfigRef == nil || rt.RPCSecretRef == nil || rt.ContainerID == "" || rt.Terminated {
		return nil, reasonSelectedTargetNotReady
	}
	if config := p.Status.Admission.Configuration.BitcoinNode; config == nil || config.Config != nil && ptr.Deref(config.Config.Compatibility, common.CompatibilityManaged) == common.CompatibilityUnverified {
		return nil, reasonSelectedTargetUnverified
	}
	execution := executions[pin.UID]
	if execution == nil {
		return nil, reasonSelectedExecutionUnavailable
	}
	if execution.DeletionTimestamp != nil || execution.Spec.Participant.Name != pin.Name {
		return nil, reasonSelectedExecutionUnavailable
	}
	if execution.Status.Armed != nil || execution.Status.PendingWalletRemoval != nil || execution.Status.Reservation != nil && *execution.Status.Reservation != objectref.BitcoinInitialization(initial) || !generationAccounted(initial, execution) {
		return nil, reasonSelectedTargetBusy
	}
	observation := execution.Status.Observation
	if observation == nil || s.Now().Sub(observation.ObservedAt.Time) > s.Freshness || observation.ObservedAt.After(s.Now()) || observation.Target.Participant.UID != p.UID || observation.Target.Pod.UID != rt.PodRef.UID || observation.Target.ContainerID != rt.ContainerID || observation.Target.PolicyDigest != rt.PolicyDigest || observation.Target.Configuration != *rt.ConfigRef || observation.Target.Credentials != *rt.RPCSecretRef || observation.Height >= math.MaxInt64 {
		return nil, reasonSelectedObservationUnavailable
	}
	if len(observation.Wallets) != len(ptr.Deref(p.Status.Admission.Configuration.BitcoinNode.WalletRefs, nil)) {
		return nil, reasonSelectedWalletsPreparing
	}
	for _, wallet := range observation.Wallets {
		pinned := false
		for _, dependency := range p.Status.Admission.Dependencies {
			pinned = pinned || dependency == wallet.Wallet
		}
		if !wallet.Ready || !pinned {
			return nil, reasonSelectedWalletsPreparing
		}
	}
	return execution, ""
}

// accountBaselineReceipts counts each exact per-node baseline receipt once, including retired producers.
func (s *Scheduler) accountBaselineReceipts(ctx context.Context, root *api.StacksNetwork, initial *bitcoin.BitcoinInitialization) map[types.UID]*bitcoin.BitcoinExecution {
	executions := map[types.UID]*bitcoin.BitcoinExecution{}
	baseline := initial.Status.Baseline
	for _, ref := range root.Status.Bitcoin.ExecutionRefs {
		var execution bitcoin.BitcoinExecution
		if err := s.Client.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: ref.Name}, &execution); err != nil || execution.UID != ref.UID || execution.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(&execution, root) {
			continue
		}
		if accountBootstrapReceipt(root, initial, &execution) {
			baseline.Sequence = max(baseline.Sequence, initial.Status.LastAccountedOffer)
		}
		executions[execution.Spec.Participant.UID] = &execution
		if baseline.Stage == bitcoin.BaselineOffered && initial.Status.Offer != nil && initial.Status.Offer.Target != nil && *initial.Status.Offer.Target == execution.Spec.Participant && equality.Semantic.DeepEqual(execution.Spec.Offer, initial.Status.Offer) {
			baseline.Stage = bitcoin.BaselineAssigned
			baseline.Scheduling.Assigned++
		}
		receipt := execution.Status.LastReceipt
		if receipt == nil || receipt.Request.Method != bitcoin.RPCGenerate || receipt.Request.Offer == nil {
			continue
		}
		offer := receipt.Request.Offer
		if offer.Mode != bitcoin.OfferBaseline || offer.Initialization != objectref.BitcoinInitialization(initial) || offer.Target == nil || *offer.Target != execution.Spec.Participant || receipt.Request.Target.Participant != execution.Spec.Participant || offer.Number > baseline.Sequence || offer.Number != execution.Status.CompletedOffer || !hashValid(receipt.BlockHash) {
			continue
		}
		index := -1
		accounted := int64(0)
		for i, cursor := range baseline.Receipts {
			if cursor.ExecutionUID == execution.UID {
				index = i
				accounted = cursor.Offer
				break
			}
		}
		if offer.Number <= accounted {
			continue
		}
		if baseline.Scheduling.Acknowledged == math.MaxInt64 {
			continue
		}
		if index < 0 {
			if len(baseline.Receipts) >= 1000 {
				continue
			}
			baseline.Receipts = append(baseline.Receipts, bitcoin.BitcoinReceiptCursor{ExecutionUID: execution.UID, Offer: offer.Number})
		} else {
			baseline.Receipts[index].Offer = offer.Number
		}
		baseline.Scheduling.Acknowledged++
		if baseline.Scheduling.LastAcknowledgedAt == nil || receipt.ReceivedAt.After(baseline.Scheduling.LastAcknowledgedAt.Time) {
			baseline.Scheduling.LastAcknowledgedAt = receipt.ReceivedAt.DeepCopy()
		}
	}
	return executions
}

// generationAccounted preserves every receipt until its appropriate scheduler cursor advances.
func generationAccounted(initial *bitcoin.BitcoinInitialization, execution *bitcoin.BitcoinExecution) bool {
	receipt := execution.Status.LastReceipt
	if receipt == nil || receipt.Request.Method != bitcoin.RPCGenerate {
		return true
	}
	if receipt.Request.Action != nil {
		return execution.Status.Action == nil
	}
	if receipt.Request.Offer == nil {
		return false
	}
	if receipt.Request.Offer.Mode != bitcoin.OfferBaseline {
		return initial.Status.LastAccountedOffer >= execution.Status.CompletedOffer
	}
	if initial.Status.Baseline != nil {
		for _, cursor := range initial.Status.Baseline.Receipts {
			if cursor.ExecutionUID == execution.UID {
				return cursor.Offer >= execution.Status.CompletedOffer
			}
		}
	}
	return false
}

// baselineOfferMatches checks retained selection identity and effective admitted target membership.
func baselineOfferMatches(initial *bitcoin.BitcoinInitialization, offer *bitcoin.BitcoinBlockOffer, inputs bitcoin.BitcoinSchedulingStatus, participant *api.StacksNetworkParticipant) error {
	baseline := initial.Status.Baseline
	if baseline == nil || baseline.Scheduling.AdmissionDigest != inputs.AdmissionDigest || offer.Target == nil || offer.Target.UID != participant.UID || offer.Target.Name != participant.Name || baseline.SelectedTarget == nil || *baseline.SelectedTarget != *offer.Target || baseline.Sequence != offer.Number || baseline.Stage != bitcoin.BaselineOffered && baseline.Stage != bitcoin.BaselineAssigned || offer.Ceiling != math.MaxInt64 || offer.Wallet != initial.Spec.PayoutWallet.Wallet || offer.Address != initial.Spec.PayoutWallet.Address {
		return fmt.Errorf("baseline opportunity authority differs")
	}
	for _, target := range inputs.Targets {
		if target.Participant == *offer.Target {
			return nil
		}
	}
	return fmt.Errorf("baseline target no longer selected")
}
