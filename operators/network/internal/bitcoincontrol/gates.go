package bitcoincontrol

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// gateAuthority binds the aggregate's current ceiling to immutable public requirements.
type gateAuthority struct {
	genesis *api.StacksGenesis
	gate    api.Gate
	index   int
}

// currentGate accepts only exact genesis identity, ordered prior completions and a frozen ceiling.
func currentGate(ctx context.Context, reader client.Reader, root *api.StacksNetwork, initial *bitcoin.BitcoinInitialization) (gateAuthority, error) {
	var authority gateAuthority
	state := root.Status.Initialization
	ref := root.Status.GenesisRef
	if ref == nil || ref.Kind != "StacksGenesis" || ref.Name != initial.Spec.Genesis.Name || ref.UID != initial.Spec.Genesis.UID || ref.Fingerprint != initial.Spec.Genesis.Fingerprint {
		return authority, errors.New("bootstrap gate authority unavailable")
	}
	var genesis api.StacksGenesis
	if err := reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: ref.Name}, &genesis); err != nil {
		return authority, err
	}
	if genesis.UID != ref.UID || genesis.Spec.Source.NetworkUID != root.UID || genesis.DeletionTimestamp != nil || !metav1.IsControlledBy(&genesis, root) || foundation.Digest(genesis.Spec.Chain) != root.Status.GenesisDigest || foundation.Digest(genesis.Spec) != ref.Fingerprint {
		return authority, errors.New("bootstrap genesis identity differs")
	}
	gates := genesis.Spec.Bootstrap.Gates
	if state == nil {
		if len(gates) == 0 || gates[0].Name != api.GatePrepareBitcoin || gates[0].BitcoinCeiling != initial.Spec.MinimumHeight {
			return authority, errors.New("initial frozen gate unavailable")
		}
		return gateAuthority{genesis: &genesis, gate: gates[0], index: 0}, nil
	}
	if state.Completed || state.AuthorizedCeiling <= 0 || state.GenesisUID != ref.UID || state.GenesisDigest != root.Status.GenesisDigest {
		return authority, errors.New("bootstrap gate authority unavailable")
	}
	index := int(state.GateIndex)
	if index < 0 || index >= len(gates) || len(state.Gates) != len(gates) || gates[index].BitcoinCeiling != state.AuthorizedCeiling || len(gates) == 0 || gates[0].Name != api.GatePrepareBitcoin || gates[0].BitcoinCeiling != initial.Spec.MinimumHeight {
		return authority, errors.New("bootstrap ceiling differs from frozen requirements")
	}
	for i, gate := range gates {
		if state.Gates[i].Name != gate.Name || i > 0 && gate.BitcoinCeiling <= gates[i-1].BitcoinCeiling {
			return authority, errors.New("bootstrap gate order differs")
		}
		if i < index && state.Gates[i].CompletedAt == nil {
			return authority, errors.New("prior bootstrap gate is incomplete")
		}
		if i >= index && state.Gates[i].CompletedAt != nil {
			return authority, errors.New("bootstrap completion index differs")
		}
	}
	if index > 0 && initial.Status.PreparedAt == nil {
		return authority, errors.New("Bitcoin preparation is not retained")
	}
	return gateAuthority{genesis: &genesis, gate: gates[index], index: index}, nil
}

// advancementReady revalidates captured Stacks actors and legacy confirmation demand.
// It is shared by selection and the worker's last authorization before sending.
func advancementReady(ctx context.Context, reader client.Reader, root *api.StacksNetwork, initial *bitcoin.BitcoinInitialization, authority gateAuthority, height int64, now time.Time) (bool, error) {
	if authority.index == 0 {
		return true, nil
	}
	pending := false
	firstAnchor := false
	establishedChain := false
	for _, required := range authority.genesis.Spec.Bootstrap.Requirements {
		if required.Participant.UID == initial.Spec.Production.UID {
			continue
		}
		var p api.StacksNetworkParticipant
		if err := reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: required.Participant.Name}, &p); err != nil {
			return false, err
		}
		if p.UID != required.Participant.UID || !participantCurrent(root, &p) {
			return false, errors.New("captured participant identity unavailable")
		}
		if p.Spec.Kind != required.Kind {
			return false, errors.New("captured participant role differs")
		}
		switch required.Kind {
		case api.ParticipantStacksNode, api.ParticipantStacksSigner:
			rt := p.Status.Runtime
			if p.Spec.Control != nil && ptr.Deref(p.Spec.Control.Suspended, false) || rt == nil || rt.ObservedGeneration != p.Generation || rt.PolicyDigest != p.Status.Admission.PolicyDigest || rt.PodRef == nil || rt.PodRef.UID == "" || rt.ConfigRef == nil || rt.ConfigRef.UID == "" || rt.ContainerID == "" || rt.Terminated {
				return false, errors.New("captured Stacks actor is not ready")
			}
			for _, kind := range []string{"WorkloadReady", "ConfigVerified"} {
				condition := meta.FindStatusCondition(p.Status.Conditions, kind)
				if condition == nil || condition.Status != metav1.ConditionTrue || condition.ObservedGeneration != p.Generation {
					return false, errors.New("captured Stacks actor is not verified")
				}
			}
			if p.Spec.Kind == api.ParticipantStacksNode {
				if view := rt.Protocol; view != nil && root.Status.GenesisRef != nil && view.GenesisUID == root.Status.GenesisRef.UID && (view.HighestStacksHeight > 0 || view.StacksHeight > 0) {
					establishedChain = true
				}
				if ptr.Deref(required.MiningEnabled, false) && awaitingFirstAnchor(root, rt, height, now) {
					firstAnchor = true
				}
			}
		case api.ParticipantStacksStacker:
			if p.Spec.Control != nil && ptr.Deref(p.Spec.Control.Paused, false) {
				continue
			}
			execution := p.Status.Execution
			if execution == nil || execution.Pending < 1 || execution.ProcessNonce == "" || execution.Phase != api.WorkerPhaseActive || execution.ObservedGeneration != p.Generation || execution.NetworkGeneration != root.Generation || execution.AppliedPolicyDigest != p.Status.Admission.PolicyDigest || now.Sub(execution.ObservedAt.Time) > 10*time.Second || execution.ObservedAt.After(now) || execution.Transactions == nil || execution.Transactions.Offered < 1 || !hashValid(execution.Transactions.LastTxID) {
				continue
			}
			for _, identity := range root.Status.Identities {
				if identity.UID == p.UID && identity.Worker != nil && identity.Worker.Shutdown == nil && identity.Worker.Disposal == nil && identity.Worker.Pod.UID == execution.PodUID && identity.Worker.ProfileDigest == execution.ProfileDigest {
					pending = true
				}
			}
		}
	}
	if authority.gate.Name == api.GateEnrollPoX4 {
		activation := int64(-1)
		for _, epoch := range authority.genesis.Spec.Chain.Epochs {
			if epoch.Name == "2.5" && epoch.StartHeight >= 0 && epoch.StartHeight < math.MaxInt64 {
				activation = epoch.StartHeight + 1
			}
		}
		if activation < 0 || activation >= authority.gate.BitcoinCeiling {
			return false, errors.New("PoX activation window unavailable")
		}
		if height >= activation {
			return pending || firstAnchor && !establishedChain, nil
		}
	}
	return true, nil
}

// awaitingFirstAnchor distinguishes confirmed empty native chainstate from unavailable observations.
func awaitingFirstAnchor(root *api.StacksNetwork, runtime *api.ParticipantRuntimeStatus, height int64, now time.Time) bool {
	view := runtime.Protocol
	policy := foundation.ObservationPolicy()
	freshness := time.Duration(3*policy.PollIntervalSeconds+policy.RPCAllowanceSeconds) * time.Second
	return view != nil && !view.Available && view.Reason == "AwaitingFirstAnchor" && view.NetworkID == 0x80000000 && view.StacksHeight == 0 && view.HighestStacksHeight == 0 && view.StacksTip == strings.Repeat("0", 64) && view.FullySynced && height >= 0 && view.BurnHeight == uint64(height) && root.Status.GenesisRef != nil && view.GenesisUID == root.Status.GenesisRef.UID && runtime.PodRef != nil && view.PodUID == runtime.PodRef.UID && view.ContainerID == runtime.ContainerID && view.ConfigurationDigest == runtime.ConfigurationDigest && !view.ObservedAt.IsZero() && !view.ObservedAt.After(now) && now.Sub(view.ObservedAt.Time) <= freshness
}
