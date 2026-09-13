package stacksoperation

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PublicInputs resolves only named public resources through a worker's scoped reader.
type PublicInputs struct {
	// Reader is uncached; it must not provide Secret or workload mutation access.
	Reader client.Reader
	// Sender is the address verified against the mounted signing key at process start.
	Sender string
}

// account validates the exact admitted account fingerprint without reading key material.
func (r PublicInputs) account(ctx context.Context, p *api.StacksNetworkParticipant, name string) (*stacks.StacksAccount, error) {
	binding, err := dependency(p, "StacksAccount", name)
	if err != nil {
		return nil, err
	}
	var account stacks.StacksAccount
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: name}, &account); err != nil {
		return nil, err
	}
	if account.UID != binding.UID || account.DeletionTimestamp != nil || account.Status.Digest != binding.Fingerprint || account.Status.Identity == nil || account.Status.ObservedGeneration != account.Generation || !meta.IsStatusConditionTrue(account.Status.Conditions, "Resolved") {
		return nil, fmt.Errorf("account identity unavailable")
	}
	return &account, nil
}

// target resolves logical participant membership to its exact admitted runtime identity.
func (r PublicInputs) target(ctx context.Context, s stacksworker.Snapshot, name string) (*api.StacksNetworkParticipant, error) {
	var identity *api.InstanceIdentity
	for i := range s.Network.Status.Identities {
		if s.Network.Status.Identities[i].Name == name {
			identity = &s.Network.Status.Identities[i]
			break
		}
	}
	if identity == nil || identity.Removing {
		return nil, fmt.Errorf("target membership unavailable")
	}
	selected := false
	for _, entry := range s.Network.Spec.Participants {
		if entry.Name == name && entry.Kind == api.ParticipantStacksNode {
			selected = true
		}
	}
	if !selected {
		return nil, fmt.Errorf("target is not selected")
	}
	var binding *common.Binding
	for i := range s.Participant.Status.Admission.Dependencies {
		b := &s.Participant.Status.Admission.Dependencies[i]
		if b.Kind == "StacksNetworkParticipant" && b.UID == identity.UID {
			binding = b
			break
		}
	}
	if binding == nil {
		return nil, fmt.Errorf("target admission binding missing")
	}
	var target api.StacksNetworkParticipant
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: s.Participant.Namespace, Name: binding.Name}, &target); err != nil {
		return nil, err
	}
	if target.UID != binding.UID || target.Spec.ParticipantName != name || target.Spec.Kind != api.ParticipantStacksNode || target.Spec.NetworkUID != s.Network.UID || !metav1.IsControlledBy(&target, s.Network) || target.DeletionTimestamp != nil || target.Status.Admission == nil || target.Status.Runtime == nil {
		return nil, fmt.Errorf("target identity unavailable")
	}
	runtime := target.Status.Runtime
	if runtime.ObservedGeneration != target.Generation || runtime.PolicyDigest != target.Status.Admission.PolicyDigest || runtime.PodRef == nil || runtime.ContainerID == "" || runtime.Terminated {
		return nil, fmt.Errorf("target runtime unavailable")
	}
	for _, kind := range []string{"ConfigVerified", "WorkloadReady"} {
		c := meta.FindStatusCondition(target.Status.Conditions, kind)
		if c == nil || c.Status != metav1.ConditionTrue || c.ObservedGeneration != target.Generation {
			return nil, fmt.Errorf("target runtime is not verified and ready")
		}
	}
	return &target, nil
}

// Transfer resolves the complete currently admitted policy and frozen activation schedule.
func (r PublicInputs) Transfer(ctx context.Context, s stacksworker.Snapshot) (TransferInputs, error) {
	if r.Reader == nil || s.Network == nil || s.Participant == nil || s.Participant.Status.Admission == nil {
		return TransferInputs{}, fmt.Errorf("transfer admission unavailable")
	}
	policy := s.Participant.Status.Admission.Configuration.StacksTransactionProduction
	if policy == nil || policy.AccountRef == nil || policy.TargetNodeRef == nil || policy.Recipient == nil || policy.AmountMicroSTX == nil || policy.FeeMicroSTX == nil || policy.Interval == nil {
		return TransferInputs{}, fmt.Errorf("transfer policy incomplete")
	}
	account, err := r.account(ctx, s.Participant, policy.AccountRef.Name)
	if err != nil {
		return TransferInputs{}, err
	}
	if account.Status.Identity.Address != r.Sender {
		return TransferInputs{}, fmt.Errorf("sender changed")
	}
	recipient := ""
	if policy.Recipient.Address != nil {
		recipient = *policy.Recipient.Address
	} else if policy.Recipient.AccountRef != nil {
		a, err := r.account(ctx, s.Participant, policy.Recipient.AccountRef.Name)
		if err != nil {
			return TransferInputs{}, err
		}
		recipient = a.Status.Identity.Address
	}
	target, err := r.target(ctx, s, policy.TargetNodeRef.Name)
	if err != nil {
		return TransferInputs{}, err
	}
	if net.ParseIP(target.Status.Runtime.PodIP) == nil {
		return TransferInputs{}, fmt.Errorf("target Pod address unavailable")
	}
	endpoint := "http://" + net.JoinHostPort(target.Status.Runtime.PodIP, "20443")
	node, err := rpc.New(rpc.Config{Endpoint: endpoint, Timeout: 10 * time.Second, MaxResponseBytes: 2 << 20})
	if err != nil {
		return TransferInputs{}, err
	}
	amount, err := strconv.ParseUint(string(*policy.AmountMicroSTX), 10, 64)
	if err != nil {
		return TransferInputs{}, err
	}
	fee, err := strconv.ParseUint(string(*policy.FeeMicroSTX), 10, 64)
	if err != nil {
		return TransferInputs{}, err
	}
	interval, err := time.ParseDuration(string(*policy.Interval))
	if err != nil {
		return TransferInputs{}, err
	}
	if s.Network.Status.GenesisRef == nil {
		return TransferInputs{}, fmt.Errorf("genesis unavailable")
	}
	var genesis api.StacksGenesis
	ref := s.Network.Status.GenesisRef
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: s.Network.Namespace, Name: ref.Name}, &genesis); err != nil {
		return TransferInputs{}, err
	}
	if genesis.UID != ref.UID || genesis.DeletionTimestamp != nil || !metav1.IsControlledBy(&genesis, s.Network) || foundation.Digest(genesis.Spec.Chain) != s.Network.Status.GenesisDigest {
		return TransferInputs{}, fmt.Errorf("genesis identity unavailable")
	}
	for _, epoch := range genesis.Spec.Chain.Epochs {
		if epoch.Name == "3.0" && epoch.StartHeight >= 0 {
			return TransferInputs{Node: node, Target: api.TrafficObservation{TargetParticipantUID: target.UID, TargetPodUID: target.Status.Runtime.PodRef.UID, TargetContainerID: target.Status.Runtime.ContainerID, TargetConfigurationDigest: target.Status.Runtime.ConfigurationDigest, GenesisUID: genesis.UID}, Recipient: recipient, Amount: amount, Fee: fee, Interval: interval, StartHeight: uint64(epoch.StartHeight)}, nil
		}
	}
	return TransferInputs{}, fmt.Errorf("epoch-3 activation unavailable")
}

// dependency finds one exact mandatory named input in the admitted policy.
func dependency(p *api.StacksNetworkParticipant, kind, name string) (common.Binding, error) {
	if p.Status.Admission != nil {
		for _, b := range p.Status.Admission.Dependencies {
			if b.Kind == kind && b.Name == name && b.UID != "" {
				return b, nil
			}
		}
	}
	return common.Binding{}, fmt.Errorf("admitted dependency unavailable")
}
