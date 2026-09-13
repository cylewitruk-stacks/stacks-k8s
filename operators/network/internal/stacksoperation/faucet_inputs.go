package stacksoperation

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FaucetNode exposes the native account, submission, inclusion and epoch reads needed by requests.
type FaucetNode interface {
	Node
	Info(context.Context) (rpc.Info, error)
}

// FaucetInputs supplies native ingress without rediscovering the immutable request destination.
type FaucetInputs struct {
	// Node exposes native account, transaction and protocol identity observations.
	Node FaucetNode
	// StartHeight is the frozen epoch-3 activation boundary.
	StartHeight uint64
}

// Faucet fresh-resolves fixed source and target identities; request limits were fixed at admission.
func (r PublicInputs) Faucet(ctx context.Context, s stacksworker.Snapshot, admission *stacks.FaucetAdmission) (FaucetInputs, error) {
	var input FaucetInputs
	if r.Reader == nil || s.Network == nil || s.Participant == nil || s.Participant.Status.Admission == nil || admission == nil || admission.SourceAccount == nil || admission.Target == nil {
		return input, errors.New("faucet inputs unavailable")
	}
	policy := s.Participant.Status.Admission.Configuration.StacksFaucet
	if policy == nil || policy.AccountRef == nil || policy.TargetNodeRef == nil {
		return input, errors.New("faucet policy unavailable")
	}
	source, err := r.account(ctx, s.Participant, policy.AccountRef.Name)
	if err != nil {
		return input, err
	}
	if source.Name != admission.SourceAccount.Name || source.UID != admission.SourceAccount.UID || source.Status.Digest != admission.SourceAccount.Fingerprint || source.Status.Identity.Address != r.Sender {
		return input, errors.New("faucet source identity changed")
	}
	target, err := r.target(ctx, s, policy.TargetNodeRef.Name)
	if err != nil {
		return input, err
	}
	if target.Name != admission.Target.Name || target.UID != admission.Target.UID {
		return input, errors.New("faucet target identity changed")
	}
	endpoint := ""
	for _, ep := range target.Status.Runtime.Endpoints {
		if ep.Name == common.EndpointRPC && ep.Host != "" && ep.Port > 0 && ep.Port <= 65535 {
			endpoint = (&url.URL{Scheme: "http", Host: net.JoinHostPort(ep.Host, strconv.Itoa(int(ep.Port)))}).String()
		}
	}
	node, err := rpc.New(rpc.Config{Endpoint: endpoint, Timeout: 10 * time.Second, MaxResponseBytes: 2 << 20})
	if err != nil {
		return input, err
	}
	ref := s.Network.Status.GenesisRef
	if ref == nil {
		return input, errors.New("faucet genesis unavailable")
	}
	var genesis api.StacksGenesis
	if err = r.Reader.Get(ctx, client.ObjectKey{Namespace: s.Network.Namespace, Name: ref.Name}, &genesis); err != nil {
		return input, err
	}
	if genesis.UID != ref.UID || genesis.DeletionTimestamp != nil || !metav1.IsControlledBy(&genesis, s.Network) || genesis.Spec.Source.NetworkUID != s.Network.UID || foundation.Digest(genesis.Spec) != ref.Fingerprint || foundation.Digest(genesis.Spec.Chain) != s.Network.Status.GenesisDigest {
		return input, errors.New("faucet genesis identity changed")
	}
	for _, epoch := range genesis.Spec.Chain.Epochs {
		if epoch.Name == "3.0" && epoch.StartHeight > 0 {
			return FaucetInputs{Node: node, StartHeight: uint64(epoch.StartHeight)}, nil
		}
	}
	return input, errors.New("faucet requires frozen Nakamoto activation")
}
