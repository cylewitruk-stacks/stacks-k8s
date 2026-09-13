package bitcoincontrol

import (
	"context"
	"fmt"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// stopReason distinguishes terminal disposal from reversible pause/suspension.
func stopReason(root *api.StacksNetwork, p *api.StacksNetworkParticipant) string {
	if failed(root) {
		return api.ReasonNetworkFailed
	}
	if root.DeletionTimestamp != nil {
		return api.ReasonNetworkDeleting
	}
	if root.Spec.Operation == api.NetworkOperationStopped {
		return api.ReasonNetworkStopped
	}
	if p != nil {
		for _, id := range root.Status.Identities {
			if id.Name == p.Spec.ParticipantName && (id.UID != p.UID || id.Removing) {
				return api.ReasonParticipantRemoved
			}
		}
	}
	if p == nil || p.DeletionTimestamp != nil {
		return api.ReasonParticipantRemoved
	}
	for _, entry := range root.Spec.Participants {
		if entry.Name == p.Spec.ParticipantName && entry.Kind == p.Spec.Kind {
			return ""
		}
	}
	return api.ReasonParticipantRemoved
}

// stop closes local mutation admission before bounded receipt draining and acknowledgement.
func (w *Worker) stop(ctx context.Context, record *bitcoin.BitcoinExecution) (bool, error) {
	root := &api.StacksNetwork{}
	if e := w.Reader.Get(ctx, client.ObjectKey{Namespace: record.Namespace, Name: "network"}, root); e != nil {
		return false, e
	}
	if root.UID != record.Spec.NetworkUID {
		return false, fmt.Errorf("owning network replaced")
	}
	p := &api.StacksNetworkParticipant{}
	e := w.Reader.Get(ctx, client.ObjectKey{Namespace: record.Namespace, Name: record.Spec.Participant.Name}, p)
	if apierrors.IsNotFound(e) {
		p = nil
	} else if e != nil {
		return false, e
	}
	reason := stopReason(root, p)
	if reason == "" {
		return false, nil
	}
	if terminalDrainAcknowledged(record, root) {
		w.mu.Lock()
		w.closed = true
		w.mu.Unlock()
		return true, nil
	}
	started := metav1.NewTime(w.Now().UTC())
	w.drain()
	attempt, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	current := &bitcoin.BitcoinExecution{}
	if e = w.Reader.Get(attempt, client.ObjectKeyFromObject(record), current); e != nil {
		return true, e
	}
	if current.UID != record.UID {
		return true, fmt.Errorf("execution record replaced during drain")
	}
	outcome := bitcoin.DrainDrained
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	if action := current.Status.Action; action != nil {
		outcome = bitcoin.DrainUncertain
		if action.StopReason == "" {
			action.StopReason = reason
		}
		if current.Status.Armed != nil || active {
			action.EffectUncertain = true
		}
		if action.Reorganization != nil && action.InvalidationAcknowledged && !action.CleanupAcknowledged {
			action.CleanupUnsafe = true
		}
	}
	if current.Status.Armed != nil || active {
		outcome = bitcoin.DrainUncertain
		current.Status.Phase = bitcoin.ExecutionBlocked
	}
	current.Status.Drain = &bitcoin.BitcoinDrainStatus{
		ProcessNonce:      w.ProcessNonce,
		NetworkGeneration: root.Generation,
		Reason:            reason,
		RequestedAt:       started,
		CompletedAt:       metav1.NewTime(w.Now().UTC()),
		Outcome:           outcome,
	}
	return true, w.Client.Status().Update(attempt, current)
}

// CheckDrained lets an actor stop after exact terminal-control acknowledgement.
// Uncertain permits disposal while retaining unresolved authority; it never means quiescence.
func CheckDrained(ctx context.Context, reader client.Reader, p *api.StacksNetworkParticipant) (bool, error) {
	root := &api.StacksNetwork{}
	if e := reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "network"}, root); e != nil {
		return false, e
	}
	if root.UID != p.Spec.NetworkUID {
		return false, fmt.Errorf("owning network identity unavailable")
	}
	reason := stopReason(root, p)
	if reason == "" {
		return false, nil
	}
	// Root destruction may terminate the control worker before it acknowledges
	// draining. Confirmed process termination permits actor disposal, without
	// changing pending execution facts or claiming those RPCs completed.
	if root.DeletionTimestamp != nil && metav1.IsControlledBy(p, root) {
		var current api.StacksNetworkParticipant
		if err := reader.Get(ctx, client.ObjectKeyFromObject(p), &current); err != nil {
			return false, err
		}
		control := current.Status.BitcoinControl
		if current.UID == p.UID && current.Spec.NetworkUID == root.UID && metav1.IsControlledBy(&current, root) &&
			control != nil &&
			control.Terminated &&
			control.ObservedGeneration == current.Generation &&
			control.NetworkGeneration == root.Generation {
			return true, nil
		}
	}
	if root.Status.Bitcoin == nil {
		return neverActivated(ctx, reader, p)
	}
	for _, ref := range root.Status.Bitcoin.ExecutionRefs {
		record := &bitcoin.BitcoinExecution{}
		if e := reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: ref.Name}, record); e != nil {
			return false, e
		}
		if record.UID != ref.UID {
			return false, fmt.Errorf("execution record replaced")
		}
		if record.Spec.Participant.UID != p.UID {
			continue
		}
		if record.Status.Armed == nil && record.Status.Observation == nil && record.Status.LastReceipt == nil {
			return true, nil
		}
		drain := record.Status.Drain
		if !terminalDrainAcknowledged(record, root) {
			return false, nil
		}
		return drain.Outcome == bitcoin.DrainUncertain ||
			drain.Outcome == bitcoin.DrainDrained && record.Status.Armed == nil, nil
	}
	return neverActivated(ctx, reader, p)
}

// acknowledgePause records a pause only after any local send and Armed authority settle.
func (w *Worker) acknowledgePause(ctx context.Context, record *bitcoin.BitcoinExecution) (bool, error) {
	root := &api.StacksNetwork{}
	if e := w.Reader.Get(ctx, client.ObjectKey{Namespace: record.Namespace, Name: "network"}, root); e != nil {
		return false, e
	}
	if root.UID != record.Spec.NetworkUID {
		return false, fmt.Errorf("owning network replaced")
	}
	if root.Spec.Operation != api.NetworkOperationPaused {
		return false, nil
	}
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	if active || record.Status.Armed != nil {
		return true, nil
	}
	if ack := record.Status.Control; ack != nil && ack.NetworkGeneration == root.Generation &&
		ack.Operation == bitcoin.ControlPaused &&
		ack.ProcessNonce == w.ProcessNonce &&
		!ack.HeartbeatAt.After(w.Now()) &&
		w.Now().
			Sub(ack.HeartbeatAt.Time) <
			time.Duration(
				foundation.ObservationPolicy().HeartbeatIntervalSeconds,
			)*time.Second {
		return true, nil
	}
	if ack := record.Status.Control; ack == nil || ack.NetworkGeneration != root.Generation ||
		ack.Operation != bitcoin.ControlPaused ||
		ack.ProcessNonce != w.ProcessNonce {
		record.Status.Control = &bitcoin.BitcoinControlAcknowledgement{
			NetworkGeneration: root.Generation,
			ProcessNonce:      w.ProcessNonce,
			Operation:         bitcoin.ControlPaused,
			ObservedAt:        metav1.NewTime(w.Now().UTC()),
		}
	}
	record.Status.Control.HeartbeatAt = metav1.NewTime(w.Now().UTC())
	return true, w.Client.Status().Update(ctx, record)
}

// neverActivated checks the worker's activation precondition and current support object.
// An actor may already be running while its control worker has never been created.
func neverActivated(ctx context.Context, reader client.Reader, p *api.StacksNetworkParticipant) (bool, error) {
	if control := p.Status.BitcoinControl; control != nil && (control.DeploymentRef != nil || len(control.Pods) > 0) {
		return false, nil
	}
	name := naming.RuntimeName(
		string(p.Spec.NetworkUID),
		string(p.UID),
		string(api.ParticipantBitcoinNode),
		p.Spec.ParticipantName,
		"control",
	)
	deployment := &appsv1.Deployment{}
	e := reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: name}, deployment)
	if apierrors.IsNotFound(e) {
		return true, nil
	}
	if e != nil {
		return false, e
	}
	return false, nil
}

// terminalDrainAcknowledged survives unrelated root revisions once sending was closed.
func terminalDrainAcknowledged(record *bitcoin.BitcoinExecution, root *api.StacksNetwork) bool {
	d := record.Status.Drain
	if d == nil || d.ProcessNonce == "" || d.NetworkGeneration > root.Generation {
		return false
	}
	switch d.Reason {
	case api.ReasonParticipantRemoved, api.ReasonNetworkStopped, api.ReasonNetworkDeleting, api.ReasonNetworkFailed:
		return d.Outcome == bitcoin.DrainDrained || d.Outcome == bitcoin.DrainUncertain
	}
	return false
}
