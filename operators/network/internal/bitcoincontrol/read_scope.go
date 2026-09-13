package bitcoincontrol

import (
	"context"
	"fmt"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// publicReadScope bounds worker reads to admitted public dependencies, excluding signing Secrets.
func publicReadScope(ctx context.Context, reader client.Reader, p *api.StacksNetworkParticipant, initial *bitcoin.BitcoinInitialization) ([]common.Binding, error) {
	queue := []common.Binding{initial.Spec.PayoutWallet.Wallet, binding("StacksNetworkParticipant", p), productionBinding(initial)}
	out := []common.Binding{}
	seen := map[string]bool{}
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		key := ref.Kind + "/" + ref.Name
		if seen[key] || ref.Kind == "Secret" || ref.Name == "" {
			continue
		}
		seen[key] = true
		if len(seen) > 1000 {
			return nil, fmt.Errorf("public control scope exceeds supported bound")
		}
		var object client.Object
		switch ref.Kind {
		case "StacksNetworkParticipant":
			object = &api.StacksNetworkParticipant{}
		case "BitcoinWallet":
			object = &bitcoin.BitcoinWallet{}
		case "StacksAccount":
			object = &stacks.StacksAccount{}
		case "BitcoinNode", "BitcoinBlockProduction", "BitcoinBlockSchedule":
			out = append(out, ref)
			continue
		default:
			continue
		}
		out = append(out, ref)
		err := reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: ref.Name}, object)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if object.GetUID() != ref.UID {
			continue
		}
		switch current := object.(type) {
		case *api.StacksNetworkParticipant:
			if current.Status.Admission != nil {
				if source := current.Status.Admission.Source; source.Name != "" {
					queue = append(queue, common.Binding{Kind: string(current.Spec.Kind), Name: source.Name, UID: source.UID})
				}
				for _, dep := range current.Status.Admission.Dependencies {
					if current.Spec.Kind == "BitcoinBlockProduction" && dep.Kind != "BitcoinBlockSchedule" && dep != initial.Spec.PayoutWallet.Wallet {
						continue
					}
					queue = append(queue, dep)
				}
			}
		case *bitcoin.BitcoinWallet:
			queue = append(queue, current.Status.Dependencies...)
		case *stacks.StacksAccount:
			queue = append(queue, current.Status.Dependencies...)
		}
	}
	return out, nil
}
