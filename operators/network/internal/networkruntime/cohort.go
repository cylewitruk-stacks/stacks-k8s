package networkruntime

import (
	"crypto/sha256"
	"fmt"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// selectedInstance validates explicit membership and the aggregate's allocated identity.
func selectedInstance(root *api.StacksNetwork, p *api.StacksNetworkParticipant) bool {
	if p == nil || p.DeletionTimestamp != nil || p.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(p, root) {
		return false
	}
	selected := false
	for _, entry := range root.Spec.Participants {
		if entry.Name == p.Spec.ParticipantName && entry.Kind == p.Spec.Kind {
			selected = true
		}
	}
	if !selected {
		return false
	}
	for _, id := range root.Status.Identities {
		if id.Name == p.Spec.ParticipantName && id.UID == p.UID && !id.Removing {
			return true
		}
	}
	return false
}

// requiredParticipant finds a still-selected instance with its whole captured admission.
func requiredParticipant(root *api.StacksNetwork, requirement api.BootstrapRequirement, participants []api.StacksNetworkParticipant) *api.StacksNetworkParticipant {
	for i := range participants {
		p := &participants[i]
		if p.UID != requirement.Participant.UID || p.Name != requirement.Participant.Name || p.DeletionTimestamp != nil || p.Spec.Kind != requirement.Kind || p.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(p, root) || p.Status.Admission == nil || !foundation.BootstrapPolicyCompatible(requirement, p.Status.Admission.Configuration) {
			continue
		}
		for _, entry := range root.Spec.Participants {
			if entry.Name == p.Spec.ParticipantName && entry.Kind == p.Spec.Kind {
				return p
			}
		}
	}
	return nil
}

// requiredWorker verifies the process that produced a frozen prerequisite observation.
func requiredWorker(root *api.StacksNetwork, requirement api.BootstrapRequirement, participants []api.StacksNetworkParticipant) *api.StacksNetworkParticipant {
	p := requiredParticipant(root, requirement, participants)
	if p == nil {
		return nil
	}
	var session *api.WorkerSession
	for _, id := range root.Status.Identities {
		if id.Name == p.Spec.ParticipantName && id.UID == p.UID && !id.Removing {
			session = id.Worker
		}
	}
	execution := p.Status.Execution
	if session == nil || session.Shutdown != nil || session.Disposal != nil || execution == nil || execution.PodUID != session.Pod.UID || execution.ProfileDigest != session.ProfileDigest || execution.ProcessNonce == "" || execution.AppliedPolicyDigest != p.Status.Admission.PolicyDigest || execution.Phase == "Failed" || execution.Phase == "Unknown" || execution.ObservedGeneration != p.Generation {
		return nil
	}
	ready := meta.FindStatusCondition(p.Status.Conditions, "WorkloadReady")
	if ready == nil || ready.Status != metav1.ConditionTrue || ready.ObservedGeneration != p.Generation {
		return nil
	}
	return p
}

// capturedAccount returns only the identity in the immutable bootstrap requirement.
func capturedAccount(requirement api.BootstrapRequirement, name string) (address, publicKey string) {
	for _, account := range requirement.Accounts {
		if account.Binding.Name == name {
			return account.Identity.Address, account.Identity.PublicKey
		}
	}
	return "", ""
}

// capturedSigner checks the exact required consensus participant and its public signing key.
func capturedSigner(root *api.StacksNetwork, g *api.StacksGenesis, requirement api.BootstrapRequirement, p *api.StacksNetworkParticipant, participants []api.StacksNetworkParticipant, key string) bool {
	policy := p.Status.Admission.Configuration.StacksStacker
	if policy == nil || policy.SignerRef == nil {
		return false
	}
	for _, required := range g.Spec.Bootstrap.Requirements {
		if required.Kind != "StacksSigner" {
			continue
		}
		signer := requiredParticipant(root, required, participants)
		if signer == nil || signer.Spec.ParticipantName != policy.SignerRef.Name || signer.Status.Admission.Configuration.StacksSigner == nil || signer.Status.Admission.Configuration.StacksSigner.AccountRef == nil {
			continue
		}
		bound := false
		for _, dep := range requirement.Dependencies {
			if dep.Kind == "StacksNetworkParticipant" && dep.Name == signer.Name && dep.UID == signer.UID {
				bound = true
			}
		}
		_, expected := capturedAccount(required, signer.Status.Admission.Configuration.StacksSigner.AccountRef.Name)
		if bound && key != "" && key == expected {
			return true
		}
	}
	return false
}

// pox4CohortSatisfied checks every frozen stacker against its exact bound worker.
func pox4CohortSatisfied(root *api.StacksNetwork, g *api.StacksGenesis, participants []api.StacksNetworkParticipant, gate api.Gate, now time.Time) bool {
	if gate.TargetCycle == nil || *gate.TargetCycle < 0 {
		return false
	}
	found := false
	for _, requirement := range g.Spec.Bootstrap.Requirements {
		if requirement.Kind != "StacksStacker" {
			continue
		}
		found = true
		p := requiredWorker(root, requirement, participants)
		if p == nil {
			return false
		}
		o := p.Status.Execution.PoX4
		if o == nil || !fresh(o.ObservedAt, now) || !o.TargetCycleMatched || o.TargetCycle != uint64(*gate.TargetCycle) || o.FirstCycle > o.TargetCycle || o.EndCycleExclusive <= o.TargetCycle || requirement.AmountMicroSTX == nil || o.AmountMicroSTX != string(*requirement.AmountMicroSTX) {
			return false
		}
		policy := p.Status.Admission.Configuration.StacksStacker
		if policy == nil || policy.HolderAccountRef == nil {
			return false
		}
		holder, _ := capturedAccount(requirement, policy.HolderAccountRef.Name)
		if holder == "" || holder != o.Holder || !capturedSigner(root, g, requirement, p, participants, o.SignerPublicKey) {
			return false
		}
	}
	return found
}

// pox5CohortSatisfied adds exact manager source, holder membership and delegated stake checks.
func pox5CohortSatisfied(root *api.StacksNetwork, g *api.StacksGenesis, participants []api.StacksNetworkParticipant, gate api.Gate, now time.Time) bool {
	if gate.TargetCycle == nil || *gate.TargetCycle < 0 {
		return false
	}
	found := false
	for _, requirement := range g.Spec.Bootstrap.Requirements {
		if requirement.Kind != "StacksStacker" {
			continue
		}
		found = true
		p := requiredWorker(root, requirement, participants)
		if p == nil {
			return false
		}
		o := p.Status.Execution.PoX5
		if o == nil || !fresh(o.ObservedAt, now) || !o.TargetCycleMatched || o.TargetCycle != uint64(*gate.TargetCycle) || o.FirstCycle > o.TargetCycle || o.EndCycleExclusive <= o.TargetCycle || requirement.AmountMicroSTX == nil || o.AmountMicroSTX != string(*requirement.AmountMicroSTX) || o.DelegatedAmountMicroSTX != o.AmountMicroSTX {
			return false
		}
		policy := p.Status.Admission.Configuration.StacksStacker
		if policy == nil || policy.HolderAccountRef == nil || policy.AdministratorAccountRef == nil {
			return false
		}
		holder, _ := capturedAccount(requirement, policy.HolderAccountRef.Name)
		administrator, _ := capturedAccount(requirement, policy.AdministratorAccountRef.Name)
		source, err := protocolcontracts.DirectManager(holder)
		if err != nil || holder != o.Holder || administrator == "" || o.Manager != administrator+".direct-signer" || o.ManagerSourceDigest != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(source))) || !capturedSigner(root, g, requirement, p, participants, o.SignerPublicKey) {
			return false
		}
	}
	return found
}
