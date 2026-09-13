package participantworkload

import (
	"context"
	"fmt"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// stacksStartAuthorized requires the real retained preparation record, never aggregate phase alone.
func (r *Reconciler) stacksStartAuthorized(
	ctx context.Context,
	root *api.StacksNetwork,
	p *api.StacksNetworkParticipant,
	now time.Time,
) error {
	if root.Status.Bitcoin == nil || root.Status.Bitcoin.InitializationRef == nil || root.Status.GenesisRef == nil {
		return fmt.Errorf("PrepareBitcoin record binding is unavailable")
	}
	ref := root.Status.Bitcoin.InitializationRef
	var initialization bitcoin.BitcoinInitialization
	if err := r.Reader.Get(
		ctx,
		client.ObjectKey{Namespace: root.Namespace, Name: ref.Name},
		&initialization,
	); err != nil {
		return err
	}
	owner := metav1.GetControllerOf(&initialization)
	if ref.Kind != bitcoin.KindBitcoinInitialization || ref.UID == "" || initialization.UID != ref.UID ||
		initialization.DeletionTimestamp != nil ||
		initialization.Spec.NetworkUID != root.UID ||
		owner == nil ||
		owner.UID != root.UID ||
		owner.Kind != api.KindStacksNetwork ||
		owner.APIVersion != api.GroupVersion.String() ||
		initialization.Spec.Genesis.UID != root.Status.GenesisRef.UID ||
		initialization.Spec.Genesis.Name != root.Status.GenesisRef.Name ||
		initialization.Status.PreparedAt == nil {
		return fmt.Errorf("PrepareBitcoin has not completed for this frozen network identity")
	}
	// Consensus startup deliberately has no enrollment or PoX registration dependency.
	if p.Spec.Kind == api.ParticipantStacksSigner {
		return nil
	}
	node := p.Status.Admission.Configuration.StacksNode
	if node == nil {
		return fmt.Errorf("missing Stacks node admission")
	}
	if node.Mining == nil || !ptr.Deref(node.Mining.Enabled, false) {
		return nil
	}
	// The live workload records prior miner activation; preparing a new config does not.
	var existing appsv1.StatefulSet
	if err := r.Reader.Get(
		ctx,
		client.ObjectKey{Namespace: p.Namespace, Name: Name(p, actorPurpose)},
		&existing,
	); err == nil {
		if !owned(&existing, p) || existing.DeletionTimestamp != nil {
			return fmt.Errorf("miner workload identity unavailable")
		}
		if ptr.Deref(existing.Spec.Replicas, 0) > 0 &&
			existing.Spec.Template.Annotations[miningEnabledAnnotation] == "true" {
			return nil
		}
	} else if !apierrors.IsNotFound(
		err,
	) {
		return err
	}
	wallet, err := r.minerWallet(ctx, p, node.Mining.BitcoinWalletRef)
	if err != nil {
		return err
	}
	btc, err := r.boundParticipant(ctx, root, p, node.BitcoinNodeRef, api.ParticipantBitcoinNode)
	if err != nil {
		return err
	}
	runtime := btc.Status.Runtime
	if runtime == nil || runtime.Terminated || runtime.PodRef == nil || runtime.ContainerID == "" ||
		runtime.ConfigRef == nil ||
		runtime.RPCSecretRef == nil {
		return fmt.Errorf("unavailable Bitcoin actor observation identity")
	}
	for _, executionRef := range root.Status.Bitcoin.ExecutionRefs {
		var execution bitcoin.BitcoinExecution
		if err := r.Reader.Get(
			ctx,
			client.ObjectKey{Namespace: root.Namespace, Name: executionRef.Name},
			&execution,
		); err != nil {
			return err
		}
		if execution.Spec.Participant.UID != btc.UID {
			continue
		}
		owner := metav1.GetControllerOf(&execution)
		if executionRef.Kind != bitcoin.KindBitcoinExecution || executionRef.UID == "" ||
			execution.UID != executionRef.UID ||
			execution.DeletionTimestamp != nil ||
			execution.Spec.NetworkUID != root.UID ||
			owner == nil ||
			owner.UID != root.UID ||
			owner.Kind != api.KindStacksNetwork ||
			owner.APIVersion != api.GroupVersion.String() {
			return fmt.Errorf("changed Bitcoin execution identity")
		}
		observation := execution.Status.Observation
		if observation == nil || observation.ObservedAt.After(now) ||
			now.Sub(observation.ObservedAt.Time) > 10*time.Second {
			return fmt.Errorf("fresh miner wallet observation unavailable")
		}
		target := observation.Target
		if target.Participant.UID != btc.UID || target.Participant.Name != btc.Name || target.Pod != *runtime.PodRef ||
			target.ContainerID != runtime.ContainerID ||
			target.Configuration != *runtime.ConfigRef ||
			target.Credentials != *runtime.RPCSecretRef ||
			target.PolicyDigest != runtime.PolicyDigest {
			return fmt.Errorf("miner observation does not match current Bitcoin process")
		}
		for _, local := range observation.Wallets {
			if local.Wallet.UID == wallet.UID && local.Wallet.Name == wallet.Name &&
				local.Name == ptr.Deref(wallet.Spec.WalletName, wallet.Name) &&
				local.Address == wallet.Status.BitcoinAddress &&
				local.Ready &&
				local.MatureOutputs >= 1 {
				return nil
			}
		}
		return fmt.Errorf("miner wallet has no confirmed mature local output")
	}
	return fmt.Errorf("miner Bitcoin execution binding unavailable")
}
