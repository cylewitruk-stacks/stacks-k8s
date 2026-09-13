package foundation

import (
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RuntimeOptions installs supported domains and the aggregate-owned runtime projection.
type RuntimeOptions struct {
	// Kinds transfers workload status ownership from the unsupported-kind fallback.
	Kinds []api.ParticipantKind
	// Runtime allocates shared records and projects network runtime observations.
	Runtime NetworkRuntime
	// Configurations validates candidate actor configuration before admission and freeze.
	Configurations CandidateConfigurationValidator
}

// Register composes independent root, identity and definition controllers in one manager.
func Register(manager ctrl.Manager, image string, options ...RuntimeOptions) error {
	root := &Reconciler{
		Client:       manager.GetClient(),
		Reader:       manager.GetAPIReader(),
		Scheme:       manager.GetScheme(),
		RuntimeKinds: map[api.ParticipantKind]bool{},
	}
	for _, option := range options {
		for _, kind := range option.Kinds {
			root.RuntimeKinds[kind] = true
		}
		if option.Runtime != nil {
			root.Runtime = option.Runtime
		}
		if option.Configurations != nil {
			root.Configurations = option.Configurations
		}
	}
	if err := root.SetupWithManager(manager); err != nil {
		return err
	}
	for _, wallet := range []bool{false, true} {
		r := &IdentityReconciler{
			Client: manager.GetClient(),
			Reader: manager.GetAPIReader(),
			Scheme: manager.GetScheme(),
			Wallet: wallet,
			Image:  image,
		}
		if err := r.SetupWithManager(manager); err != nil {
			return err
		}
	}
	for _, prototype := range []client.Object{
		&bitcoin.BitcoinNode{}, &stacks.StacksNode{}, &stacks.StacksSigner{}, &stacks.StacksStacker{},
		&stacks.StacksFaucet{}, &stacks.StacksContractSet{}, &stacks.StacksTransactionProduction{},
		&bitcoin.BitcoinBlockProduction{}, &api.StacksEpochSchedule{}, &bitcoin.BitcoinBlockSchedule{},
	} {
		r := &DefinitionReconciler{Client: manager.GetClient(), Scheme: manager.GetScheme(), Prototype: prototype}
		if err := r.SetupWithManager(manager); err != nil {
			return err
		}
	}
	return nil
}
