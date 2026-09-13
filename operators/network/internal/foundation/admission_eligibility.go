package foundation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateAdmissionSource verifies retained provenance, permitting newer rejected source generations.
func ValidateAdmissionSource(ctx context.Context, reader client.Reader, p *api.StacksNetworkParticipant) error {
	if p.Status.Admission == nil {
		return fmt.Errorf("participant has no retained admission")
	}
	source := p.Status.Admission.Source
	if source.Name == "" {
		if source.UID != "" || (source.Generation == 0 && p.Spec.Source.Name != "") {
			return fmt.Errorf("retained source provenance unavailable")
		}
		return nil
	}
	if source.UID == "" {
		return fmt.Errorf("retained definition UID unavailable")
	}
	definition := DefinitionObject(p.Spec.Kind)
	if definition == nil {
		return fmt.Errorf("unsupported retained definition kind")
	}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: source.Name}, definition); err != nil {
		return fmt.Errorf("retained definition unavailable: %w", err)
	}
	if definition.GetUID() != source.UID || definition.GetDeletionTimestamp() != nil {
		return fmt.Errorf("retained definition identity unavailable")
	}
	return nil
}

// AdmissionReady requires a current aggregate eligibility decision for the retained policy.
// Unknown or stale decisions are availability failures, not proof of identity loss.
func AdmissionReady(p *api.StacksNetworkParticipant) error {
	condition := meta.FindStatusCondition(p.Status.Conditions, api.ConditionAdmissionReady)
	if condition != nil && condition.Status == metav1.ConditionFalse {
		return fmt.Errorf("retained admission ineligible: %s", condition.Reason)
	}
	if condition == nil || condition.ObservedGeneration != p.Generation || condition.Status != metav1.ConditionTrue {
		return apierrors.NewServiceUnavailable("retained admission eligibility has not been observed for this generation")
	}
	return nil
}

// ValidateAdmissionEligibility verifies current retained-source authority before a new mutation.
// Definite aggregate withdrawal takes precedence over a failed source observation.
func ValidateAdmissionEligibility(ctx context.Context, reader client.Reader, p *api.StacksNetworkParticipant) error {
	sourceErr := ValidateAdmissionSource(ctx, reader, p)
	readyErr := AdmissionReady(p)
	if readyErr != nil && !TransientAPIError(readyErr) {
		return readyErr
	}
	if sourceErr != nil {
		return sourceErr
	}
	return readyErr
}

// admissionReadiness evaluates retained eligibility independently of candidate validation.
func (r *Reconciler) admissionReadiness(ctx context.Context, root *api.StacksNetwork, p *api.StacksNetworkParticipant, dependencies *dependencyCheck) (metav1.ConditionStatus, string, string) {
	if p.Status.Admission == nil || Digest(p.Status.Admission.Configuration) != p.Status.Admission.PolicyDigest {
		return metav1.ConditionFalse, api.ReasonAdmissionUnavailable, "No complete retained policy is available"
	}
	id := findIdentity(root.Status.Identities, p.Spec.ParticipantName)
	selected := false
	for _, entry := range root.Spec.Participants {
		selected = selected || entry.Name == p.Spec.ParticipantName && entry.Kind == p.Spec.Kind
	}
	if !selected || id == nil || id.UID != p.UID || id.Removing || p.DeletionTimestamp != nil || p.Spec.NetworkUID != root.UID || !ownedUID(p, root.UID) {
		return metav1.ConditionFalse, api.ReasonIdentityUnavailable, "Retained participant identity is no longer eligible"
	}
	if err := dependencies.source(ctx, p); err != nil {
		return eligibilityFailure(err, reasonDefinitionUnavailable)
	}
	if err := dependencies.validate(ctx, p.Status.Admission.Dependencies); err != nil {
		return eligibilityFailure(err, api.ReasonIdentityUnavailable)
	}
	return metav1.ConditionTrue, reasonRetainedPolicyEligible, "Retained source and required identities remain eligible"
}

// eligibilityFailure preserves the difference between observed loss and a failed observation.
func eligibilityFailure(err error, reason string) (metav1.ConditionStatus, string, string) {
	if TransientAPIError(err) {
		return metav1.ConditionUnknown, api.ReasonObservationUnavailable, "Retained admission identity could not be observed"
	}
	return metav1.ConditionFalse, reason, err.Error()
}

// TransientAPIError recognizes transport and API availability errors without masking denial or loss.
func TransientAPIError(err error) bool {
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) || apierrors.IsNotFound(err) || errors.Is(err, context.Canceled) {
		return false
	}
	if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || apierrors.IsServiceUnavailable(err) || apierrors.IsTooManyRequests(err) || apierrors.IsInternalError(err) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return true
	}
	var network net.Error
	return errors.As(err, &network) && network.Timeout()
}
