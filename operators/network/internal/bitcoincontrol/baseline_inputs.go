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
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// baselineCompleted requires every captured gate before granting ceiling-free baseline authority.
func baselineCompleted(ctx context.Context, reader client.Reader, root *api.StacksNetwork, initial *bitcoin.BitcoinInitialization) error {
	state, ref := root.Status.Initialization, root.Status.GenesisRef
	if state == nil || !state.Completed || initial.Status.PreparedAt == nil || ref == nil || *ref != initial.Spec.Genesis || state.GenesisUID != ref.UID || state.GenesisDigest != root.Status.GenesisDigest {
		return fmt.Errorf("completed initialization binding unavailable")
	}
	var genesis api.StacksGenesis
	if err := reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: ref.Name}, &genesis); err != nil {
		return err
	}
	if genesis.UID != ref.UID || genesis.DeletionTimestamp != nil || !metav1.IsControlledBy(&genesis, root) || genesis.Spec.Source.NetworkUID != root.UID || foundation.Digest(genesis.Spec) != ref.Fingerprint || foundation.Digest(genesis.Spec.Chain) != root.Status.GenesisDigest {
		return fmt.Errorf("completed genesis identity differs")
	}
	gates := genesis.Spec.Bootstrap.Gates
	if len(gates) == 0 || len(gates) != len(state.Gates) || int(state.GateIndex) != len(gates) || gates[0].Name != api.GatePrepareBitcoin || gates[0].BitcoinCeiling != initial.Spec.MinimumHeight || state.AuthorizedCeiling != gates[len(gates)-1].BitcoinCeiling {
		return fmt.Errorf("completed gate inventory differs")
	}
	for i, gate := range gates {
		if state.Gates[i].Name != gate.Name || state.Gates[i].CompletedAt == nil || i > 0 && gate.BitcoinCeiling <= gates[i-1].BitcoinCeiling {
			return fmt.Errorf("captured gate completion unavailable")
		}
	}
	return nil
}

// baselineInputs validates the admitted policy without following rejected candidate changes.
func baselineInputs(ctx context.Context, reader client.Reader, root *api.StacksNetwork, initial *bitcoin.BitcoinInitialization, p *api.StacksNetworkParticipant) (bitcoin.BitcoinSchedulingStatus, error) {
	return resolveBaselineInputs(ctx, reader, root, initial, p, false)
}

// resolveBaselineInputs keeps scheduler credential metadata checks separate from public dispatch inputs.
func resolveBaselineInputs(ctx context.Context, reader client.Reader, root *api.StacksNetwork, initial *bitcoin.BitcoinInitialization, p *api.StacksNetworkParticipant, publicOnly bool) (bitcoin.BitcoinSchedulingStatus, error) {
	out := bitcoin.BitcoinSchedulingStatus{Initialization: binding("BitcoinInitialization", initial)}
	if !participantCurrent(root, p) || p.Spec.Kind != api.ParticipantBitcoinBlockProduction || p.Status.Admission.Configuration.BitcoinBlockProduction == nil {
		return out, fmt.Errorf("admitted production identity unavailable")
	}
	policy := p.Status.Admission.Configuration.BitcoinBlockProduction
	if policy.Schedule == nil || policy.PayoutWalletRef == nil || policy.Initialization == nil || policy.Initialization.MinimumHeight != initial.Spec.MinimumHeight || policy.Initialization.MatureOutputsPerMiner != initial.Spec.MatureOutputsPerMiner {
		return out, fmt.Errorf("baseline policy incomplete")
	}
	if err := foundation.ValidateAdmissionEligibility(ctx, reader, p); err != nil {
		return out, err
	}
	var payout *common.Binding
	for i := range p.Status.Admission.Dependencies {
		dep := p.Status.Admission.Dependencies[i]
		if dep.Kind == "BitcoinWallet" && dep.Name == policy.PayoutWalletRef.Name {
			copy := dep
			payout = &copy
		}
		if dep.Kind == "BitcoinBlockSchedule" {
			if out.ScheduleRef != nil {
				return out, fmt.Errorf("ambiguous admitted schedule")
			}
			var schedule bitcoin.BitcoinBlockSchedule
			if err := reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: dep.Name}, &schedule); err != nil || schedule.UID != dep.UID || schedule.DeletionTimestamp != nil || foundation.Digest(schedule.Spec) != dep.Fingerprint || !equality.Semantic.DeepEqual(policy.Schedule, &schedule.Spec) {
				return out, fmt.Errorf("admitted schedule identity unavailable")
			}
			copy := dep
			out.ScheduleRef = &copy
			out.ScheduleGeneration = schedule.Generation
		}
	}
	if payout == nil || *payout != initial.Spec.PayoutWallet.Wallet {
		return out, fmt.Errorf("captured payout identity differs")
	}
	validate := foundation.ValidateDependencyBindings
	if publicOnly {
		validate = foundation.ValidatePublicDependencyBindings
	}
	if err := validate(ctx, reader, root, []common.Binding{*payout}); err != nil {
		return out, err
	}
	var wallet bitcoin.BitcoinWallet
	if err := reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: payout.Name}, &wallet); err != nil {
		return out, err
	}
	observed, err := publicWallet(&wallet)
	if err != nil || observed != initial.Spec.PayoutWallet {
		return out, fmt.Errorf("captured public payout differs")
	}
	targets := ptr.Deref(policy.Targets, nil)
	if len(targets) == 0 || len(targets) > 100 {
		return out, fmt.Errorf("admitted baseline targets unavailable")
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if target.Weight < 1 || seen[target.NodeRef.Name] {
			return out, fmt.Errorf("invalid weighted target")
		}
		seen[target.NodeRef.Name] = true
		var uid string
		for _, id := range root.Status.Identities {
			if id.Name == target.NodeRef.Name {
				uid = string(id.UID)
			}
		}
		var pin *common.Binding
		for _, dep := range p.Status.Admission.Dependencies {
			if dep.Kind == "StacksNetworkParticipant" && string(dep.UID) == uid && uid != "" {
				copy := dep
				pin = &copy
				break
			}
		}
		if pin == nil {
			return out, fmt.Errorf("target admission binding unavailable")
		}
		out.Targets = append(out.Targets, bitcoin.BitcoinScheduledTarget{Participant: *pin, Weight: target.Weight})
	}
	out.PolicyDigest = p.Status.Admission.PolicyDigest
	out.AdmissionDigest = foundation.Digest(struct {
		Production common.Binding
		Admission  *api.Admission
	}{binding("StacksNetworkParticipant", p), p.Status.Admission})
	out.Schedule = policy.Schedule.DeepCopy()
	_, upper, err := cadenceBounds(out.Schedule)
	if err != nil {
		return out, err
	}
	out.ProgressWindowSeconds = int64((foundation.ProgressWindow(upper) + time.Second - 1) / time.Second)
	return out, nil
}

// selectBaselineTarget draws once from declared weights without consulting target health.
func (s *Scheduler) selectBaselineTarget(targets []bitcoin.BitcoinScheduledTarget) (common.Binding, error) {
	total := int64(0)
	for _, target := range targets {
		if target.Weight < 1 || total > math.MaxInt64-int64(target.Weight) {
			return common.Binding{}, fmt.Errorf("invalid target weights")
		}
		total += int64(target.Weight)
	}
	if total == 0 {
		return common.Binding{}, fmt.Errorf("no weighted targets")
	}
	draw := s.Draw(total)
	if draw < 0 || draw >= total {
		return common.Binding{}, fmt.Errorf("invalid target draw")
	}
	for _, target := range targets {
		if draw < int64(target.Weight) {
			return target.Participant, nil
		}
		draw -= int64(target.Weight)
	}
	return common.Binding{}, fmt.Errorf("target draw unavailable")
}
