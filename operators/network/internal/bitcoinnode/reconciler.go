// Package bitcoinnode reconciles BitcoinNode workloads.
package bitcoinnode

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/network/api/v1alpha1"
	operatorlabels "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/labels"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/leaf"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/ports"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/workload"
)

// Reconciler owns the workload and status of BitcoinNode resources.
type Reconciler struct {
	client.Client
	APIReader client.Reader
	Scheme    *runtime.Scheme
}

// Reconcile moves one BitcoinNode toward its declared workload state.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	actor := &networkv1alpha1.BitcoinNode{}
	if err := r.Get(ctx, request.NamespacedName, actor); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	patchBase := actor.DeepCopy()
	if !actor.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	descriptor, err := describe(actor)
	permanent := err != nil
	var status networkv1alpha1.ActorStatus
	if err == nil {
		status, err = (workload.Engine{Client: r.Client, Reader: r.APIReader, Scheme: r.Scheme}).Reconcile(ctx, descriptor)
		permanent = workload.IsPermanent(err)
	}
	if leaf.IsTransientError(err) {
		return ctrl.Result{}, err
	}
	status.Conditions = append([]metav1.Condition(nil), actor.Status.Conditions...)
	if err != nil {
		status = leaf.DegradedStatus(actor.Generation, err, actor.Status.Conditions)
	} else {
		status = leaf.WithReadyCondition(status)
	}
	statusErr := r.updateStatus(ctx, actor, patchBase, status)
	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}
	return ctrl.Result{}, leaf.ReconcileError(err, permanent)
}

func describe(actor *networkv1alpha1.BitcoinNode) (workload.Descriptor, error) {
	specDigest, err := workload.SpecDigest(actor.Spec)
	if err != nil {
		return workload.Descriptor{}, fmt.Errorf("digest BitcoinNode specification: %w", err)
	}
	config := actor.Spec.Config
	if config.Generated != nil {
		if config.Generated.Profile != "bitcoin-regtest/v1" {
			return workload.Descriptor{}, fmt.Errorf("unsupported Bitcoin profile %q", config.Generated.Profile)
		}
		config = networkv1alpha1.ConfigSource{Inline: &networkv1alpha1.InlineConfig{Data: profiles.Bitcoin(portOr(actor.Spec.RPCPort, 18443)), Key: "bitcoin.conf"}}
	}
	args := []string{"-conf=" + workload.ConfigPath(config, "BitcoinNode"), "-datadir=/data", "-printtoconsole=1"}
	for _, peer := range actor.Spec.PeerRefs {
		args = append(args, "-addnode="+peer+fmt.Sprintf(":%d", portOr(actor.Spec.P2PPort, 18444)))
	}
	return workload.Descriptor{Owner: actor, Kind: "BitcoinNode", Network: actor.Spec.NetworkRef.Name, Actor: actor.Spec.ActorName, Role: string(actor.Spec.Role),
		Image: actor.Spec.Image, ImagePullPolicy: actor.Spec.ImagePullPolicy, ImagePullSecrets: actor.Spec.ImagePullSecrets,
		Config: config, SpecDigest: specDigest, Command: []string{"bitcoind"}, Args: args,
		Ports:           ports.Bitcoin(portOr(actor.Spec.RPCPort, 18443), portOr(actor.Spec.P2PPort, 18444)),
		DependencyImage: actor.Spec.DependencyImage, Workload: actor.Spec.Workload, Container: actor.Spec.Container, Suspended: actor.Spec.Suspended}, nil
}

func portOr(value, fallback int32) int32 {
	if value != 0 {
		return value
	}
	return fallback
}
func (r *Reconciler) updateStatus(ctx context.Context, actor, patchBase *networkv1alpha1.BitcoinNode, status networkv1alpha1.ActorStatus) error {
	if reflect.DeepEqual(patchBase.Status, status) {
		return nil
	}
	actor.Status = status
	return r.Status().Patch(ctx, actor, client.MergeFrom(patchBase))
}

// SetupWithManager registers BitcoinNode and owned workload watches.
func (r *Reconciler) SetupWithManager(manager ctrl.Manager, concurrency int) error {
	if r.Client == nil || r.APIReader == nil || r.Scheme == nil {
		return fmt.Errorf("BitcoinNode reconciler requires client, uncached reader, and scheme")
	}
	return ctrl.NewControllerManagedBy(manager).For(&networkv1alpha1.BitcoinNode{}).Owns(&appsv1.StatefulSet{}).Owns(&corev1.Service{}).Owns(&corev1.ConfigMap{}).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(leaf.PodRequest(operatorlabels.Bitcoin))).WithOptions(controller.Options{MaxConcurrentReconciles: concurrency}).Complete(r)
}
