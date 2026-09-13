package bitcoincontrol

import (
	"math"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// accountBootstrapReceipt consumes retained execution evidence independently of current selection.
// The caller persists the funding counters and cursor together before permitting another send.
// Producer authorization belongs to the retained request, not the latest selected producer.
func accountBootstrapReceipt(root *api.StacksNetwork, initial *bitcoin.BitcoinInitialization, execution *bitcoin.BitcoinExecution) bool {
	if execution.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(execution, root) || execution.Spec.Participant != initial.Spec.Target {
		return false
	}
	receipt := execution.Status.LastReceipt
	if receipt == nil || receipt.Request.Method != bitcoin.RPCGenerate || receipt.Request.Action != nil || receipt.Request.Offer == nil || receipt.Request.Reservation != objectref.BitcoinInitialization(initial) || receipt.Request.Target.Participant != initial.Spec.Target || !hashValid(receipt.BlockHash) {
		return false
	}
	offer := receipt.Request.Offer
	if offer.Mode != "" && offer.Mode != bitcoin.OfferBootstrap || offer.Initialization != objectref.BitcoinInitialization(initial) || offer.Production.Kind != api.KindStacksNetworkParticipant || offer.Production.UID == "" || offer.Production.Name == "" || offer.Number <= initial.Status.LastAccountedOffer || offer.Number != execution.Status.CompletedOffer || offer.ExpectedHeight < 0 || offer.ExpectedHeight >= offer.Ceiling {
		return false
	}
	known := offer.Wallet == initial.Spec.PayoutWallet.Wallet && offer.Address == initial.Spec.PayoutWallet.Address
	funding := false
	for _, wallet := range initial.Spec.MinerWallets {
		if wallet.Wallet == offer.Wallet && wallet.Address == offer.Address {
			known = true
			funding = offer.ExpectedHeight < initial.Spec.MinimumHeight
		}
	}
	if !known {
		return false
	}
	if funding {
		found := false
		for i := range initial.Status.Funded {
			count := &initial.Status.Funded[i]
			if count.WalletUID == offer.Wallet.UID {
				if count.Outputs == math.MaxInt32 {
					return false
				}
				count.Outputs++
				found = true
				break
			}
		}
		if !found {
			initial.Status.Funded = append(initial.Status.Funded, bitcoin.BitcoinFundingCount{WalletUID: offer.Wallet.UID, Outputs: 1})
		}
	}
	initial.Status.LastAccountedOffer = offer.Number
	return true
}
