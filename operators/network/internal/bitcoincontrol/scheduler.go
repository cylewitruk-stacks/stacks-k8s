package bitcoincontrol

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// bootstrapAdmissionWindow gives control workers time to admit a bootstrap offer
// independently of the next cadence tick; current authority is still checked before send.
const bootstrapAdmissionWindow = 30 * time.Second

// Scheduler owns bootstrap advancement and baseline cadence without RPC credentials.
type Scheduler struct {
	// Client writes initialization status and per-node desired block offers.
	Client client.Client
	// Reader provides uncached admitted identity and observations.
	Reader client.Reader
	// Now controls freshness and cadence.
	Now func() time.Time
	// Draw samples a duration offset in [0,n), once per new durable opportunity.
	Draw func(int64) int64
	// Freshness bounds successful native observations; zero defaults to ten seconds.
	Freshness time.Duration
}

// SetupWithManager installs the scheduler independently of node mutation workers.
func (s *Scheduler) SetupWithManager(m ctrl.Manager) error {
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Draw == nil {
		s.Draw = rand.Int64N
	}
	if s.Freshness == 0 {
		s.Freshness = 10 * time.Second
	}
	return ctrl.NewControllerManagedBy(m).
		Named("bitcoin-initialization-v1alpha2").
		For(&bitcoin.BitcoinInitialization{}, builder.WithPredicates(schedulerEvents())).
		Watches(
			&bitcoin.BitcoinBlockScheduleOverride{},
			handler.EnqueueRequestsFromMapFunc(s.enqueue),
			builder.WithPredicates(schedulerEvents()),
		).
		Watches(
			&bitcoin.BitcoinExecution{},
			handler.EnqueueRequestsFromMapFunc(s.enqueue),
			builder.WithPredicates(schedulerEvents()),
		).
		Watches(
			&api.StacksNetwork{},
			handler.EnqueueRequestsFromMapFunc(s.enqueue),
			builder.WithPredicates(schedulerEvents()),
		).
		Watches(
			&api.StacksNetworkParticipant{},
			handler.EnqueueRequestsFromMapFunc(s.enqueue),
			builder.WithPredicates(schedulerEvents()),
		).
		Complete(s)
}

// enqueue routes runtime/control changes to the one retained network scheduler.
func (s *Scheduler) enqueue(ctx context.Context, obj client.Object) []ctrl.Request {
	root := &api.StacksNetwork{}
	if e := s.Client.Get(
		ctx,
		client.ObjectKey{Namespace: obj.GetNamespace(), Name: "network"},
		root,
	); e != nil || root.Status.Bitcoin == nil ||
		root.Status.Bitcoin.InitializationRef == nil {
		return nil
	}
	return []ctrl.Request{
		{NamespacedName: client.ObjectKey{
			Namespace: root.Namespace,
			Name:      root.Status.Bitcoin.InitializationRef.Name,
		}},
	}
}

// Reconcile advances frozen gates before enabling independently selected baseline opportunities.
func (s *Scheduler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	record := &bitcoin.BitcoinInitialization{}
	if e := s.Reader.Get(ctx, request.NamespacedName, record); e != nil {
		return ctrl.Result{}, client.IgnoreNotFound(e)
	}
	root := &api.StacksNetwork{}
	if e := s.Reader.Get(ctx, client.ObjectKey{Namespace: record.Namespace, Name: "network"}, root); e != nil {
		return ctrl.Result{}, e
	}
	if root.UID != record.Spec.NetworkUID || root.Status.Bitcoin == nil ||
		root.Status.Bitcoin.InitializationRef == nil ||
		root.Status.Bitcoin.InitializationRef.UID != record.UID ||
		!metav1.IsControlledBy(record, root) {
		return ctrl.Result{}, fmt.Errorf("initialization binding unavailable")
	}
	if changed, err := s.reconcileOverride(ctx, root, record); err != nil {
		return ctrl.Result{}, err
	} else if changed {
		return ctrl.Result{RequeueAfter: schedulerDelay(record, s.Now())}, nil
	}
	before := record.DeepCopy()
	report := func(phase bitcoin.InitializationPhase, reason string) (ctrl.Result, error) {
		record.Status.Phase, record.Status.Reason = phase, reason
		if !equality.Semantic.DeepEqual(before.Status, record.Status) {
			if e := s.Client.Status().Update(ctx, record); e != nil {
				return ctrl.Result{}, e
			}
		}
		return ctrl.Result{RequeueAfter: schedulerDelay(record, s.Now())}, nil
	}
	if root.Status.Initialization != nil && root.Status.Initialization.Completed {
		return s.reconcileBaseline(ctx, root, record)
	}
	if failed(root) {
		return report(bitcoin.InitializationBlocked, api.ReasonNetworkFailed)
	}
	if root.DeletionTimestamp != nil || root.Spec.Operation == api.NetworkOperationStopped {
		record.Status.NextOpportunityAt = nil
		return report(bitcoin.InitializationAbandoned, api.ReasonNetworkStopped)
	}
	if record.Status.PreparedAt == nil && record.Status.FirstCeilingObservedAt != nil &&
		!s.Now().Before(record.Status.FirstCeilingObservedAt.Add(120*time.Second)) {
		record.Status.NextOpportunityAt = nil
		return report(bitcoin.InitializationBlocked, api.ReasonPrepareBitcoinObservationDeadline)
	}
	authority, e := currentGate(ctx, s.Reader, root, record)
	if e != nil {
		return report(bitcoin.InitializationHeld, reasonBootstrapGateAuthorityUnavailable)
	}
	production, e := s.currentProduction(ctx, root, record)
	if e != nil {
		return report(bitcoin.InitializationBlocked, reasonProductionIdentityUnavailable)
	}
	currentBinding := objectref.Participant(production)
	if record.Status.Production == nil || *record.Status.Production != currentBinding {
		record.Status.Production = &currentBinding
		record.Status.NextOpportunityAt = nil
	}
	if root.Spec.Operation == api.NetworkOperationPaused ||
		production.Spec.Control != nil && ptr.Deref(production.Spec.Control.Paused, false) {
		record.Status.NextOpportunityAt = nil
		return report(bitcoin.InitializationPaused, api.ReasonDesiredPause)
	}
	allRecords := make(map[string]*bitcoin.BitcoinExecution, len(root.Status.Bitcoin.ExecutionRefs))
	for _, ref := range root.Status.Bitcoin.ExecutionRefs {
		candidate := &bitcoin.BitcoinExecution{}
		if e := s.Reader.Get(ctx, client.ObjectKey{
			Namespace: root.Namespace,
			Name:      ref.Name,
		}, candidate); e != nil {
			return report(bitcoin.InitializationBlocked, reasonExecutionRecordUnavailable)
		}
		if candidate.UID != ref.UID {
			return report(bitcoin.InitializationBlocked, reasonExecutionRecordReplaced)
		}
		allRecords[string(candidate.Spec.Participant.UID)] = candidate
	}
	if target := allRecords[string(record.Spec.Target.UID)]; target != nil {
		accountBootstrapReceipt(root, record, target)
	}
	var target *bitcoin.BitcoinExecution
	var height int64 = -1
	tip := ""
	mature := map[string]int32{}
	// Observe the frozen target first so another node cannot postpone the ceiling deadline.
	nodes := make([]common.Binding, 0, len(record.Spec.Nodes))
	for _, node := range record.Spec.Nodes {
		if node.UID == record.Spec.Target.UID {
			nodes = append(nodes, node)
		}
	}
	for _, node := range record.Spec.Nodes {
		if node.UID != record.Spec.Target.UID {
			nodes = append(nodes, node)
		}
	}
	for _, node := range nodes {
		execution := allRecords[string(node.UID)]
		if execution == nil || execution.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(execution, root) {
			return report(bitcoin.InitializationBlocked, reasonExecutionBindingUnavailable)
		}
		if node.UID == record.Spec.Target.UID {
			target = execution
		}
		if execution.Status.Armed != nil {
			return report(bitcoin.InitializationWaiting, reasonRPCOutstanding)
		}
		observation := execution.Status.Observation
		if observation == nil || s.Now().Sub(observation.ObservedAt.Time) > s.Freshness ||
			observation.ObservedAt.After(s.Now()) {
			return report(bitcoin.InitializationWaiting, reasonNodeObservationUnavailable)
		}
		p := &api.StacksNetworkParticipant{}
		if e := s.Reader.Get(
			ctx,
			client.ObjectKey{Namespace: root.Namespace, Name: node.Name},
			p,
		); e != nil ||
			p.UID != node.UID ||
			!participantCurrent(
				root,
				p,
			) ||
			p.Status.Runtime == nil ||
			p.Status.Runtime.PodRef == nil ||
			p.Status.Runtime.PodRef.UID != observation.Target.Pod.UID ||
			p.Status.Runtime.ContainerID != observation.Target.ContainerID ||
			p.Status.Admission.PolicyDigest != observation.Target.PolicyDigest ||
			p.Status.Runtime.ConfigRef == nil ||
			*p.Status.Runtime.ConfigRef != observation.Target.Configuration ||
			p.Status.Runtime.RPCSecretRef == nil ||
			*p.Status.Runtime.RPCSecretRef != observation.Target.Credentials {
			return report(bitcoin.InitializationWaiting, reasonNodeObservationIdentityChanged)
		}
		if node.UID == record.Spec.Target.UID && observation.Height >= record.Spec.MinimumHeight &&
			record.Status.FirstCeilingObservedAt == nil {
			observed := observation.ObservedAt
			record.Status.FirstCeilingObservedAt = &observed
		}
		if height < 0 {
			height, tip = observation.Height, observation.Tip
		} else if height != observation.Height || tip != observation.Tip {
			return report(bitcoin.InitializationWaiting, reasonCoreViewsDiverged)
		}
		expectedWallets := ptr.Deref(p.Status.Admission.Configuration.BitcoinNode.WalletRefs, nil)
		if len(observation.Wallets) != len(expectedWallets) {
			return report(bitcoin.InitializationWaiting, reasonWalletInventoryIncomplete)
		}
		for _, ref := range expectedWallets {
			found := false
			for _, observed := range observation.Wallets {
				for _, bound := range p.Status.Admission.Dependencies {
					found = found ||
						bound.Kind == bitcoin.KindBitcoinWallet &&
							bound.Name == ref.Name &&
							bound.UID == observed.Wallet.UID &&
							bound.Fingerprint == observed.Wallet.Fingerprint
				}
			}
			if !found {
				return report(bitcoin.InitializationWaiting, reasonWalletIdentityUnavailable)
			}
		}
		for _, wallet := range observation.Wallets {
			if !wallet.Ready {
				return report(bitcoin.InitializationWaiting, reasonWalletsPreparing)
			}
			if wallet.MatureOutputs > mature[string(wallet.Wallet.UID)] {
				mature[string(wallet.Wallet.UID)] = wallet.MatureOutputs
			}
		}
	}
	if target == nil || height < 0 {
		return report(bitcoin.InitializationBlocked, reasonInitializationTargetUnavailable)
	}
	if height > authority.gate.BitcoinCeiling {
		return report(bitcoin.InitializationBlocked, api.ReasonFrozenCeilingExceeded)
	}
	if authority.index == 0 && height >= record.Spec.MinimumHeight {
		if height != record.Spec.MinimumHeight {
			return report(bitcoin.InitializationBlocked, api.ReasonFrozenCeilingExceeded)
		}
		if record.Status.FirstCeilingObservedAt == nil {
			now := metav1.NewTime(s.Now().UTC())
			record.Status.FirstCeilingObservedAt = &now
		}
		prepared := true
		for _, wallet := range record.Spec.MinerWallets {
			prepared = prepared && mature[string(wallet.Wallet.UID)] >= record.Spec.MatureOutputsPerMiner
		}
		if prepared && record.Status.PreparedAt == nil {
			now := metav1.NewTime(s.Now().UTC())
			record.Status.PreparedAt = &now
		}
		record.Status.NextOpportunityAt = nil
		if !prepared && s.Now().Sub(record.Status.FirstCeilingObservedAt.Time) >= 120*time.Second {
			return report(bitcoin.InitializationBlocked, api.ReasonPrepareBitcoinObservationDeadline)
		}
		if !prepared {
			return report(bitcoin.InitializationHeld, reasonMinerMaturityUnconfirmed)
		}
		return report(bitcoin.InitializationHeld, reasonNextGateNotImplemented)
	}
	if record.Status.PreparedAt != nil && height < record.Spec.MinimumHeight {
		return report(bitcoin.InitializationBlocked, api.ReasonPreparedChainRegressed)
	}
	if height >= authority.gate.BitcoinCeiling {
		record.Status.NextOpportunityAt = nil
		return report(bitcoin.InitializationHeld, reasonFrozenGateReached)
	}
	if err := foundation.ValidateAdmissionEligibility(ctx, s.Reader, production); err != nil {
		record.Status.NextOpportunityAt = nil
		record.Status.Offer = nil
		return report(bitcoin.InitializationBlocked, reasonProductionSourceUnavailable)
	}
	ready, e := advancementReady(ctx, s.Reader, root, record, authority, height, s.Now())
	if e != nil {
		return report(bitcoin.InitializationWaiting, reasonStacksReadinessUnavailable)
	}
	if !ready {
		return report(bitcoin.InitializationWaiting, reasonAwaitingEnrollmentDemand)
	}
	if offer := record.Status.Offer; offer != nil && offer.Number > record.Status.LastAccountedOffer &&
		s.Now().Before(offer.ExpiresAt.Time) && offer.ExpectedHeight == height && offer.ExpectedTip == tip &&
		offer.PolicyDigest == production.Status.Admission.PolicyDigest && offer.Production == currentBinding {
		if !equality.Semantic.DeepEqual(target.Spec.Offer, offer) {
			target.Spec.Offer = offer.DeepCopy()
			if e := s.Client.Update(ctx, target); e != nil {
				return ctrl.Result{}, e
			}
		}
		return report(bitcoin.InitializationPreparing, reasonOpportunityOutstanding)
	}
	if record.Status.NextOpportunityAt != nil && s.Now().Before(record.Status.NextOpportunityAt.Time) {
		return report(bitcoin.InitializationPreparing, reasonCadenceWaiting)
	}
	interval, e := s.interval(
		effectiveSchedule(record, production.Status.Admission.Configuration.BitcoinBlockProduction.Schedule),
	)
	if e != nil {
		return report(bitcoin.InitializationBlocked, reasonCadenceUnavailable)
	}
	if record.Status.NextOpportunityAt == nil {
		next := metav1.NewTime(s.Now().Add(interval).UTC())
		record.Status.NextOpportunityAt = &next
		return report(bitcoin.InitializationPreparing, reasonCadenceArmed)
	}
	if s.Now().Before(record.Status.NextOpportunityAt.Time) {
		return report(bitcoin.InitializationPreparing, reasonCadenceWaiting)
	}
	wallet := record.Spec.PayoutWallet
	for _, candidate := range record.Spec.MinerWallets {
		if record.Status.PreparedAt != nil {
			break
		}
		count := int32(0)
		for _, funded := range record.Status.Funded {
			if funded.WalletUID == candidate.Wallet.UID {
				count = funded.Outputs
			}
		}
		if count < record.Spec.MatureOutputsPerMiner {
			wallet = candidate
			break
		}
	}
	number := max(record.Status.LastAccountedOffer, target.Status.CompletedOffer)
	if record.Status.Offer != nil {
		number = max(number, record.Status.Offer.Number)
	}
	if target.Spec.Offer != nil {
		number = max(number, target.Spec.Offer.Number)
	}
	if number == math.MaxInt64 {
		return report(bitcoin.InitializationBlocked, reasonOpportunityCounterExhausted)
	}
	number++
	next := metav1.NewTime(s.Now().Add(interval).UTC())
	record.Status.NextOpportunityAt = &next
	record.Status.Offer = &bitcoin.BitcoinBlockOffer{
		Override:       overrideBinding(record),
		Initialization: objectref.BitcoinInitialization(record),
		Production:     objectref.Participant(production),
		PolicyDigest:   production.Status.Admission.PolicyDigest,
		Number:         number,
		Address:        wallet.Address,
		Wallet:         wallet.Wallet,
		ExpectedHeight: height,
		ExpectedTip:    tip,
		Ceiling:        authority.gate.BitcoinCeiling,
		ExpiresAt:      metav1.NewTime(s.Now().Add(max(interval, bootstrapAdmissionWindow)).UTC()),
	}
	result, e := report(bitcoin.InitializationPreparing, reasonOpportunitySelected)
	result.RequeueAfter = time.Millisecond
	return result, e
}

// interval validates the compiled cadence and samples only within its declared bounds.
func (s *Scheduler) interval(schedule *bitcoin.BitcoinBlockScheduleSpec) (time.Duration, error) {
	low, high, err := cadenceBounds(schedule)
	if err != nil {
		return 0, err
	}
	if low == high {
		return low, nil
	}
	draw := s.Draw(int64(high-low) + 1)
	if draw < 0 || draw > int64(high-low) {
		return 0, fmt.Errorf("invalid cadence draw")
	}
	return low + time.Duration(draw), nil
}

// cadenceBounds validates complete timing independently of random selection.
func cadenceBounds(schedule *bitcoin.BitcoinBlockScheduleSpec) (time.Duration, time.Duration, error) {
	if schedule == nil {
		return 0, 0, fmt.Errorf("compiled schedule unavailable")
	}
	parse := func(value *common.Duration) (time.Duration, error) {
		if value == nil {
			return 0, fmt.Errorf("cadence interval unavailable")
		}
		d, err := time.ParseDuration(string(*value))
		if err != nil || d < time.Second || d > time.Hour {
			return 0, fmt.Errorf("unsupported cadence bound")
		}
		return d, nil
	}
	cadence := schedule.Cadence
	if cadence.Mode == bitcoin.CadenceFixed {
		if cadence.MinimumInterval != nil || cadence.MaximumInterval != nil {
			return 0, 0, fmt.Errorf("fixed cadence fields conflict")
		}
		d, err := parse(cadence.Interval)
		return d, d, err
	}
	if cadence.Mode != bitcoin.CadenceUniform || cadence.Interval != nil {
		return 0, 0, fmt.Errorf("unsupported cadence mode")
	}
	low, err := parse(cadence.MinimumInterval)
	if err != nil {
		return 0, 0, err
	}
	high, err := parse(cadence.MaximumInterval)
	if err != nil || high < low {
		return 0, 0, fmt.Errorf("invalid uniform cadence")
	}
	return low, high, nil
}
