// Package productionledger reads the producer's retained per-target reservation facts.
package productionledger

import (
	"context"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Read returns a target ledger and its verified aggregate ownership binding.
func Read(ctx context.Context, reader client.Reader, n *networkv1.StacksNetwork, namespace, network, target string) (*bitcoinv1.BitcoinProductionTarget, bool, error) {
	t := &bitcoinv1.BitcoinProductionTarget{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: target}, t); err != nil {
		return t, false, client.IgnoreNotFound(err)
	}
	p := &bitcoinv1.BitcoinBlockProduction{}
	err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: network}, p)
	if apierrors.IsNotFound(err) {
		return t, false, nil
	}
	if err != nil {
		return t, false, err
	}
	return t, n.UID != "" && p.Spec.NetworkUID == string(n.UID) && n.Status.BitcoinProductionUID == string(p.UID) && metav1.IsControlledBy(p, n) && p.Binds(t), nil
}
