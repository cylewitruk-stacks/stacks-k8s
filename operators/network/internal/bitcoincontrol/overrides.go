package bitcoincontrol

import (
	"context"
	"fmt"
	"sort"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// OverrideFinalizer retains a request until its scheduler activation is withdrawn.
const OverrideFinalizer = "bitcoin.stacks.org/schedule-override"

// OverrideFieldManager owns only schedule override lifecycle status.
const OverrideFieldManager = "stacks-bitcoin-schedule-override"

// reconcileOverride serializes activation through the existing initialization status CAS.
func (s *Scheduler) reconcileOverride(ctx context.Context, root *api.StacksNetwork, record *bitcoin.BitcoinInitialization) (bool, error) {
	if active := record.Status.Override; active != nil {
		request := &bitcoin.BitcoinBlockScheduleOverride{}
		err := s.Reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: active.Override.Name}, request)
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		current := err == nil && request.UID == active.Override.UID
		production, productionErr := s.currentProduction(ctx, root, record)
		valid := current && request.DeletionTimestamp == nil && !overrideTerminal(request.Status.Phase) && root.DeletionTimestamp == nil && !failed(root) && root.Spec.Operation != api.NetworkOperationStopped && productionErr == nil && objectref.Participant(production) == active.Production && s.Now().Before(active.ExpiresAt.Time)
		if valid {
			schedule, pin, err := s.overrideSchedule(ctx, root.Namespace, request)
			valid = err == nil && equality.Semantic.DeepEqual(schedule, &active.Schedule) && equality.Semantic.DeepEqual(pin, active.ScheduleRef)
		}
		if valid {
			return false, nil
		}
		// Preserve activation through a lost status publication before withdrawing its slot.
		if current && !equality.Semantic.DeepEqual(request.Status.Admission, active) {
			return true, nil
		}
		// Let the lifecycle writer retain the terminal outcome before expiry's
		// activation disappears. Dispatch already rejects an expired override.
		if current && !s.Now().Before(active.ExpiresAt.Time) && !overrideTerminal(request.Status.Phase) {
			return true, nil
		}
		record.Status.Override = nil
		s.withdrawTiming(record)
		return true, s.Client.Status().Update(ctx, record)
	}
	if root.DeletionTimestamp != nil || failed(root) || root.Spec.Operation == api.NetworkOperationStopped {
		return false, nil
	}
	production, err := s.currentProduction(ctx, root, record)
	if err != nil {
		return false, nil
	}
	var requests bitcoin.BitcoinBlockScheduleOverrideList
	if err := s.Reader.List(ctx, &requests, client.InNamespace(root.Namespace), client.Limit(1001)); err != nil {
		return false, err
	}
	if len(requests.Items) > 1000 || requests.Continue != "" {
		return false, fmt.Errorf("override inventory incomplete")
	}
	sort.Slice(requests.Items, func(i, j int) bool {
		a, b := &requests.Items[i], &requests.Items[j]
		if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
			return a.CreationTimestamp.Before(&b.CreationTimestamp)
		}
		return a.UID < b.UID
	})
	for i := range requests.Items {
		request := &requests.Items[i]
		if request.Spec.NetworkUID != root.UID || request.DeletionTimestamp != nil || overrideTerminal(request.Status.Phase) || request.Status.Admission != nil || !s.Now().Before(request.CreationTimestamp.Add(5*time.Minute)) || request.Spec.ProductionRef.Name != production.Spec.ParticipantName || !controllerutil.ContainsFinalizer(request, OverrideFinalizer) {
			continue
		}
		schedule, pin, err := s.overrideSchedule(ctx, root.Namespace, request)
		if err != nil {
			continue
		}
		duration, err := time.ParseDuration(string(request.Spec.Duration))
		if err != nil || duration <= 0 || duration > time.Hour {
			continue
		}
		now := metav1.NewTime(s.Now().UTC())
		record.Status.Override = &bitcoin.BitcoinActiveOverride{Override: objectref.BitcoinScheduleOverride(request), Production: objectref.Participant(production), Schedule: *schedule, ScheduleRef: pin, StartedAt: now, ExpiresAt: metav1.NewTime(now.Add(duration))}
		s.withdrawTiming(record)
		return true, s.Client.Status().Update(ctx, record)
	}
	return false, nil
}

// overrideSchedule validates public immutable timing without accessing credentials.
func (s *Scheduler) overrideSchedule(ctx context.Context, namespace string, request *bitcoin.BitcoinBlockScheduleOverride) (*bitcoin.BitcoinBlockScheduleSpec, *common.Binding, error) {
	if (request.Spec.Schedule == nil) == (request.Spec.ScheduleRef == nil) {
		return nil, nil, fmt.Errorf("exactly one schedule required")
	}
	schedule := request.Spec.Schedule.DeepCopy()
	var pin *common.Binding
	if request.Spec.ScheduleRef != nil {
		var object bitcoin.BitcoinBlockSchedule
		if err := s.Reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: request.Spec.ScheduleRef.Name}, &object); err != nil {
			return nil, nil, err
		}
		if object.UID == "" || object.DeletionTimestamp != nil {
			return nil, nil, fmt.Errorf("schedule identity unavailable")
		}
		schedule = object.Spec.DeepCopy()
		value := objectref.BitcoinBlockSchedule(&object)
		value.Fingerprint = foundation.Digest(object.Spec)
		pin = &value
	}
	if _, _, err := cadenceBounds(schedule); err != nil {
		return nil, nil, err
	}
	return schedule, pin, nil
}

// overrideTerminal distinguishes closed requests from activation candidates.
func overrideTerminal(phase bitcoin.OverridePhase) bool {
	return phase == bitcoin.OverrideCompleted || phase == bitcoin.OverrideExpired || phase == bitcoin.OverrideCancelled
}

// effectiveSchedule keeps temporary timing separate from the latest complete baseline admission.
func effectiveSchedule(record *bitcoin.BitcoinInitialization, baseline *bitcoin.BitcoinBlockScheduleSpec) *bitcoin.BitcoinBlockScheduleSpec {
	if record.Status.Override != nil {
		return record.Status.Override.Schedule.DeepCopy()
	}
	return baseline
}

// withdrawTiming invalidates an unsent selection before reanchoring changed timing.
func (s *Scheduler) withdrawTiming(record *bitcoin.BitcoinInitialization) {
	if record.Status.Baseline != nil {
		s.withdrawBaseline(record)
	} else {
		record.Status.NextOpportunityAt = nil
		record.Status.Offer = nil
	}
}

// overrideBinding stamps the exact timing authority onto a newly committed opportunity.
func overrideBinding(record *bitcoin.BitcoinInitialization) *common.Binding {
	if record.Status.Override == nil {
		return nil
	}
	return record.Status.Override.Override.DeepCopy()
}

// authorizeTimingOverride rejects expired, cancelled or replaced temporary authority before send.
func (w *Worker) authorizeTimingOverride(ctx context.Context, a admitted, offer *bitcoin.BitcoinBlockOffer) error {
	active := a.initialization.Status.Override
	if active == nil {
		if offer.Override != nil {
			return fmt.Errorf("override activation withdrawn")
		}
		return nil
	}
	if offer.Override == nil || *offer.Override != active.Override || active.Production != offer.Production || !w.Now().Before(active.ExpiresAt.Time) {
		return fmt.Errorf("override activation differs or expired")
	}
	var request bitcoin.BitcoinBlockScheduleOverride
	if err := w.Reader.Get(ctx, client.ObjectKey{Namespace: a.root.Namespace, Name: active.Override.Name}, &request); err != nil {
		return err
	}
	if request.UID != active.Override.UID || request.Spec.NetworkUID != a.root.UID || request.DeletionTimestamp != nil || overrideTerminal(request.Status.Phase) || !controllerutil.ContainsFinalizer(&request, OverrideFinalizer) {
		return fmt.Errorf("override request unavailable")
	}
	scheduler := Scheduler{Reader: w.Reader}
	schedule, pin, err := scheduler.overrideSchedule(ctx, a.root.Namespace, &request)
	if err != nil || !equality.Semantic.DeepEqual(schedule, &active.Schedule) || !equality.Semantic.DeepEqual(pin, active.ScheduleRef) {
		return fmt.Errorf("override schedule identity differs")
	}
	return nil
}

// ValidateSchedulingOverride checks that projected timing still matches the retained activation.
// A nil projection also requires no currently active override; stale cancellation cannot remain operational.
func ValidateSchedulingOverride(ctx context.Context, reader client.Reader, root *api.StacksNetwork, production *api.StacksNetworkParticipant, scheduling *bitcoin.BitcoinSchedulingStatus, now time.Time) error {
	if scheduling == nil || root.Status.Bitcoin == nil || root.Status.Bitcoin.InitializationRef == nil {
		return fmt.Errorf("scheduler identity unavailable")
	}
	ref := root.Status.Bitcoin.InitializationRef
	var initial bitcoin.BitcoinInitialization
	if err := reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: ref.Name}, &initial); err != nil {
		return err
	}
	if initial.UID != ref.UID || initial.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(&initial, root) || initial.DeletionTimestamp != nil || !equality.Semantic.DeepEqual(initial.Status.Override, scheduling.Override) {
		return fmt.Errorf("effective timing projection differs")
	}
	worker := Worker{Reader: reader, Now: func() time.Time { return now }}
	return worker.authorizeTimingOverride(ctx, admitted{root: root, initialization: &initial}, &bitcoin.BitcoinBlockOffer{Production: objectref.Participant(production), Override: overrideBinding(&initial)})
}
