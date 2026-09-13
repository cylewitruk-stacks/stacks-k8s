//go:build live

package publicintegration

import (
	"context"
	"fmt"
	"reflect"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// scheduleTrial pins the public baseline, effective timing and optional override being exercised.
type scheduleTrial struct {
	producer  types.UID
	baseline  *bitcoin.BitcoinBlockSchedule
	effective bitcoin.BitcoinBlockScheduleSpec
	targets   []bitcoin.ProductionTarget
	override  types.UID
	after     time.Time
}

// scheduleMatches requires current admitted intent and its effective domain projection to agree.
func scheduleMatches(s snapshot, t scheduleTrial) (*participantEvidence, bool) {
	if s.Status.ObservationPolicy == nil {
		return nil, false
	}
	freshness := time.Duration(3*s.Status.ObservationPolicy.PollIntervalSeconds+s.Status.ObservationPolicy.RPCAllowanceSeconds) * time.Second
	for i := range s.Participants {
		p := &s.Participants[i]
		if p.Identity.UID != t.producer {
			continue
		}
		a, v := p.Status.Admission, p.Status.Scheduling
		if a == nil || a.Configuration.BitcoinBlockProduction == nil || v == nil || v.PolicyDigest != a.PolicyDigest || v.ObservedAt == nil || v.ObservedAt.After(s.At) || s.At.Sub(v.ObservedAt.Time) > freshness {
			return nil, false
		}
		policy := a.Configuration.BitcoinBlockProduction
		if policy.Schedule == nil || !reflect.DeepEqual(*policy.Schedule, t.baseline.Spec) || policy.Targets == nil || !reflect.DeepEqual(*policy.Targets, t.targets) || v.Schedule == nil || !reflect.DeepEqual(*v.Schedule, t.effective) || v.ScheduleRef == nil || v.ScheduleRef.UID != t.baseline.UID {
			return nil, false
		}
		if (t.override == "") != (v.Override == nil) || t.override != "" && v.Override.Override.UID != t.override {
			return nil, false
		}
		if len(v.Targets) != len(t.targets) {
			return nil, false
		}
		for _, desired := range t.targets {
			found := false
			for _, selected := range v.Targets {
				for _, node := range s.Participants {
					if node.Kind == "BitcoinNode" && node.Name == desired.NodeRef.Name && node.Identity.UID == selected.Participant.UID && node.Identity.Name == selected.Participant.Name && selected.Weight == desired.Weight {
						found = true
					}
				}
			}
			if !found {
				return nil, false
			}
		}
		return p, true
	}
	return nil, false
}

// scheduleReceipt accepts only exact current-process baseline receipts carrying the tested authority.
func scheduleReceipt(receipt *bitcoin.BitcoinRPCReceipt, view *bitcoin.BitcoinObservation, p *participantEvidence, t scheduleTrial) bool {
	if receipt == nil || view == nil || receipt.Request.Method != "Generate" || receipt.Request.Action != nil || receipt.Request.ID == "" || receipt.BlockHash == "" || !receipt.ReceivedAt.After(t.after) || receipt.ReceivedAt.After(view.ObservedAt.Time) || receipt.Request.Target != view.Target {
		return false
	}
	offer := receipt.Request.Offer
	if offer == nil || offer.Mode != "Baseline" || offer.Production.UID != t.producer || offer.PolicyDigest != p.Status.Scheduling.PolicyDigest || offer.Initialization != p.Status.Scheduling.Initialization || offer.Target == nil || *offer.Target != view.Target.Participant || view.Height <= offer.ExpectedHeight {
		return false
	}
	return t.override == "" && offer.Override == nil || t.override != "" && offer.Override != nil && offer.Override.UID == t.override
}

// awaitSchedule collects distinct receipts from every selected target without asserting statistical fairness.
func (h *harness) awaitSchedule(ctx context.Context, name string, t scheduleTrial) (snapshot, error) {
	receipts := map[string]bitcoin.BitcoinRPCReceipt{}
	nodes := map[types.UID]bool{}
	result, err := h.wait(ctx, name, h.config.progressTimeout, true, func(s snapshot) (bool, error) {
		p, ready := scheduleMatches(s, t)
		if !ready {
			return false, nil
		}
		for _, node := range s.Participants {
			if node.Kind != "BitcoinNode" {
				continue
			}
			selected := false
			for _, target := range p.Status.Scheduling.Targets {
				selected = selected || target.Participant.UID == node.Identity.UID
			}
			if !selected {
				continue
			}
			for _, record := range s.Executions {
				if record.ParticipantUID != node.Identity.UID {
					continue
				}
				view, e := (actionSelection{Participant: node, Execution: record.Identity}).observe(s)
				if e == nil && scheduleReceipt(record.Status.LastReceipt, view, p, t) {
					receipt := *record.Status.LastReceipt.DeepCopy()
					receipts[receipt.Request.ID] = receipt
					nodes[node.Identity.UID] = true
				}
			}
		}
		return len(receipts) >= 4 && len(nodes) == len(t.targets), nil
	})
	if err != nil {
		return result, err
	}
	err = h.event(name+"-receipts", receipts)
	return result, err
}

// scheduleDefinition creates a single immutable timing declaration with an acknowledged UID.
func (h *harness) scheduleDefinition(ctx context.Context, name string, cadence bitcoin.Cadence) (*bitcoin.BitcoinBlockSchedule, error) {
	object := &bitcoin.BitcoinBlockSchedule{TypeMeta: metav1.TypeMeta{APIVersion: bitcoin.GroupVersion.String(), Kind: "BitcoinBlockSchedule"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.config.namespace}, Spec: bitcoin.BitcoinBlockScheduleSpec{Cadence: cadence}}
	if err := h.c.Create(ctx, object); err != nil {
		return nil, err
	}
	return object, nil
}

// changeSchedule changes only the exact fixture's reusable producer, preserving frozen payout/initialization.
func (h *harness) changeSchedule(ctx context.Context, original *bitcoin.BitcoinBlockProduction, schedule *bitcoin.BitcoinBlockSchedule, targets []bitcoin.ProductionTarget) error {
	var root api.StacksNetwork
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: "network"}, &root); err != nil {
		return err
	}
	if root.UID != h.rootUID || root.DeletionTimestamp != nil || root.Spec.Operation != "Running" {
		return fmt.Errorf("schedule qualification root unavailable")
	}
	current := &bitcoin.BitcoinBlockProduction{}
	if err := h.c.Get(ctx, client.ObjectKeyFromObject(original), current); err != nil {
		return err
	}
	if current.UID != original.UID || current.DeletionTimestamp != nil {
		return fmt.Errorf("producer definition replaced")
	}
	base := current.DeepCopy()
	current.Spec = *original.Spec.DeepCopy()
	if schedule != nil {
		current.Spec.Schedule = nil
		current.Spec.ScheduleRef = &common.NameRef{Name: schedule.Name}
		current.Spec.Targets = ptr.To(append([]bitcoin.ProductionTarget(nil), targets...))
	}
	return h.c.Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// qualifySchedules exercises baseline timing/selection and override expiry/cancellation through public CRs.
func (h *harness) qualifySchedules(ctx context.Context, before snapshot) (snapshot, error) {
	original, originalSchedule, producer, err := h.scheduleFixture(ctx, before)
	if err != nil {
		return before, err
	}
	fixed, err := h.scheduleDefinition(ctx, "qualify-fixed", bitcoin.Cadence{Mode: "Fixed", Interval: ptr.To(common.Duration("5s"))})
	if err != nil {
		return before, err
	}
	uniform, err := h.scheduleDefinition(ctx, "qualify-uniform", bitcoin.Cadence{Mode: "Uniform", MinimumInterval: ptr.To(common.Duration("5s")), MaximumInterval: ptr.To(common.Duration("7s"))})
	if err != nil {
		return before, err
	}
	latest, err := h.scheduleDefinition(ctx, "qualify-latest", bitcoin.Cadence{Mode: "Fixed", Interval: ptr.To(common.Duration("7s"))})
	if err != nil {
		return before, err
	}
	targets := []bitcoin.ProductionTarget{{NodeRef: common.NameRef{Name: "btc-01"}, Weight: 1}, {NodeRef: common.NameRef{Name: "btc-02"}, Weight: 2}}
	for _, tc := range []struct {
		name     string
		schedule *bitcoin.BitcoinBlockSchedule
		targets  []bitcoin.ProductionTarget
	}{{"fixed-single-target", fixed, targets[:1]}, {"uniform-weighted-targets", uniform, targets}} {
		trial := scheduleTrial{producer: producer, baseline: tc.schedule, effective: tc.schedule.Spec, targets: tc.targets, after: time.Now()}
		if err = h.changeSchedule(ctx, original, tc.schedule, tc.targets); err != nil {
			return before, err
		}
		before, err = h.awaitSchedule(ctx, tc.name, trial)
		if err != nil {
			return before, err
		}
	}
	for _, cancel := range []bool{false, true} {
		name, duration := "qualify-override-expiry", common.Duration("180s")
		if cancel {
			name, duration = "qualify-override-cancel", "5m"
		}
		request := &bitcoin.BitcoinBlockScheduleOverride{TypeMeta: metav1.TypeMeta{APIVersion: bitcoin.GroupVersion.String(), Kind: "BitcoinBlockScheduleOverride"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.config.namespace}, Spec: bitcoin.BitcoinBlockScheduleOverrideSpec{NetworkUID: h.rootUID, ProductionRef: common.NameRef{Name: "blocks"}, Schedule: fixed.Spec.DeepCopy(), Duration: duration}}
		if err = h.c.Create(ctx, request); err != nil {
			return before, err
		}
		baseline := uniform
		if cancel {
			baseline = latest
		}
		trial := scheduleTrial{producer: producer, baseline: baseline, effective: fixed.Spec, targets: targets, override: request.UID, after: time.Now()}
		before, err = h.awaitSchedule(ctx, name+"-active", trial)
		if err != nil {
			return before, err
		}
		if !cancel {
			if err = h.changeSchedule(ctx, original, latest, targets); err != nil {
				return before, err
			}
			trial.baseline = latest
			trial.after = time.Now()
			before, err = h.awaitSchedule(ctx, name+"-baseline-updated", trial)
			if err != nil {
				return before, err
			}
		} else {
			uid := request.UID
			if err = h.c.Delete(ctx, request, client.Preconditions{UID: &uid}); err != nil {
				return before, err
			}
		}
		before, err = h.wait(ctx, name+"-withdrawn", h.config.progressTimeout, true, func(s snapshot) (bool, error) {
			if cancel {
				var current bitcoin.BitcoinBlockScheduleOverride
				e := h.c.Get(ctx, client.ObjectKeyFromObject(request), &current)
				if client.IgnoreNotFound(e) != nil {
					return false, e
				}
				if e == nil {
					return false, nil
				}
			} else {
				uid := request.UID
				if e := h.c.Get(ctx, client.ObjectKeyFromObject(request), request); e != nil {
					return false, e
				}
				if request.UID != uid {
					return false, fmt.Errorf("override replaced")
				}
				if request.Status.Phase != "Completed" {
					return false, nil
				}
			}
			trial.override = ""
			trial.effective = latest.Spec
			_, ready := scheduleMatches(s, trial)
			return ready, nil
		})
		if err != nil {
			return before, err
		}
		trial.after = time.Now()
		before, err = h.awaitSchedule(ctx, name+"-latest-baseline", trial)
		if err != nil {
			return before, err
		}
	}
	if err = h.changeSchedule(ctx, original, nil, nil); err != nil {
		return before, err
	}
	trial := scheduleTrial{producer: producer, baseline: originalSchedule, effective: originalSchedule.Spec, targets: ptr.Deref(original.Spec.Targets, nil), after: time.Now()}
	before, err = h.awaitSchedule(ctx, "original-baseline-restored", trial)
	if err != nil {
		return before, err
	}
	return h.awaitProgress(ctx, "post-schedule-progress", before)
}

// scheduleFixture resolves the sole referenced producer and its current reusable schedule.
func (h *harness) scheduleFixture(ctx context.Context, before snapshot) (*bitcoin.BitcoinBlockProduction, *bitcoin.BitcoinBlockSchedule, types.UID, error) {
	var root api.StacksNetwork
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: "network"}, &root); err != nil {
		return nil, nil, "", err
	}
	if root.UID != h.rootUID {
		return nil, nil, "", fmt.Errorf("schedule root changed")
	}
	var source string
	var producer types.UID
	for _, entry := range root.Spec.Participants {
		if entry.Kind != "BitcoinBlockProduction" {
			continue
		}
		if source != "" || entry.Definition.Ref == nil || entry.Overrides != nil {
			return nil, nil, "", fmt.Errorf("schedule qualification requires one referenced producer without overrides")
		}
		source = entry.Definition.Ref.Name
		for _, p := range before.Participants {
			if p.Name == entry.Name {
				producer = p.Identity.UID
			}
		}
	}
	if source == "" || producer == "" {
		return nil, nil, "", fmt.Errorf("producer unavailable")
	}
	original := &bitcoin.BitcoinBlockProduction{}
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: source}, original); err != nil {
		return nil, nil, "", err
	}
	if original.Spec.ScheduleRef == nil {
		return nil, nil, "", fmt.Errorf("fixture baseline schedule reference required")
	}
	originalSchedule := &bitcoin.BitcoinBlockSchedule{}
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: original.Spec.ScheduleRef.Name}, originalSchedule); err != nil {
		return nil, nil, "", err
	}
	return original, originalSchedule, producer, nil
}
