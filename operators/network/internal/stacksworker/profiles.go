package stacksworker

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Profiles compiles public role bootstrap inputs without reading signing key bytes.
type Profiles struct {
	// Client creates participant-owned immutable public configuration.
	Client client.Client
	// Reader performs uncached public and Secret metadata reads.
	Reader client.Reader
	// Image selects the worker binary image before durable binding.
	Image string
}

// TransactionProfiles retains the transaction provisioning entrypoint with shared role resolution.
type TransactionProfiles = Profiles

// bootstrapAccount pins one role-specific key together with its verified public identity.
type bootstrapAccount struct {
	// Account binds the admitted reusable public identity.
	Account common.Binding `json:"account"`
	// Identity is public verification material for the mounted key.
	Identity common.PublicIdentity `json:"identity"`
	// Credential exposes only the selected entry under the role's mount path.
	Credential KeyMount `json:"credential"`
}

// account resolves exact admitted public identity and scoped credential metadata.
func (r Profiles) account(
	ctx context.Context,
	p *api.StacksNetworkParticipant,
	name, role string,
) (bootstrapAccount, error) {
	var account stacks.StacksAccount
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: name}, &account); err != nil {
		return bootstrapAccount{}, err
	}
	captured := false
	if p.Status.Admission != nil {
		for _, b := range p.Status.Admission.Dependencies {
			if b.Kind == stacks.KindStacksAccount && b.Name == name && b.UID == account.UID &&
				b.Fingerprint == account.Status.Digest {
				captured = true
			}
		}
	}
	if !captured || account.DeletionTimestamp != nil || account.Status.ObservedGeneration != account.Generation ||
		!meta.IsStatusConditionTrue(account.Status.Conditions, common.ConditionResolved) ||
		account.Status.Identity == nil ||
		account.Status.CredentialsRef == nil ||
		account.Status.CredentialsUID == "" {
		return bootstrapAccount{}, fmt.Errorf("role account is not admitted and resolved")
	}
	key := KeyMount{
		Role: role,
		Secret: common.Binding{
			Kind: common.KindSecret,
			Name: account.Status.CredentialsRef.Name,
			UID:  account.Status.CredentialsUID,
		},
		Key: account.Status.CredentialsRef.Key,
	}
	secretMeta := &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: common.KindSecret},
	}
	if err := r.Reader.Get(
		ctx,
		client.ObjectKey{Namespace: p.Namespace, Name: key.Secret.Name},
		secretMeta,
	); err != nil {
		return bootstrapAccount{}, err
	}
	if secretMeta.UID != key.Secret.UID || secretMeta.DeletionTimestamp != nil {
		return bootstrapAccount{}, fmt.Errorf("role key identity changed")
	}
	return bootstrapAccount{
		Account:    objectref.WithFingerprint(objectref.Account(&account), account.Status.Digest),
		Identity:   *account.Status.Identity,
		Credential: key,
	}, nil
}

// signer resolves the exact admitted consensus participant without reading its private config.
func (r Profiles) signer(
	ctx context.Context,
	root *api.StacksNetwork,
	p *api.StacksNetworkParticipant,
	name string,
) (*api.StacksNetworkParticipant, error) {
	generated := foundation.ParticipantName(string(root.UID), name)
	var signer api.StacksNetworkParticipant
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: generated}, &signer); err != nil {
		return nil, err
	}
	captured := false
	for _, b := range p.Status.Admission.Dependencies {
		if b.Kind == api.KindStacksNetworkParticipant && b.Name == generated && b.UID == signer.UID {
			captured = true
		}
	}
	if !captured || signer.Spec.NetworkUID != root.UID || signer.Spec.ParticipantName != name ||
		signer.Spec.Kind != api.ParticipantStacksSigner ||
		signer.DeletionTimestamp != nil ||
		!metav1.IsControlledBy(&signer, root) ||
		signer.Status.Admission == nil ||
		signer.Status.Admission.Configuration.StacksSigner == nil ||
		signer.Status.Admission.Configuration.StacksSigner.AccountRef == nil {
		return nil, fmt.Errorf("consensus participant identity unavailable")
	}
	return &signer, nil
}

// Resolve pins each role's keys and immutable public genesis/identity configuration.
func (r Profiles) Resolve(
	ctx context.Context,
	root *api.StacksNetwork,
	p *api.StacksNetworkParticipant,
) (Profile, error) {
	if p.Status.Admission == nil || root.Status.GenesisRef == nil {
		return Profile{}, fmt.Errorf("worker admission unavailable")
	}
	var accounts []bootstrapAccount
	var placement *common.Placement
	add := func(source *api.StacksNetworkParticipant, ref *common.NameRef, role string) error {
		if ref == nil {
			return fmt.Errorf("role account missing")
		}
		account, err := r.account(ctx, source, ref.Name, role)
		if err == nil {
			accounts = append(accounts, account)
		}
		return err
	}
	policy := p.Status.Admission.Configuration
	//nolint:exhaustive // Only the supported Stacks worker roles receive these inputs; other kinds stay excluded.
	switch p.Spec.Kind {
	case api.ParticipantStacksFaucet:
		if policy.StacksFaucet == nil {
			return Profile{}, fmt.Errorf("faucet policy unavailable")
		}
		placement = policy.StacksFaucet.WorkerPlacement
		if err := add(p, policy.StacksFaucet.AccountRef, KeyRoleSender); err != nil {
			return Profile{}, err
		}
	case api.ParticipantStacksTransactionProduction:
		if policy.StacksTransactionProduction == nil {
			return Profile{}, fmt.Errorf("transaction policy unavailable")
		}
		placement = policy.StacksTransactionProduction.WorkerPlacement
		if err := add(p, policy.StacksTransactionProduction.AccountRef, KeyRoleSender); err != nil {
			return Profile{}, err
		}
	case api.ParticipantStacksContractSet:
		if policy.StacksContractSet == nil {
			return Profile{}, fmt.Errorf("contract policy unavailable")
		}
		placement = policy.StacksContractSet.WorkerPlacement
		if err := add(p, policy.StacksContractSet.DeployerAccountRef, KeyRoleDeployer); err != nil {
			return Profile{}, err
		}
	case api.ParticipantStacksStacker:
		stacker := policy.StacksStacker
		if stacker == nil || stacker.SignerRef == nil {
			return Profile{}, fmt.Errorf("stacker policy unavailable")
		}
		placement = stacker.WorkerPlacement
		if err := add(p, stacker.HolderAccountRef, KeyRoleHolder); err != nil {
			return Profile{}, err
		}
		if err := add(p, stacker.AdministratorAccountRef, KeyRoleAdministrator); err != nil {
			return Profile{}, err
		}
		signer, err := r.signer(ctx, root, p, stacker.SignerRef.Name)
		if err != nil {
			return Profile{}, err
		}
		if err := add(signer, signer.Status.Admission.Configuration.StacksSigner.AccountRef, "consensus"); err != nil {
			return Profile{}, err
		}
	default:
		return Profile{}, fmt.Errorf("worker role bootstrap is unsupported")
	}
	var genesis api.StacksGenesis
	if err := r.Reader.Get(
		ctx,
		client.ObjectKey{Namespace: p.Namespace, Name: root.Status.GenesisRef.Name},
		&genesis,
	); err != nil {
		return Profile{}, err
	}
	if genesis.UID != root.Status.GenesisRef.UID || genesis.DeletionTimestamp != nil ||
		!metav1.IsControlledBy(&genesis, root) ||
		foundation.Digest(genesis.Spec.Chain) != root.Status.GenesisDigest {
		return Profile{}, fmt.Errorf("frozen genesis identity changed")
	}
	// Bootstrap excludes live amount, fee, destination, timing and root generation.
	input := struct {
		Network     common.Binding     `json:"network"`
		Participant common.Binding     `json:"participant"`
		Genesis     common.Binding     `json:"genesis"`
		Accounts    []bootstrapAccount `json:"accounts"`
	}{objectref.Network(root), objectref.Participant(p), *root.Status.GenesisRef, accounts}
	raw, err := json.Marshal(input)
	if err != nil {
		return Profile{}, err
	}
	metadata := metadata(p)
	metadata.Name = foundation.RuntimeName(
		string(root.UID),
		string(p.UID),
		string(p.Spec.Kind),
		p.Spec.ParticipantName,
		"bootstrap-"+foundation.Digest(input)[7:19],
	)
	configuration := &corev1.ConfigMap{
		ObjectMeta: metadata,
		Immutable:  ptr.To(true),
		Data:       map[string]string{"input.json": string(raw)},
	}
	if err := r.Client.Create(ctx, configuration); err != nil && !apierrors.IsAlreadyExists(err) {
		return Profile{}, err
	}
	var current corev1.ConfigMap
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(configuration), &current); err != nil {
		return Profile{}, err
	}
	if !metav1.IsControlledBy(&current, p) || current.DeletionTimestamp != nil ||
		!ptr.Deref(current.Immutable, false) ||
		!reflect.DeepEqual(current.Data, configuration.Data) {
		return Profile{}, fmt.Errorf("worker bootstrap configuration conflicts")
	}
	reads, err := r.Reads(ctx, root, p)
	if err != nil {
		return Profile{}, err
	}
	keys := make([]KeyMount, 0, len(accounts))
	for _, account := range accounts {
		keys = append(keys, account.Credential)
	}
	return (Profile{
		Image:         r.Image,
		Configuration: objectref.ConfigMap(&current),
		Keys:          keys,
		Reads:         reads,
		Placement:     placement,
	}).Normalize()
}

// Reads selects current public dependencies; keys remain mounted and never API-readable.
func (r Profiles) Reads(
	ctx context.Context,
	root *api.StacksNetwork,
	p *api.StacksNetworkParticipant,
) ([]ReadBinding, error) {
	if p.Status.Admission == nil || root.Status.GenesisRef == nil {
		return nil, fmt.Errorf("worker admitted dependencies unavailable")
	}
	reads := []ReadBinding{
		{
			APIVersion: api.GroupVersion.String(),
			Resource:   api.ResourceStacksGenesis,
			Name:       root.Status.GenesisRef.Name,
		},
	}
	if source := p.Status.Admission.Source; source.Name != "" {
		reads = append(
			reads,
			ReadBinding{
				APIVersion: stacks.GroupVersion.String(),
				Resource:   strings.ToLower(string(p.Spec.Kind)) + "s",
				Name:       source.Name,
			},
		)
	}
	add := func(dependencies []common.Binding) {
		for _, b := range dependencies {
			switch b.Kind {
			case stacks.KindStacksAccount:
				reads = append(
					reads,
					ReadBinding{
						APIVersion: stacks.GroupVersion.String(),
						Resource:   stacks.ResourceStacksAccount,
						Name:       b.Name,
					},
				)
			case api.KindStacksNetworkParticipant:
				reads = append(
					reads,
					ReadBinding{
						APIVersion: api.GroupVersion.String(),
						Resource:   api.ResourceStacksNetworkParticipant,
						Name:       b.Name,
					},
				)
			}
		}
	}
	add(p.Status.Admission.Dependencies)
	if stacker := p.Status.Admission.Configuration.StacksStacker; stacker != nil {
		if stacker.SignerRef == nil {
			return nil, fmt.Errorf("consensus participant missing")
		}
		signer, err := r.signer(ctx, root, p, stacker.SignerRef.Name)
		if err != nil {
			return nil, err
		}
		add(signer.Status.Admission.Dependencies)
	}
	if len(reads) > 100 {
		return nil, fmt.Errorf("worker dependency bound exceeded")
	}
	return reads, nil
}
