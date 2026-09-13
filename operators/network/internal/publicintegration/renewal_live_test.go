//go:build live

package publicintegration

import (
	"context"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// renewalEvidence keeps native lock coverage distinct from transaction attribution.
type renewalEvidence struct {
	Participant  identity                       `json:"participant"`
	Worker       workerIdentity                 `json:"worker"`
	Observation  api.PoX5EnrollmentObservation  `json:"observation"`
	Transactions api.TransactionExecutionStatus `json:"transactions"`
}

// renewalCohort accepts only fresh observations from currently bound, active stacker processes.
func renewalCohort(s snapshot) (map[types.UID]renewalEvidence, bool) {
	if s.Status.ObservationPolicy == nil {
		return nil, false
	}
	freshness := time.Duration(3*s.Status.ObservationPolicy.PollIntervalSeconds+s.Status.ObservationPolicy.RPCAllowanceSeconds) * time.Second
	fresh := func(at metav1.Time) bool { return !at.IsZero() && !at.After(s.At) && s.At.Sub(at.Time) <= freshness }
	out := map[types.UID]renewalEvidence{}
	for _, p := range s.Participants {
		if p.Kind != "StacksStacker" {
			continue
		}
		e := p.Status.Execution
		if p.Identity.UID == "" || e == nil || e.Phase != "Active" || e.Pending != 0 || e.ObservedGeneration != p.Identity.Generation || e.NetworkGeneration != s.Root.Generation || !fresh(e.ObservedAt) || e.PoX5 == nil || e.Transactions == nil || p.Status.Admission == nil || e.AppliedPolicyDigest != p.Status.Admission.PolicyDigest {
			return nil, false
		}
		observed := e.PoX5
		if !fresh(observed.ObservedAt) || !observed.TargetCycleMatched || observed.Holder == "" || observed.Manager == "" || observed.SignerPublicKey == "" || observed.ManagerSourceDigest == "" || observed.AmountMicroSTX == "" || observed.AmountMicroSTX != observed.DelegatedAmountMicroSTX || observed.FirstCycle >= observed.EndCycleExclusive || observed.TargetCycle < observed.FirstCycle || observed.TargetCycle >= observed.EndCycleExclusive || observed.UnlockHeight <= observed.BurnHeight || observed.StacksTip == "" {
			return nil, false
		}
		bound := false
		for _, id := range s.Status.Identities {
			if id.UID == p.Identity.UID && !id.Removing && id.Worker != nil && id.Worker.Shutdown == nil && id.Worker.Disposal == nil && id.Worker.Pod.UID == e.PodUID && id.Worker.ProfileDigest == e.ProfileDigest && e.ProcessNonce != "" {
				bound = true
				break
			}
		}
		if !bound {
			return nil, false
		}
		out[p.Identity.UID] = renewalEvidence{Participant: p.Identity, Worker: workerIdentity{UID: e.PodUID, ProcessNonce: e.ProcessNonce}, Observation: *observed, Transactions: *e.Transactions.DeepCopy()}
	}
	return out, len(out) > 0
}

// renewalAdvanced requires continuous lock extension and settled worker activity for every original holder.
func renewalAdvanced(before, after map[types.UID]renewalEvidence) bool {
	if len(before) == 0 || len(before) != len(after) {
		return false
	}
	for uid, old := range before {
		next, ok := after[uid]
		if !ok || old.Worker != next.Worker || old.Participant.UID != next.Participant.UID {
			return false
		}
		a, b := old.Observation, next.Observation
		if a.Holder != b.Holder || a.Manager != b.Manager || a.ManagerSourceDigest != b.ManagerSourceDigest || a.SignerPublicKey != b.SignerPublicKey || a.AmountMicroSTX != b.AmountMicroSTX || a.FirstCycle != b.FirstCycle || b.EndCycleExclusive <= a.EndCycleExclusive || b.UnlockHeight <= a.UnlockHeight || b.BurnHeight <= a.BurnHeight || !b.ObservedAt.After(a.ObservedAt.Time) || b.StacksTip == a.StacksTip {
			return false
		}
		if next.Transactions.Offered <= old.Transactions.Offered || (next.Transactions.Included <= old.Transactions.Included && next.Transactions.PostconditionObserved <= old.Transactions.PostconditionObserved) {
			return false
		}
	}
	return true
}

// awaitRenewalBaseline waits for settled, fresh cohort observations before measuring extension.
func (h *harness) awaitRenewalBaseline(ctx context.Context) (snapshot, error) {
	expected := 2
	if h.config.variant == "full30" {
		expected = 6
	}
	return h.wait(ctx, "renewal-cohort-ready", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		cohort, ready := renewalCohort(s)
		return condition(s, "Operational", metav1.ConditionTrue) && ready && len(cohort) == expected, nil
	})
}

// qualifyRenewal waits for actual maintained coverage without altering cadence or issuing transactions.
func (h *harness) qualifyRenewal(ctx context.Context, baseline snapshot) (snapshot, error) {
	workers := workerIdentities(baseline)
	baseline, err := h.awaitRenewalBaseline(ctx)
	if err != nil {
		return baseline, err
	}
	if err = h.verifyWorkers(ctx, workers, baseline); err != nil {
		return baseline, err
	}
	old, _ := renewalCohort(baseline)
	if err := h.event("renewal-baseline", old); err != nil {
		return baseline, err
	}
	renewed, err := h.wait(ctx, "pox5-renewal", h.config.timeout, true, func(s snapshot) (bool, error) {
		current, ready := renewalCohort(s)
		return condition(s, "Operational", metav1.ConditionTrue) && ready && renewalAdvanced(old, current), nil
	})
	if err != nil {
		return renewed, err
	}
	next, _ := renewalCohort(renewed)
	if err = h.verifyWorkers(ctx, workerIdentities(baseline), renewed); err != nil {
		return renewed, err
	}
	if err = h.event("pox5-renewal-observed", map[string]any{"before": old, "after": next}); err != nil {
		return renewed, err
	}
	return h.awaitProgress(ctx, "post-renewal-progress", renewed)
}
