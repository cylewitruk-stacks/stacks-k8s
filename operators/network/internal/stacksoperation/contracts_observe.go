package stacksoperation

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// contractState brackets the source inventory, registry tuple and deployer nonce at one tip.
type contractState struct {
	info          rpc.ChainView
	nakamoto      bool
	account       rpc.Account
	sources       map[string]bool
	registryEmpty bool
	observation   *api.ContractSetObservation
}

// contractConflict reports proven immutable-source or established-registry disagreement.
var contractConflict = errors.New("native contract state differs from captured requirements")

// registryArguments preserves native key ordering and validates public compressed identities.
func registryArguments(in ContractInputs) ([]clarity.Value, error) {
	if len(in.SignerPublicKeys) < 2 || len(in.SignerPublicKeys) > 100 || in.Threshold <= uint64(len(in.SignerPublicKeys)/2) || in.Threshold > uint64(len(in.SignerPublicKeys)) {
		return nil, errors.New("invalid explicit registry quorum")
	}
	keys := make([]clarity.Value, 0, len(in.SignerPublicKeys))
	for _, key := range in.SignerPublicKeys {
		public, err := identity.FromPublic(key)
		if err != nil || public.PublicKey != key {
			return nil, errors.New("invalid registry signer key")
		}
		raw, _ := hex.DecodeString(key)
		keys = append(keys, clarity.Value{Type: clarity.Buffer, Bytes: raw})
	}
	aggregate, err := identity.FromPublic(in.AggregatePublicKey)
	if err != nil || aggregate.PublicKey != in.AggregatePublicKey {
		return nil, errors.New("invalid registry aggregate key")
	}
	raw, _ := hex.DecodeString(in.AggregatePublicKey)
	return []clarity.Value{{Type: clarity.List, Items: keys}, {Type: clarity.Buffer, Bytes: raw}, clarity.Uint(in.Threshold)}, nil
}

// observeContracts verifies all public postconditions without issuing a transaction.
func observeContracts(ctx context.Context, in ContractInputs, sources []protocolcontracts.Source, now time.Time) (contractState, error) {
	state := contractState{sources: map[string]bool{}}
	before, err := in.Node.ChainView(ctx)
	if err != nil {
		return state, err
	}
	if before.NetworkID != 0x80000000 || !before.FullySynced || !canonicalHash(before.IndexBlockID) {
		return state, errors.New("contract observation chain identity differs")
	}
	state.info = before
	if before.BurnHeight < in.Epoch3Height {
		return state, nil
	}
	state.nakamoto, err = in.Node.NakamotoTip(ctx, before)
	if err != nil || !state.nakamoto {
		return state, err
	}
	state.account, err = in.Node.AccountAt(ctx, in.Deployer, before.IndexBlockID)
	if err != nil {
		return state, err
	}
	conflict := false
	for _, source := range sources {
		observed, found, err := in.Node.SourceAt(ctx, in.Deployer, source.Name, before.IndexBlockID)
		if err != nil {
			return state, err
		}
		if found && observed != source.Source {
			conflict = true
		}
		state.sources[source.Name] = found
	}
	if len(state.sources) == len(sources) {
		complete := true
		for _, found := range state.sources {
			complete = complete && found
		}
		if complete && !conflict {
			if err = observeRegistry(ctx, in, &state, now); err != nil {
				if !errors.Is(err, contractConflict) {
					return state, err
				}
				conflict = true
			}
		}
	}
	after, err := in.Node.ChainView(ctx)
	if err != nil {
		return state, err
	}
	if before != after {
		return contractState{}, errors.New("contract observation tip changed")
	}
	if conflict {
		return state, contractConflict
	}
	return state, nil
}

// observeRegistry accepts only exact initialization or the untouched deployer-controlled registry.
func observeRegistry(ctx context.Context, in ContractInputs, state *contractState, now time.Time) error {
	args, err := registryArguments(in)
	if err != nil {
		return err
	}
	observed, err := in.Node.ReadOnlyAt(ctx, state.info.IndexBlockID, in.Deployer, in.Deployer, "sbtc-registry", "get-current-signer-data", nil)
	if err != nil {
		return err
	}
	if observed.Type != clarity.Tuple || len(observed.Fields) != 4 {
		return errors.New("incomplete registry observation")
	}
	keys, kok := observed.Fields["current-signer-set"]
	aggregate, aok := observed.Fields["current-aggregate-pubkey"]
	threshold, tok := observed.Fields["current-signature-threshold"]
	principal, pok := observed.Fields["current-signer-principal"]
	n, err := unsigned(threshold)
	if !kok || !aok || !tok || !pok || err != nil || keys.Type != clarity.List || aggregate.Type != clarity.Buffer || principal.Type != clarity.StandardPrincipal {
		return errors.New("invalid registry observation types")
	}
	if len(keys.Items) == 0 && bytes.Equal(aggregate.Bytes, []byte{0}) && n == 0 && principal.Text == in.Deployer {
		state.registryEmpty = true
		return nil
	}
	expected, err := in.Node.ReadOnlyAt(ctx, state.info.IndexBlockID, in.Deployer, in.Deployer, "sbtc-bootstrap-signers", "pubkeys-to-principal", []clarity.Value{args[0], args[2]})
	if err != nil {
		return err
	}
	version, _, err := identity.DecodeAddress(expected.Text)
	if expected.Type != clarity.StandardPrincipal || err != nil || version != 21 {
		return errors.New("registry derived principal unavailable")
	}
	actualKeys, err := clarity.Encode(keys)
	if err != nil {
		return err
	}
	expectedKeys, _ := clarity.Encode(args[0])
	if !bytes.Equal(actualKeys, expectedKeys) || !bytes.Equal(aggregate.Bytes, args[1].Bytes) || n != in.Threshold || principal.Text != expected.Text {
		return contractConflict
	}
	state.observation = &api.ContractSetObservation{Deployer: in.Deployer, Bundle: in.Bundle, SourceDigest: foundation.Digest(in.SourceHashes), SignerPublicKeys: append([]string(nil), in.SignerPublicKeys...), AggregatePublicKey: in.AggregatePublicKey, Threshold: in.Threshold, SignerPrincipal: expected.Text, Complete: true, StacksTip: state.info.IndexBlockID, BurnHeight: state.info.BurnHeight, ObservedAt: metav1.NewTime(now.UTC())}
	return nil
}
