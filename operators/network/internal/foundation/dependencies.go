package foundation

import (
	"context"
	"fmt"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// dependencyCheck validates current bindings at admission and capture boundaries.
// Reads are uncached; memoization lasts only for this validation pass.
type dependencyCheck struct {
	reader   client.Reader
	root     *api.StacksNetwork
	results  map[common.Binding]error
	visiting map[common.Binding]bool
}

func newDependencyCheck(reader client.Reader, root *api.StacksNetwork) *dependencyCheck {
	return &dependencyCheck{reader: reader, root: root, results: map[common.Binding]error{}, visiting: map[common.Binding]bool{}}
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
	var obj client.Object
	switch b.Kind {
	case "StacksAccount":
		obj = &stacks.StacksAccount{}
	case "BitcoinWallet":
		obj = &bitcoin.BitcoinWallet{}
	case "StacksNetworkParticipant":
		obj = &api.StacksNetworkParticipant{}
	case "BitcoinBlockSchedule":
		obj = &bitcoin.BitcoinBlockSchedule{}
	case "StacksEpochSchedule":
		obj = &api.StacksEpochSchedule{}
	case "Secret":
		obj = &metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}}
	default:
		return fmt.Errorf("unsupported dependency kind %s", b.Kind)
	}
	key := client.ObjectKey{Namespace: v.root.Namespace, Name: b.Name}
	if err := v.reader.Get(ctx, key, obj); err != nil || b.UID == "" || obj.GetUID() != b.UID || obj.GetDeletionTimestamp() != nil {
		return fmt.Errorf("%s %s identity unavailable", b.Kind, b.Name)
	}
	if identity, ok := obj.(resolvable); ok && (b.Kind == "StacksAccount" || b.Kind == "BitcoinWallet") {
		status := identity.GetResolutionStatus()
		if !resolved(identity) || status.Digest != b.Fingerprint {
			return fmt.Errorf("%s %s public identity unavailable", b.Kind, b.Name)
		}
		if ref := status.CredentialsRef; ref != nil {
			if err := v.binding(ctx, common.Binding{Kind: "Secret", Name: ref.Name, UID: status.CredentialsUID}); err != nil {
				return err
			}
		}
		if wallet, ok := obj.(*bitcoin.BitcoinWallet); ok && wallet.Spec.KeySource != nil && wallet.Spec.KeySource.StacksMinerAccountRef != nil {
			ref := wallet.Spec.KeySource.StacksMinerAccountRef
			if len(status.Dependencies) != 1 || status.Dependencies[0].Kind != "StacksAccount" || status.Dependencies[0].Name != ref.Name {
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
		if source := p.Spec.Source; source.Name != "" {
			definition := DefinitionObject(p.Spec.Kind)
			if definition == nil {
				return fmt.Errorf("participant %s has an unsupported definition", p.Spec.ParticipantName)
			}
			if err := v.reader.Get(ctx, client.ObjectKey{Namespace: v.root.Namespace, Name: source.Name}, definition); err != nil || definition.GetUID() != source.UID || definition.GetDeletionTimestamp() != nil {
				return fmt.Errorf("participant %s definition identity unavailable", p.Spec.ParticipantName)
			}
		}
		return v.validate(ctx, p.Status.Admission.Dependencies)
	}
	return nil
}
