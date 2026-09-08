package accountledger

import (
	"context"
	"fmt"
	"reflect"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/ingress"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Admitter binds account use to current compiled declarations and a qualified ingress.
type Admitter struct {
	// Reader must bypass the cache for account and runtime authority checks.
	Reader client.Reader
}

// Admit validates authority independently of unrelated actor readiness.
func (a Admitter) Admit(ctx context.Context, account *stacks.StacksAccount, consumerUID string, mutation bool) (Target, error) {
	parent := &network.StacksNetwork{}
	if err := a.Reader.Get(ctx, client.ObjectKey{Namespace: account.Namespace, Name: account.Spec.NetworkName}, parent); err != nil {
		return Target{}, fmt.Errorf("managed account parent cannot be read")
	}
	if string(parent.UID) != account.Spec.NetworkUID || !parent.DeletionTimestamp.IsZero() || !metav1.IsControlledBy(account, parent) || !pinned(parent, "StacksAccount", account.Name, string(account.UID)) {
		return Target{}, fmt.Errorf("managed account is not pinned to the current environment")
	}
	current := &stacks.StacksAccount{}
	if err := a.Reader.Get(ctx, client.ObjectKeyFromObject(account), current); err != nil || current.UID != account.UID || current.ResourceVersion != account.ResourceVersion || !current.DeletionTimestamp.IsZero() {
		return Target{}, fmt.Errorf("managed account changed during admission")
	}
	var consumer client.Object
	prefix := ""
	switch account.Spec.Policy.ConsumerKind {
	case "StacksContractSet":
		consumer, prefix = &stacks.StacksContractSet{}, "contracts"
	case "StacksStackingParticipant":
		consumer, prefix = &stacks.StacksStackingParticipant{}, "stacking"
	default:
		return Target{}, fmt.Errorf("unsupported account consumer")
	}
	name := naming.Child(parent.Name, prefix+"-"+account.Spec.Policy.Consumer)
	if err := a.Reader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: name}, consumer); err != nil {
		return Target{}, fmt.Errorf("managed account consumer cannot be read")
	}
	if string(consumer.GetUID()) != consumerUID || consumerUID == "" || !metav1.IsControlledBy(consumer, parent) || !pinned(parent, account.Spec.Policy.ConsumerKind, name, consumerUID) {
		return Target{}, fmt.Errorf("managed account consumer identity changed")
	}
	if mutation {
		if parent.Spec.Suspended || parent.Spec.Operation == nil || parent.Spec.Operation.Paused || !consumer.GetDeletionTimestamp().IsZero() {
			return Target{}, fmt.Errorf("managed operation is paused or withdrawn")
		}
		declared := false
		for _, p := range parent.Spec.Operation.Accounts {
			if p.Name == account.Spec.Policy.Name && reflect.DeepEqual(p, account.Spec.Policy) {
				declared = true
			}
		}
		if !declared || !consumerCurrent(parent, consumer) {
			return Target{}, fmt.Errorf("managed account or consumer declaration is not current")
		}
	}
	target, err := ingress.Admit(ctx, a.Reader, parent, account.Spec.Policy.Target, account.Spec.Policy.ConfigDigest)
	if err != nil {
		return Target{}, err
	}
	return Target{Endpoint: target.Endpoint, ActorUID: target.ActorUID, PodUID: target.PodUID, ContainerID: target.ContainerID}, nil
}

// pinned checks the retained incarnation independently of aggregate inventory readiness.
func pinned(parent *network.StacksNetwork, kind, name, uid string) bool {
	if uid == "" {
		return false
	}
	for _, identity := range parent.Status.Capabilities {
		if identity.Kind == kind && identity.Name == name {
			return identity.UID == uid
		}
	}
	return false
}

// consumerCurrent checks the actual consumer policy, including pause, before signing.
func consumerCurrent(parent *network.StacksNetwork, consumer client.Object) bool {
	switch c := consumer.(type) {
	case *stacks.StacksContractSet:
		if c.Spec.NetworkUID != string(parent.UID) || c.Spec.NetworkName != parent.Name || c.Spec.Policy.Paused {
			return false
		}
		for _, p := range parent.Spec.Operation.ContractSets {
			if reflect.DeepEqual(p, c.Spec.Policy) {
				return true
			}
		}
	case *stacks.StacksStackingParticipant:
		if c.Spec.NetworkUID != string(parent.UID) || c.Spec.NetworkName != parent.Name || c.Spec.Policy.Paused {
			return false
		}
		for _, p := range parent.Spec.Operation.Participants {
			if reflect.DeepEqual(p, c.Spec.Policy) {
				return true
			}
		}
	}
	return false
}
