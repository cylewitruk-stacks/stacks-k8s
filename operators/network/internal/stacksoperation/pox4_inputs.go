package stacksoperation

import (
	"context"
	"errors"
	"math/big"
	"net"
	"net/url"
	"strconv"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PoX4 resolves current public identities and preserves initial cohort requirements independently of late joins.
func (r PublicInputs) PoX4(ctx context.Context, s stacksworker.Snapshot) (PoX4Inputs, error) {
	var input PoX4Inputs
	if r.Reader == nil || s.Network == nil || s.Participant == nil || s.Participant.Status.Admission == nil {
		return input, errors.New("PoX admission unavailable")
	}
	policy := s.Participant.Status.Admission.Configuration.StacksStacker
	if policy == nil || policy.HolderAccountRef == nil || policy.SignerRef == nil || policy.TargetNodeRef == nil ||
		policy.AmountMicroSTX == nil ||
		policy.LockCycles == nil ||
		policy.RenewWhenRemainingCycles == nil {
		return input, errors.New("PoX policy incomplete")
	}
	if *policy.LockCycles < 1 || *policy.LockCycles > 12 || *policy.RenewWhenRemainingCycles < 0 ||
		*policy.RenewWhenRemainingCycles >= *policy.LockCycles {
		return input, errors.New("invalid PoX cycle policy")
	}
	holder, err := r.account(ctx, s.Participant, policy.HolderAccountRef.Name)
	if err != nil {
		return input, err
	}
	if holder.Status.Identity.Address != r.Sender {
		return input, errors.New("PoX holder identity changed")
	}
	signerName := foundation.ParticipantName(string(s.Network.UID), policy.SignerRef.Name)
	signerBinding, err := dependency(s.Participant, api.KindStacksNetworkParticipant, signerName)
	if err != nil {
		return input, err
	}
	selected := false
	for _, entry := range s.Network.Spec.Participants {
		selected = selected || entry.Kind == api.ParticipantStacksSigner && entry.Name == policy.SignerRef.Name
	}
	allocated := false
	for _, id := range s.Network.Status.Identities {
		if id.Name == policy.SignerRef.Name {
			allocated = id.UID == signerBinding.UID && !id.Removing
		}
	}
	if !selected || !allocated {
		return input, errors.New("PoX signer membership unavailable")
	}
	var signer api.StacksNetworkParticipant
	if err = r.Reader.Get(
		ctx,
		client.ObjectKey{Namespace: s.Participant.Namespace, Name: signerBinding.Name},
		&signer,
	); err != nil {
		return input, err
	}
	if signer.UID != signerBinding.UID || signer.Spec.NetworkUID != s.Network.UID ||
		signer.Spec.Kind != api.ParticipantStacksSigner ||
		signer.Spec.ParticipantName != policy.SignerRef.Name ||
		signer.DeletionTimestamp != nil ||
		!metav1.IsControlledBy(&signer, s.Network) ||
		signer.Status.Admission == nil ||
		signer.Status.Admission.Configuration.StacksSigner == nil ||
		signer.Status.Admission.Configuration.StacksSigner.AccountRef == nil {
		return input, errors.New("PoX signer identity unavailable")
	}
	consensus, err := r.account(ctx, &signer, signer.Status.Admission.Configuration.StacksSigner.AccountRef.Name)
	if err != nil {
		return input, err
	}
	target, err := r.target(ctx, s, policy.TargetNodeRef.Name)
	if err != nil {
		return input, err
	}
	endpoint := ""
	for _, ep := range target.Status.Runtime.Endpoints {
		if ep.Name == common.EndpointRPC && ep.Host != "" && ep.Port > 0 && ep.Port <= 65535 {
			endpoint = (&url.URL{Scheme: "http", Host: net.JoinHostPort(ep.Host, strconv.Itoa(int(ep.Port)))}).String()
		}
	}
	node, err := rpc.New(rpc.Config{Endpoint: endpoint, Timeout: 10 * time.Second, MaxResponseBytes: 2 << 20})
	if err != nil {
		return input, err
	}
	amount, ok := new(big.Int).SetString(string(*policy.AmountMicroSTX), 10)
	if !ok || amount.Sign() <= 0 || amount.BitLen() > 128 {
		return input, errors.New("PoX amount invalid")
	}
	genesis, err := r.inputGenesis(ctx, s, api.ParticipantStacksStacker)
	if err != nil {
		return input, err
	}
	initial, err := initialRequirement(genesis, s.Participant)
	if err != nil {
		return input, err
	}
	captured := false
	for _, required := range genesis.Spec.Bootstrap.Requirements {
		if required.Kind != api.ParticipantStacksStacker || required.Participant.UID != s.Participant.UID {
			continue
		}
		holderMatched, signerMatched := false, false
		for _, account := range required.Accounts {
			holderMatched = holderMatched ||
				account.Binding.UID == holder.UID && account.Identity == *holder.Status.Identity
		}
		for _, dep := range required.Dependencies {
			signerMatched = signerMatched || dep.Kind == api.KindStacksNetworkParticipant && dep.UID == signer.UID
		}
		captured = holderMatched && signerMatched
	}
	signerCaptured := false
	for _, required := range genesis.Spec.Bootstrap.Requirements {
		if required.Kind == api.ParticipantStacksSigner && required.Participant.UID == signer.UID {
			for _, account := range required.Accounts {
				signerCaptured = signerCaptured ||
					account.Binding.UID == consensus.UID && account.Identity == *consensus.Status.Identity
			}
		}
	}
	if initial != nil && (!captured || !signerCaptured) {
		return input, errors.New("captured PoX cohort identity unavailable")
	}
	input = PoX4Inputs{
		InitialCohort:            initial != nil,
		Node:                     node,
		Holder:                   r.Sender,
		SignerPublicKey:          consensus.Status.Identity.PublicKey,
		Amount:                   amount,
		LockCycles:               uint64(*policy.LockCycles),
		RenewWhenRemainingCycles: uint64(*policy.RenewWhenRemainingCycles),
	}
	for _, gate := range genesis.Spec.Bootstrap.Gates {
		if gate.Name == api.GateEnrollPoX4 && gate.TargetCycle != nil && *gate.TargetCycle >= 0 &&
			gate.BitcoinCeiling > 0 {
			if initial != nil {
				input.TargetCycle = uint64(*gate.TargetCycle)
			}
			input.EnrollmentCeiling = uint64(gate.BitcoinCeiling)
		}
	}
	for _, epoch := range genesis.Spec.Chain.Epochs {
		if epoch.Name == "3.0" && epoch.StartHeight > 0 {
			input.Epoch3Height = uint64(epoch.StartHeight)
		}
	}
	if input.EnrollmentCeiling == 0 || input.Epoch3Height <= input.EnrollmentCeiling {
		return PoX4Inputs{}, errors.New("frozen PoX timing unavailable")
	}
	return input, nil
}
