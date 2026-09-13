package stacksworker

import (
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
)

func TestAllocationWaitsForIdentityPublication(t *testing.T) {
	root, p, _, _ := fixture(t)
	root.Status.Identities = nil
	p.Status = api.ParticipantStatus{}
	fact := ProjectSession(root, p, nil, nil, time.Now())
	if fact.Failed || fact.Changed || !fact.Unknown || fact.Reason != "AllocationPending" || len(root.Status.Identities) != 0 {
		t.Fatalf("allocation must not bind or fail: %+v", fact)
	}
	root.Status.Identities = []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: p.UID}}
	fact = ProjectSession(root, p, nil, nil, time.Now())
	if fact.Failed || fact.Changed || fact.Unknown || fact.Reason != "CandidatePending" || root.Status.Identities[0].Worker != nil {
		t.Fatalf("published identity must await a candidate: %+v", fact)
	}
}

func TestMissingSessionHistoryCannotBecomeAllocation(t *testing.T) {
	for _, name := range []string{"admitted", "candidate", "execution", "wrong-name", "wrong-uid", "deselected", "foreign-owner", "finalizer", "pod", "workload", "config", "process"} {
		t.Run(name, func(t *testing.T) {
			root, p, _, _ := fixture(t)
			admitted, candidate := p.Status.Admission, p.Status.Runtime
			root.Status.Identities = nil
			p.Status = api.ParticipantStatus{}
			switch name {
			case "admitted":
				p.Status.Admission = admitted
			case "candidate":
				p.Status.Runtime = candidate
			case "execution":
				p.Status.Execution = &api.WorkerExecutionStatus{}
			case "wrong-name":
				p.Name = "another-name"
			case "wrong-uid":
				root.Status.Identities = []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: "old-uid"}}
			case "deselected":
				root.Spec.Participants = nil
			case "finalizer":
				p.Finalizers = []string{Finalizer}
			case "pod":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{PodRef: &common.Binding{Kind: "Pod", Name: "prior", UID: "prior"}}
			case "workload":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{WorkloadRefs: []common.Binding{{Kind: "Pod", Name: "prior", UID: "prior"}}}
			case "config":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{ConfigRef: &common.Binding{Kind: "ConfigMap", Name: "prior", UID: "prior"}}
			case "process":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{ContainerID: "prior"}
			case "foreign-owner":
				p.OwnerReferences = nil
			}
			fact := ProjectSession(root, p, nil, nil, time.Now())
			if !fact.Failed || fact.Reason != "WorkerIdentityLost" || fact.Changed {
				t.Fatalf("lost identity must remain failed: %+v", fact)
			}
		})
	}
}
