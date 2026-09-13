//go:build live

package publicintegration

import (
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestScheduleQualificationReceiptRejectsDifferentAuthorityAndOldEvidence(t *testing.T) {
	at := time.Unix(1000, 0)
	trial := scheduleTrial{producer: "production", override: "override", after: at}
	policy := &participantEvidence{Status: api.ParticipantStatus{Scheduling: &bitcoin.BitcoinSchedulingStatus{PolicyDigest: "policy", Initialization: common.Binding{UID: "initialization"}}}}
	view := &bitcoin.BitcoinObservation{Height: 301, ObservedAt: metav1.NewTime(at.Add(2 * time.Second)), Target: bitcoin.BitcoinTargetIdentity{Participant: common.Binding{UID: "actor"}, ContainerID: "container"}}
	original := &bitcoin.BitcoinRPCReceipt{Request: bitcoin.BitcoinArmedRPC{ID: "dispatch", Method: "Generate", Target: view.Target, Offer: &bitcoin.BitcoinBlockOffer{Mode: "Baseline", Production: common.Binding{UID: trial.producer}, Initialization: policy.Status.Scheduling.Initialization, PolicyDigest: "policy", Target: &view.Target.Participant, Override: &common.Binding{UID: trial.override}, ExpectedHeight: 300}}, ReceivedAt: metav1.NewTime(at.Add(time.Second)), BlockHash: "block"}
	if !scheduleReceipt(original, view, policy, trial) {
		t.Fatal("valid current baseline receipt rejected")
	}
	plain := original.DeepCopy()
	plain.Request.Offer.Override = nil
	baseline := trial
	baseline.override = ""
	if !scheduleReceipt(plain, view, policy, baseline) {
		t.Fatal("ordinary baseline receipt rejected")
	}
	for name, change := range map[string]func(*bitcoin.BitcoinRPCReceipt){
		"old receipt":            func(r *bitcoin.BitcoinRPCReceipt) { r.ReceivedAt = metav1.NewTime(at) },
		"later than native view": func(r *bitcoin.BitcoinRPCReceipt) { r.ReceivedAt = metav1.NewTime(at.Add(3 * time.Second)) },
		"action receipt":         func(r *bitcoin.BitcoinRPCReceipt) { r.Request.Action = &common.Binding{UID: "action"} },
		"different process":      func(r *bitcoin.BitcoinRPCReceipt) { r.Request.Target.ContainerID = "other" },
		"missing offer":          func(r *bitcoin.BitcoinRPCReceipt) { r.Request.Offer = nil },
		"bootstrap receipt":      func(r *bitcoin.BitcoinRPCReceipt) { r.Request.Offer.Mode = "Bootstrap" },
		"old producer":           func(r *bitcoin.BitcoinRPCReceipt) { r.Request.Offer.Production.UID = "other" },
		"old policy":             func(r *bitcoin.BitcoinRPCReceipt) { r.Request.Offer.PolicyDigest = "other" },
		"other initialization":   func(r *bitcoin.BitcoinRPCReceipt) { r.Request.Offer.Initialization.UID = "other" },
		"other target":           func(r *bitcoin.BitcoinRPCReceipt) { r.Request.Offer.Target.UID = "other" },
		"different override":     func(r *bitcoin.BitcoinRPCReceipt) { r.Request.Offer.Override.UID = "other" },
		"ordinary baseline":      func(r *bitcoin.BitcoinRPCReceipt) { r.Request.Offer.Override = nil },
		"no observed advance":    func(r *bitcoin.BitcoinRPCReceipt) { r.Request.Offer.ExpectedHeight = view.Height },
	} {
		t.Run(name, func(t *testing.T) {
			r := original.DeepCopy()
			change(r)
			if scheduleReceipt(r, view, policy, trial) {
				t.Fatal("wrong authority/evidence accepted")
			}
		})
	}
}

func TestScheduleQualificationMatchesResolvedBaselineAndTemporaryTiming(t *testing.T) {
	now := time.Unix(1000, 0)
	baseline := &bitcoin.BitcoinBlockSchedule{ObjectMeta: metav1.ObjectMeta{Name: "baseline", UID: "schedule"}, Spec: bitcoin.BitcoinBlockScheduleSpec{Cadence: bitcoin.Cadence{Mode: "Fixed", Interval: ptr.To(common.Duration("7s"))}}}
	effective := bitcoin.BitcoinBlockScheduleSpec{Cadence: bitcoin.Cadence{Mode: "Fixed", Interval: ptr.To(common.Duration("5s"))}}
	targets := []bitcoin.ProductionTarget{{NodeRef: common.NameRef{Name: "node"}, Weight: 2}}
	trial := scheduleTrial{producer: "production", baseline: baseline, effective: effective, targets: targets, override: "override"}
	policy := foundation.ObservationPolicy()
	fresh := func() snapshot {
		return snapshot{At: now, Status: api.StacksNetworkStatus{ObservationPolicy: &policy}, Participants: []participantEvidence{{Identity: identity{Name: "node-resource", UID: "node-uid"}, Name: "node", Kind: "BitcoinNode"}, {Identity: identity{UID: "production"}, Kind: "BitcoinBlockProduction", Status: api.ParticipantStatus{Admission: &api.Admission{PolicyDigest: "policy", Configuration: api.Configuration{BitcoinBlockProduction: &bitcoin.BitcoinBlockProductionSpec{Schedule: baseline.Spec.DeepCopy(), Targets: ptr.To(targets)}}}, Scheduling: &bitcoin.BitcoinSchedulingStatus{PolicyDigest: "policy", ObservedAt: ptr.To(metav1.NewTime(now)), Schedule: effective.DeepCopy(), ScheduleRef: &common.Binding{UID: baseline.UID}, Targets: []bitcoin.BitcoinScheduledTarget{{Participant: common.Binding{Name: "node-resource", UID: "node-uid"}, Weight: 2}}, Override: &bitcoin.BitcoinActiveOverride{Override: common.Binding{UID: "override"}}}}}}}
	}
	if _, ok := scheduleMatches(fresh(), trial); !ok {
		t.Fatal("resolved baseline/effective override rejected")
	}
	for name, change := range map[string]func(*snapshot){
		"stale status": func(s *snapshot) {
			s.Participants[1].Status.Scheduling.ObservedAt = ptr.To(metav1.NewTime(now.Add(-17 * time.Second)))
		},
		"old baseline": func(s *snapshot) {
			s.Participants[1].Status.Admission.Configuration.BitcoinBlockProduction.Schedule = effective.DeepCopy()
		},
		"baseline used during override": func(s *snapshot) { s.Participants[1].Status.Scheduling.Schedule = baseline.Spec.DeepCopy() },
		"replacement schedule":          func(s *snapshot) { s.Participants[1].Status.Scheduling.ScheduleRef.UID = "other" },
		"replacement target":            func(s *snapshot) { s.Participants[0].Identity.UID = "other" },
		"wrong weight":                  func(s *snapshot) { s.Participants[1].Status.Scheduling.Targets[0].Weight = 1 },
		"old policy projection":         func(s *snapshot) { s.Participants[1].Status.Scheduling.PolicyDigest = "other" },
		"missing override":              func(s *snapshot) { s.Participants[1].Status.Scheduling.Override = nil },
	} {
		t.Run(name, func(t *testing.T) {
			s := fresh()
			change(&s)
			if _, ok := scheduleMatches(s, trial); ok {
				t.Fatal("mismatched scheduling authority accepted")
			}
		})
	}
}
