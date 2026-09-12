package stacksoperation

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// transferFixture supplies an exact ingress and an injected native observation clock.
func transferFixture(t *testing.T) (*TransferRole, *trafficNode, stacksworker.Snapshot, *TransferInputs, *time.Time) {
	t.Helper()
	key := strings.Repeat("0", 63) + "1"
	public, _ := identity.FromPrivate(key)
	role, err := NewTransferRole(key, public.Address)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	role.Now = func() time.Time { return now }
	node := &trafficNode{memoryNode: nodeFixture(), info: rpc.Info{NetworkID: 0x80000000, BurnHeight: 300}}
	input := &TransferInputs{Node: node, Recipient: public.Address, Amount: 1, Fee: 1, Interval: 10 * time.Second, StartHeight: 251, Target: api.TrafficObservation{TargetParticipantUID: "target", TargetPodUID: "pod", TargetContainerID: "container", TargetConfigurationDigest: "config", GenesisUID: "genesis"}}
	role.Resolve = func(context.Context, stacksworker.Snapshot) (TransferInputs, error) { return *input, nil }
	snapshot := stacksworker.Snapshot{Participant: &api.StacksNetworkParticipant{Status: api.ParticipantStatus{Admission: &api.Admission{PolicyDigest: "policy"}}}, Authorize: permit}
	return role, node, snapshot, input, &now
}
func TestTrafficCanonicalRecheckPreservesProgressAndReorgKnowledge(t *testing.T) {
	r, node, s, _, now := transferFixture(t)
	ctx := context.Background()
	got, _ := r.Step(ctx, s)
	if got.Reason != "Accepted" {
		t.Fatal(got)
	}
	node.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	*now = now.Add(time.Second)
	got, _ = r.Step(ctx, s)
	first := got.Transactions.LastInclusion.ObservedAt
	observed := got.Traffic.ObservedAt
	if got.Traffic == nil || !got.Traffic.Available || !got.Traffic.Found || got.Traffic.SubmittedBurnHeight != 300 || got.Traffic.TargetPodUID != "pod" || got.Traffic.ProgressWindowSeconds != 130 {
		t.Fatalf("missing exact first evidence: %+v", got)
	}
	s.Paused = true
	*now = now.Add(2 * time.Second)
	node.info.BurnHeight = 301
	got, _ = r.Step(ctx, s)
	if !got.Traffic.Available || got.Traffic.BurnHeight != 301 || !got.Traffic.ObservedAt.After(observed.Time) || got.Transactions.Included != 1 || !got.Transactions.LastInclusion.ObservedAt.Equal(&first) || node.sends != 1 {
		t.Fatal("paused recheck recounted or freshened progress")
	}
	originalReport := got.Traffic.DeepCopy()
	node.errorRead = errors.New("RPC unavailable")
	*now = now.Add(2 * time.Second)
	got, _ = r.Step(ctx, s)
	if got.Traffic.Available || !got.Traffic.ObservedAt.Equal(&originalReport.ObservedAt) {
		t.Fatal("RPC error freshened native evidence")
	}
	node.errorRead = nil
	node.inclusion = rpc.Inclusion{}
	*now = now.Add(2 * time.Second)
	got, _ = r.Step(ctx, s)
	if !got.Traffic.Available || got.Traffic.Found || got.Traffic.Success || got.Traffic.BlockID != "" || !got.Transactions.LastInclusion.ObservedAt.Equal(&first) {
		t.Fatal("canonical removal not exposed independently of original inclusion")
	}
}
func TestTrafficRechecksOriginalTargetWhileNewPolicyIsPending(t *testing.T) {
	r, original, s, input, now := transferFixture(t)
	ctx := context.Background()
	_, _ = r.Step(ctx, s)
	original.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	*now = now.Add(time.Second)
	first, _ := r.Step(ctx, s)
	next := &trafficNode{memoryNode: nodeFixture(), info: rpc.Info{NetworkID: 0x80000000, BurnHeight: 305}}
	next.account.Nonce = 8
	input.Node = next
	input.Target.TargetPodUID = "new-pod"
	input.Interval = 60 * time.Second
	s.Participant.Status.Admission.PolicyDigest = "next-policy"
	*now = now.Add(2 * time.Second)
	got, _ := r.Step(ctx, s)
	if next.sends != 1 || got.AppliedPolicyDigest != "next-policy" || got.Traffic.TargetPodUID != "pod" || got.Traffic.EffectiveIntervalSeconds != 60 || got.Traffic.ProgressWindowSeconds != 190 || got.Traffic.TxID != first.Traffic.TxID || got.Traffic.SubmittedBurnHeight != 300 {
		t.Fatalf("old inclusion retargeted or cadence stale: %+v", got)
	}
	original.inclusion = rpc.Inclusion{}
	*now = now.Add(2 * time.Second)
	got, _ = r.Step(ctx, s)
	if got.Traffic.Found || !got.Traffic.Available || got.Pending != 1 || got.Transactions.Included != 1 {
		t.Fatal("new pending blocked original canonical recheck")
	}
	next.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("c", 64)}
	*now = now.Add(2 * time.Second)
	got, _ = r.Step(ctx, s)
	if got.Traffic.TargetPodUID != "new-pod" || got.Traffic.SubmittedBurnHeight != 305 || got.Transactions.Included != 2 {
		t.Fatal("new inclusion lost its original submission binding")
	}
}
func TestTrafficPreparedIngressRollWithdrawsAuthorization(t *testing.T) {
	r, node, s, input, _ := transferFixture(t)
	reads := 0
	r.Resolve = func(context.Context, stacksworker.Snapshot) (TransferInputs, error) {
		reads++
		v := *input
		if reads > 1 {
			v.Target.TargetPodUID = "replacement"
		}
		return v, nil
	}
	got, _ := r.Step(context.Background(), s)
	if got.Reason != "AuthorizationUnavailable" || node.sends != 0 || r.inputs.value != nil {
		t.Fatal("prepared bytes sent after ingress incarnation changed")
	}
}
func TestTrafficFallbackUsesOnlyActuallyAppliedInputs(t *testing.T) {
	r, node, s, input, now := transferFixture(t)
	ctx := context.Background()
	remembered := 0
	s.RememberApplied = func(string) { remembered++ }
	_, _ = r.Step(ctx, s)
	node.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
	_, _ = r.Step(ctx, s)
	node.account.Nonce = 8
	applied := s
	applied.CachedApplied = true
	applied.RememberApplied = nil
	s.Participant = s.Participant.DeepCopy()
	s.Participant.Status.Admission.PolicyDigest = "new-unresolved"
	fallback := func(digest string, err error) (stacksworker.Snapshot, bool) {
		return applied, digest == "policy" && stacksworker.TransientAPIError(err)
	}
	s.AppliedFallback = fallback
	applied.AppliedFallback = fallback
	r.Resolve = func(context.Context, stacksworker.Snapshot) (TransferInputs, error) {
		return TransferInputs{}, apierrors.NewServiceUnavailable("offline")
	}
	input.Amount = 90
	*now = now.Add(10 * time.Second)
	got, _ := r.Step(ctx, s)
	if node.sends != 2 || got.AppliedPolicyDigest != "policy" || remembered != 1 {
		t.Fatalf("old baseline not retained or new candidate adopted: %+v", got)
	}
	node.account.Nonce = 9
	_, _ = r.Step(ctx, s)
	r.Resolve = func(context.Context, stacksworker.Snapshot) (TransferInputs, error) {
		return TransferInputs{}, errors.New("dependency UID changed")
	}
	*now = now.Add(10 * time.Second)
	got, _ = r.Step(ctx, s)
	if node.sends != 2 || got.Reason != "DependenciesUnavailable" || r.inputs.value != nil {
		t.Fatal("definite dependency denial retained cache")
	}
	got, _ = r.Step(ctx, applied)
	if got.Reason != "DependenciesUnavailable" || node.sends != 2 {
		t.Fatal("invalidated cache revived offline")
	}
}
func TestContractCacheClonesPublicCollections(t *testing.T) {
	cache := appliedInputs[ContractInputs]{}
	s := stacksworker.Snapshot{Participant: &api.StacksNetworkParticipant{Status: api.ParticipantStatus{Admission: &api.Admission{PolicyDigest: "one"}}}}
	input := ContractInputs{SourceHashes: map[string]string{"contract": "hash"}, SignerPublicKeys: []string{"key"}}
	cache.remember(s, input, cloneContractInputs)
	input.SourceHashes["contract"] = "changed"
	input.SignerPublicKeys[0] = "changed"
	s.CachedApplied = true
	resolve := func(context.Context, stacksworker.Snapshot) (ContractInputs, error) {
		t.Fatal("cached policy selection read Kubernetes")
		return ContractInputs{}, nil
	}
	got, _, err := cache.resolve(context.Background(), s, "one", resolve, cloneContractInputs)
	if err != nil || got.SourceHashes["contract"] != "hash" || got.SignerPublicKeys[0] != "key" {
		t.Fatal("cache aliased public configuration")
	}
	got.SourceHashes["contract"] = "changed-again"
	again, _, _ := cache.resolve(context.Background(), s, "one", resolve, cloneContractInputs)
	if again.SourceHashes["contract"] != "hash" {
		t.Fatal("cache read aliases retained policy")
	}
	s.Participant.Status.Admission.PolicyDigest = "two"
	if _, _, err = cache.resolve(context.Background(), s, "one", resolve, cloneContractInputs); err == nil {
		t.Fatal("new policy used old typed cache")
	}
}

func TestContractSurvivingAppliedCacheContinuesNativeConvergence(t *testing.T) {
	r, node, s := contractFixture(t)
	ctx := context.Background()
	remembered := 0
	s.RememberApplied = func(string) { remembered++ }
	first, _ := r.Step(ctx, s)
	if first.Pending != 1 || remembered != 1 {
		t.Fatal("initial contract policy not applied")
	}
	node.sources[r.sources[0].Name] = r.sources[0].Source
	node.nonce++
	_, _ = r.Step(ctx, s)
	s.CachedApplied = true
	r.Resolve = func(context.Context, stacksworker.Snapshot) (ContractInputs, error) {
		t.Fatal("cached contract selection read Kubernetes")
		return ContractInputs{}, nil
	}
	next, _ := r.Step(ctx, s)
	if len(node.sent) != 2 || next.Pending != 1 || next.AppliedPolicyDigest != first.AppliedPolicyDigest || remembered != 1 {
		t.Fatal("cached role failed to continue declared convergence")
	}
	node.sources[r.sources[1].Name] = r.sources[1].Source
	node.nonce++
	_, _ = r.Step(ctx, s)
	s.CachedApplied = false
	r.Resolve = func(context.Context, stacksworker.Snapshot) (ContractInputs, error) {
		return ContractInputs{}, errors.New("deployer identity changed")
	}
	got, _ := r.Step(ctx, s)
	if got.Reason != "DependenciesUnavailable" || r.inputs.value != nil {
		t.Fatal("definite contract dependency loss retained cache")
	}
	s.CachedApplied = true
	got, _ = r.Step(ctx, s)
	if got.Reason != "DependenciesUnavailable" || len(node.sent) != 2 {
		t.Fatal("revoked contract cache continued sending")
	}
}

func TestTrafficFirstRecheckUnavailablePreservesInclusionAndRecovers(t *testing.T) {
	for _, mode := range []string{"read-error", "moving-tip"} {
		for _, drain := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/drain=%v", mode, drain), func(t *testing.T) {
				r, node, s, _, now := transferFixture(t)
				ctx := context.Background()
				first, err := r.Step(ctx, s)
				if err != nil || first.Reason != "Accepted" {
					t.Fatalf("offer: %+v %v", first, err)
				}
				node.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
				node.viewErr = errors.New("canonical read unavailable")
				if mode == "moving-tip" {
					node.viewErr = nil
					node.movingView = true
				}
				*now = now.Add(time.Second)
				var transactions *api.TransactionExecutionStatus
				var traffic *api.TrafficObservation
				if drain {
					result, e := r.Drain(ctx, s)
					err = e
					transactions = result.Transactions
					traffic = result.Traffic
					if !result.Done || !result.Settled || result.Pending != 0 {
						t.Fatal("settled receipt did not drain")
					}
				} else {
					result, e := r.Step(ctx, s)
					err = e
					transactions = result.Transactions
					traffic = result.Traffic
				}
				if err != nil || traffic != nil || transactions == nil || transactions.Included != 1 || transactions.LastInclusion == nil || node.sends != 1 {
					t.Fatalf("inclusion lost or invalid observation exposed: traffic=%+v facts=%+v err=%v", traffic, transactions, err)
				}
				inclusion := transactions.LastInclusion.DeepCopy()
				node.viewErr = nil
				node.movingView = false
				s.Paused = true
				*now = now.Add(2 * time.Second)
				recovered, e := r.Step(ctx, s)
				if e != nil || recovered.Traffic == nil || !recovered.Traffic.Available || recovered.Traffic.ObservedAt.IsZero() || recovered.Transactions.Included != 1 || !reflect.DeepEqual(inclusion, recovered.Transactions.LastInclusion) || node.sends != 1 {
					t.Fatalf("recheck failed to recover independently of receipt: %+v %v", recovered, e)
				}
			})
		}
	}
}
