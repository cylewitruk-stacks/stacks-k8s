package transactions

import (
	"context"
	"fmt"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/ingress"
	"reflect"
	"time"

	stacksv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// admittedTarget pins the address and runtime identity used by one dispatch.
type admittedTarget struct {
	// endpoint is the admitted Pod's RPC address, without mutable Service resolution.
	endpoint string
	// actorUID identifies the StacksNode incarnation.
	actorUID string
	// podUID identifies the admitted Pod incarnation.
	podUID string
	// containerID identifies the container reported by kubelet at admission.
	containerID string
}

// admit uses direct reads and validates only this capability's required actor.
func (r *Reconciler) admit(ctx context.Context, policy *stacksv1alpha1.StacksTransactionProduction, parent *networkv1alpha1.StacksNetwork) (admittedTarget, error) {
	var zero admittedTarget
	if !metav1.IsControlledBy(policy, parent) || policy.Spec.NetworkUID != string(parent.UID) || parent.Status.TransactionProductionUID != string(policy.UID) {
		return zero, fmt.Errorf("production ledger is not bound to this network")
	}
	if !policy.Status.Outstanding && (parent.Spec.StacksTransactionProduction == nil || !reflect.DeepEqual(*parent.Spec.StacksTransactionProduction, policy.Spec.Policy)) {
		return zero, fmt.Errorf("compiled production policy is not current")
	}
	target, err := ingress.Admit(ctx, r.APIReader, parent, policy.Spec.Policy.Target, r.Profile.ConfigDigest)
	if err != nil {
		return zero, err
	}
	if !policy.Status.Outstanding && policy.Spec.Policy.MinimumBurnHeight > 0 {
		observer, ok := r.RPC.(interface {
			Height(context.Context, string) (int64, error)
		})
		if !ok {
			return zero, fmt.Errorf("transfer profile requires burn-height observation")
		}
		read, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		height, err := observer.Height(read, target.Endpoint)
		if err != nil || height < policy.Spec.Policy.MinimumBurnHeight {
			return zero, fmt.Errorf("waiting for transfer minimum burn height")
		}
	}
	return admittedTarget{endpoint: target.Endpoint, actorUID: target.ActorUID, podUID: target.PodUID, containerID: target.ContainerID}, nil
}
