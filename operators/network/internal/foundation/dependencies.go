package foundation

import (
	"context"
	"fmt"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// dependencyCheck validates current bindings at admission and capture boundaries.
// Reads are uncached; memoization lasts only for this validation pass.
type dependencyCheck struct {
	// publicOnly excludes credential metadata checks for consumers of immutable public facts.
	publicOnly bool
	reader     client.Reader
	root       *api.StacksNetwork
	results    map[common.Binding]error
	sources    map[common.Binding]error
	visiting   map[common.Binding]bool
}

func newDependencyCheck(reader client.Reader, root *api.StacksNetwork) *dependencyCheck {
	return &dependencyCheck{reader: reader, root: root, results: map[common.Binding]error{}, sources: map[common.Binding]error{}, visiting: map[common.Binding]bool{}}
}

// validate keeps historical bindings separate from evidence of current availability.
func (v *dependencyCheck) validate(ctx context.Context, bindings []common.Binding) error {
	for _, b := range bindings {
		if err := v.binding(ctx, b); err != nil {
			return err
		}
	}
	return nil
}

func (v *dependencyCheck) binding(ctx context.Context, b common.Binding) error {
	if err, ok := v.results[b]; ok {
		return err
	}
	if v.visiting[b] {
		return fmt.Errorf("cyclic admission dependency %s %s", b.Kind, b.Name)
	}
	v.visiting[b] = true
	err := v.read(ctx, b)
	delete(v.visiting, b)
	v.results[b] = err
	return err
}

func (v *dependencyCheck) read(ctx context.Context, b common.Binding) error {
	if b.Kind == common.KindSecret && v.publicOnly {
		if b.Name == "" || b.UID == "" {
			return fmt.Errorf("credential identity unavailable")
		}
		return nil
	}
	var obj client.Object
	switch b.Kind {
	case stacks.KindStacksAccount:
		obj = &stacks.StacksAccount{}
	case bitcoin.KindBitcoinWallet:
		obj = &bitcoin.BitcoinWallet{}
	case api.KindStacksNetworkParticipant:
		obj = &api.StacksNetworkParticipant{}
	case bitcoin.KindBitcoinBlockSchedule:
		obj = &bitcoin.BitcoinBlockSchedule{}
	case api.KindStacksEpochSchedule:
		obj = &api.StacksEpochSchedule{}
	case common.KindSecret:
		obj = &metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: common.KindSecret}}
	default:
		return fmt.Errorf("unsupported dependency kind %s", b.Kind)
	}
	key := client.ObjectKey{Namespace: v.root.Namespace, Name: b.Name}
	if err := v.reader.Get(ctx, key, obj); err != nil {
		return fmt.Errorf("%s %s identity unavailable: %w", b.Kind, b.Name, err)
	}
	if b.UID == "" || obj.GetUID() != b.UID || obj.GetDeletionTimestamp() != nil {
		return fmt.Errorf("%s %s identity unavailable", b.Kind, b.Name)
	}
	if wallet, ok := obj.(*bitcoin.BitcoinWallet); ok {
		if err := ValidateWalletProfile(wallet.Spec); err != nil {
			return err
		}
	}
	if identity, ok := obj.(resolvable); ok && (b.Kind == stacks.KindStacksAccount || b.Kind == bitcoin.KindBitcoinWallet) {
		status := identity.GetResolutionStatus()
		if !resolved(identity) || status.Digest != b.Fingerprint {
			return fmt.Errorf("%s %s public identity unavailable", b.Kind, b.Name)
		}
		if ref := status.CredentialsRef; ref != nil {
			if err := v.binding(ctx, common.Binding{Kind: common.KindSecret, Name: ref.Name, UID: status.CredentialsUID}); err != nil {
				return err
			}
		}
		if wallet, ok := obj.(*bitcoin.BitcoinWallet); ok && wallet.Spec.KeySource != nil && wallet.Spec.KeySource.StacksMinerAccountRef != nil {
			ref := wallet.Spec.KeySource.StacksMinerAccountRef
			if len(status.Dependencies) != 1 || status.Dependencies[0].Kind != stacks.KindStacksAccount || status.Dependencies[0].Name != ref.Name {
				return fmt.Errorf("wallet %s derived account binding unavailable", b.Name)
			}
			return v.validate(ctx, status.Dependencies)
		}
	}
	if p, ok := obj.(*api.StacksNetworkParticipant); ok {
		id := findIdentity(v.root.Status.Identities, p.Spec.ParticipantName)
		selected := false
		for _, entry := range v.root.Spec.Participants {
			selected = selected || entry.Name == p.Spec.ParticipantName && entry.Kind == p.Spec.Kind
		}
		if !selected || id == nil || id.Removing || id.UID != p.UID || p.Spec.NetworkUID != v.root.UID || !ownedUID(p, v.root.UID) {
			return fmt.Errorf("participant %s is no longer selected with its admitted identity", p.Spec.ParticipantName)
		}
		if p.Status.Admission == nil {
			return fmt.Errorf("participant %s has no admitted policy", p.Spec.ParticipantName)
		}
		if err := v.source(ctx, p); err != nil {
			return err
		}
		return v.validate(ctx, p.Status.Admission.Dependencies)
	}
	return nil
}

// ValidateParticipantAdmission checks a current complete policy and all captured dependencies.
// The caller must read the root and participant through the uncached reader and separately
// enforce operation, runtime identity and capability-specific gates before mutation.
func ValidateParticipantAdmission(ctx context.Context, reader client.Reader, root *api.StacksNetwork, participant *api.StacksNetworkParticipant) error {
	if participant.Namespace != root.Namespace || participant.Status.Admission == nil || Digest(participant.Status.Admission.Configuration) != participant.Status.Admission.PolicyDigest {
		return fmt.Errorf("participant has no complete admitted policy")
	}
	return newDependencyCheck(reader, root).validate(ctx, []common.Binding{objectref.Participant(participant)})
}

// ValidateDependencyBindings checks an explicitly selected set of admitted mandatory inputs.
// Capability controllers select these inputs; target liveness remains a separate decision.
func ValidateDependencyBindings(ctx context.Context, reader client.Reader, root *api.StacksNetwork, bindings []common.Binding) error {
	return newDependencyCheck(reader, root).validate(ctx, bindings)
}

// ValidatePublicDependencyBindings validates public identities without granting signing Secret access.
// Bitcoin dispatch uses this after the scheduler validated credential metadata for its short-lived offer.
func ValidatePublicDependencyBindings(ctx context.Context, reader client.Reader, root *api.StacksNetwork, bindings []common.Binding) error {
	check := newDependencyCheck(reader, root)
	check.publicOnly = true
	return check.validate(ctx, bindings)
}

// ValidatePublicParticipantAdmission validates an admitted actor using public dependencies only.
func ValidatePublicParticipantAdmission(ctx context.Context, reader client.Reader, root *api.StacksNetwork, participant *api.StacksNetworkParticipant) error {
	if participant.Namespace != root.Namespace || participant.Status.Admission == nil || Digest(participant.Status.Admission.Configuration) != participant.Status.Admission.PolicyDigest {
		return fmt.Errorf("participant has no complete admitted policy")
	}
	return ValidatePublicDependencyBindings(ctx, reader, root, []common.Binding{objectref.Participant(participant)})
}

// source memoizes immutable source identity checks only within this validation pass.
func (v *dependencyCheck) source(ctx context.Context, p *api.StacksNetworkParticipant) error {
	if p.Status.Admission == nil || p.Status.Admission.Source.Name == "" {
		return ValidateAdmissionSource(ctx, v.reader, p)
	}
	source := p.Status.Admission.Source
	key := common.Binding{Kind: string(p.Spec.Kind), Name: source.Name, UID: source.UID}
	if err, ok := v.sources[key]; ok {
		return err
	}
	err := ValidateAdmissionSource(ctx, v.reader, p)
	v.sources[key] = err
	return err
}
