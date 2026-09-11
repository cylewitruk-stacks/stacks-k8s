package foundation

import ctrl "sigs.k8s.io/controller-runtime"

// Register composes independent root, identity and definition controllers in one manager.
func Register(manager ctrl.Manager, image string) error {
	root := &Reconciler{Client: manager.GetClient(), Reader: manager.GetAPIReader(), Scheme: manager.GetScheme()}
	if err := root.SetupWithManager(manager); err != nil {
		return err
	}
	for _, wallet := range []bool{false, true} {
		r := &IdentityReconciler{Client: manager.GetClient(), Reader: manager.GetAPIReader(), Scheme: manager.GetScheme(), Wallet: wallet, Image: image}
		if err := r.SetupWithManager(manager); err != nil {
			return err
		}
	}
	for _, kind := range []string{"BitcoinNode", "StacksNode", "StacksSigner", "StacksStacker", "StacksFaucet", "StacksContractSet", "StacksTransactionProduction", "BitcoinBlockProduction", "StacksEpochSchedule", "BitcoinBlockSchedule"} {
		r := &DefinitionReconciler{Client: manager.GetClient(), Scheme: manager.GetScheme(), Kind: kind}
		if err := r.SetupWithManager(manager); err != nil {
			return err
		}
	}
	return nil
}
