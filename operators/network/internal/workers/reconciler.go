package workers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Reconciler owns Kubernetes execution resources for one capability kind, without signing or RPC access.
type Reconciler struct {
	// Client writes workloads; Reader supplies uncached authority and ownership reads.
	Client client.Client
	Reader client.Reader
	// Scheme resolves controller owner references.
	Scheme *runtime.Scheme
	// Component identifies the capability kind managed by this controller.
	Component string
	// Settings supplies operator-selected worker images and execution features.
	Settings Settings
}

// object returns the capability type watched by this controller.
func (r *Reconciler) object() client.Object {
	switch r.Component {
	case "bitcoin-production":
		return &bitcoin.BitcoinProductionTarget{}
	case "stacks-transactions":
		return &stacks.StacksTransactionProduction{}
	case "stacks-contracts":
		return &stacks.StacksContractSet{}
	case "stacks-stacking":
		return &stacks.StacksStackingParticipant{}
	case "stacks-receipts":
		return &network.StacksNetwork{}
	default:
		panic("unsupported workload component")
	}
}

// SetupWithManager registers a capability watch and owned-workload recovery watches.
func (r *Reconciler) SetupWithManager(m ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(m).Named("workload-" + r.Component).For(r.object()).
		Owns(&apps.Deployment{}).Owns(&core.ServiceAccount{}).Owns(&rbac.Role{}).Owns(&rbac.RoleBinding{}).Owns(&core.Service{}).Complete(r)
}

// Reconcile restores owned workloads without changing execution ledgers or network pause policy.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	object := r.object()
	if err := r.Reader.Get(ctx, req.NamespacedName, object); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	p, err := r.describe(ctx, object)
	if err != nil {
		if reportErr := r.condition(ctx, object, false, "ConfigurationUnavailable", err.Error()); reportErr != nil {
			return ctrl.Result{}, reportErr
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	if p == nil {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	permissions, err := rules(*p, r.Settings)
	if err != nil {
		return ctrl.Result{}, err
	}
	name := Name(p.owner, p.component)
	no := false
	resources := []client.Object{
		&core.ServiceAccount{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: object.GetNamespace()}, AutomountServiceAccountToken: &no},
		&rbac.Role{TypeMeta: metav1.TypeMeta{APIVersion: rbac.SchemeGroupVersion.String(), Kind: "Role"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: object.GetNamespace()}, Rules: permissions},
		&rbac.RoleBinding{TypeMeta: metav1.TypeMeta{APIVersion: rbac.SchemeGroupVersion.String(), Kind: "RoleBinding"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: object.GetNamespace()}, Subjects: []rbac.Subject{{Kind: "ServiceAccount", Name: name, Namespace: object.GetNamespace()}}, RoleRef: rbac.RoleRef{APIGroup: rbac.GroupName, Kind: "Role", Name: name}},
	}
	if p.component == "stacks-receipts" {
		resources = append(resources, &core.Service{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"}, ObjectMeta: metav1.ObjectMeta{Name: naming.Child(p.network, "receipts"), Namespace: object.GetNamespace()}, Spec: core.ServiceSpec{Selector: map[string]string{"app.kubernetes.io/name": p.component, "network.stacks.org/network-uid": p.networkUID}, Ports: []core.ServicePort{{Name: "receipts", Port: 8082, TargetPort: intstr.FromInt32(8082)}}}})
	}
	d := deployment(*p, r.Settings)
	d.TypeMeta = metav1.TypeMeta{APIVersion: apps.SchemeGroupVersion.String(), Kind: "Deployment"}
	resources = append(resources, d)
	for _, desired := range resources {
		if err := r.apply(ctx, object, desired); err != nil {
			_ = r.condition(ctx, object, false, "WorkloadUnavailable", "Execution resources could not be reconciled; inspect operator logs")
			return ctrl.Result{}, err
		}
	}
	current := &apps.Deployment{}
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(d), current); err != nil {
		return ctrl.Result{}, err
	}
	ready := current.Status.ObservedGeneration == current.Generation && current.Status.AvailableReplicas == 1 && current.Status.UpdatedReplicas == 1
	reason, message := "Starting", "Waiting for the owned execution Deployment"
	if ready {
		reason, message = "Available", "Execution Deployment is available; protocol readiness is reported separately"
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, r.condition(ctx, object, ready, reason, message)
}

// apply enforces declared fields only on the current owned incarnation, preserving API defaults.
func (r *Reconciler) apply(ctx context.Context, owner, desired client.Object) error {
	if err := controllerutil.SetControllerReference(owner, desired, r.Scheme); err != nil {
		return err
	}
	current := desired.DeepCopyObject().(client.Object)
	err := r.Reader.Get(ctx, client.ObjectKeyFromObject(desired), current)
	if apierrors.IsNotFound(err) {
		return r.Client.Create(ctx, desired, client.FieldOwner("stacks-workloads"))
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(current, owner) || !current.GetDeletionTimestamp().IsZero() {
		return fmt.Errorf("execution resource %s is foreign or terminating", current.GetName())
	}
	if desiredDeployment, ok := desired.(*apps.Deployment); ok {
		live := current.(*apps.Deployment)
		base := live.DeepCopy()
		// Create and Apply can share field ownership. Explicit CAS replacement of
		// the credential projection prevents omitted mounts surviving withdrawal.
		live.Spec.Template.Spec.Volumes = desiredDeployment.Spec.Template.Spec.Volumes
		live.Spec.Template.Spec.ImagePullSecrets = desiredDeployment.Spec.Template.Spec.ImagePullSecrets
		for i := range live.Spec.Template.Spec.Containers {
			for _, declared := range desiredDeployment.Spec.Template.Spec.Containers {
				if live.Spec.Template.Spec.Containers[i].Name == declared.Name {
					live.Spec.Template.Spec.Containers[i].VolumeMounts = declared.VolumeMounts
				}
			}
		}
		if !reflect.DeepEqual(base.Spec, live.Spec) {
			if err := r.Client.Patch(ctx, live, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
				return err
			}
		}
	}
	desired.SetResourceVersion(current.GetResourceVersion())
	return r.Client.Patch(ctx, desired, client.Apply, client.FieldOwner("stacks-workloads"), client.ForceOwnership)
}

// describe validates aggregate pins before selecting any credential mount.
func (r *Reconciler) describe(ctx context.Context, object client.Object) (*plan, error) {
	p := &plan{owner: object, component: r.Component}
	var parent *network.StacksNetwork
	switch o := object.(type) {
	case *network.StacksNetwork:
		parent = o
		p.network = o.Name
		p.networkUID = string(o.UID)
	case *bitcoin.BitcoinProductionTarget:
		p.network = o.Spec.NetworkName
		p.networkUID = o.Spec.NetworkUID
		p.credentials = o.Spec.Policy.CredentialsSecret
	case *stacks.StacksTransactionProduction:
		p.network = o.Spec.NetworkName
		p.networkUID = o.Spec.NetworkUID
		p.credentials = o.Spec.Policy.CredentialsSecret
	case *stacks.StacksContractSet:
		p.network = o.Spec.NetworkName
		p.networkUID = o.Spec.NetworkUID
	case *stacks.StacksStackingParticipant:
		p.network = o.Spec.NetworkName
		p.networkUID = o.Spec.NetworkUID
	}
	if parent == nil {
		parent = &network.StacksNetwork{}
		if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: p.network}, parent); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
	}
	if string(parent.UID) != p.networkUID || !parent.DeletionTimestamp.IsZero() {
		return nil, nil
	}
	switch o := object.(type) {
	case *bitcoin.BitcoinProductionTarget:
		root := &bitcoin.BitcoinBlockProduction{}
		if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: o.Namespace, Name: p.network}, root); err != nil {
			return nil, err
		}
		if !metav1.IsControlledBy(root, parent) || parent.Status.BitcoinProductionUID != string(root.UID) || !root.Binds(o) {
			return nil, fmt.Errorf("production ledger is not pinned")
		}
		if root.Spec.Policy.CredentialsSecret != p.credentials {
			return nil, fmt.Errorf("production credentials are not current")
		}
	case *stacks.StacksTransactionProduction:
		if !metav1.IsControlledBy(o, parent) || parent.Status.TransactionProductionUID != string(o.UID) {
			return nil, fmt.Errorf("transaction ledger is not pinned")
		}
		if parent.Spec.StacksTransactionProduction != nil && !reflect.DeepEqual(*parent.Spec.StacksTransactionProduction, o.Spec.Policy) {
			return nil, fmt.Errorf("transfer policy is not current")
		}
	case *stacks.StacksContractSet:
		if !pinned(parent, o, "StacksContractSet") {
			return nil, fmt.Errorf("contract capability is not pinned")
		}
		current := false
		if parent.Spec.Operation != nil {
			for _, policy := range parent.Spec.Operation.ContractSets {
				if reflect.DeepEqual(policy, o.Spec.Policy) {
					current = true
				}
			}
		}
		account, err := r.account(ctx, parent, o.Spec.Policy.Account, "StacksContractSet", o.Spec.Policy.Name, current)
		if err != nil {
			return nil, err
		}
		p.accounts = []string{account.Name}
		if current {
			p.keys = []stacks.ArtifactReference{account.Spec.Policy.KeySecretRef}
		}
	case *stacks.StacksStackingParticipant:
		if !pinned(parent, o, "StacksStackingParticipant") {
			return nil, fmt.Errorf("stacking capability is not pinned")
		}
		current := false
		if parent.Spec.Operation != nil {
			for _, policy := range parent.Spec.Operation.Participants {
				if reflect.DeepEqual(policy, o.Spec.Policy) {
					current = true
				}
			}
		}
		for _, name := range []string{o.Spec.Policy.HolderAccount, o.Spec.Policy.AdministratorAccount} {
			account, err := r.account(ctx, parent, name, "StacksStackingParticipant", o.Spec.Policy.Name, current)
			if err != nil {
				return nil, err
			}
			p.accounts = append(p.accounts, account.Name)
			if current {
				p.keys = append(p.keys, account.Spec.Policy.KeySecretRef)
			}
		}
		if current {
			p.keys = append(p.keys, o.Spec.Policy.ConsensusKeySecretRef)
		}
	case *network.StacksNetwork:
		for _, pin := range o.Status.Capabilities {
			if pin.Kind == "StacksAccount" {
				p.accounts = append(p.accounts, pin.Name)
			}
		}
		if len(p.accounts) == 0 {
			return nil, nil
		}
	}
	if (p.component == "bitcoin-production" || p.component == "stacks-transactions") && p.credentials == "" {
		return nil, fmt.Errorf("capability credentialsSecret is required to provision its worker")
	}
	return p, nil
}

// pinned checks the parent's retained incarnation and direct ownership.
func pinned(parent *network.StacksNetwork, child client.Object, kind string) bool {
	if !metav1.IsControlledBy(child, parent) {
		return false
	}
	for _, pin := range parent.Status.Capabilities {
		if pin.Kind == kind && pin.Name == child.GetName() && pin.UID == string(child.GetUID()) {
			return true
		}
	}
	return false
}

// account verifies retained assignment; signing additionally requires a current declaration.
func (r *Reconciler) account(ctx context.Context, parent *network.StacksNetwork, name, kind, consumer string, current bool) (*stacks.StacksAccount, error) {
	account := &stacks.StacksAccount{}
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: naming.Child(parent.Name, "account-"+name)}, account); err != nil {
		return nil, err
	}
	if !pinned(parent, account, "StacksAccount") || account.Spec.NetworkUID != string(parent.UID) || !account.DeletionTimestamp.IsZero() || account.Spec.Policy.ConsumerKind != kind || account.Spec.Policy.Consumer != consumer {
		return nil, fmt.Errorf("signing account is not assigned to this capability")
	}
	if !current {
		return account, nil
	}
	if parent.Spec.Operation == nil {
		return nil, fmt.Errorf("signing account declaration is absent")
	}
	for _, policy := range parent.Spec.Operation.Accounts {
		if reflect.DeepEqual(policy, account.Spec.Policy) {
			return account, nil
		}
	}
	return nil, fmt.Errorf("signing account declaration is not current")
}

// condition reports workload availability without overwriting executor-owned evidence.
func (r *Reconciler) condition(ctx context.Context, object client.Object, ready bool, reason, message string) error {
	if _, network := object.(*network.StacksNetwork); network {
		return nil
	}
	current := object.DeepCopyObject().(client.Object)
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(object), current); err != nil {
		return client.IgnoreNotFound(err)
	}
	if current.GetUID() != object.GetUID() {
		return nil
	}
	before := current.DeepCopyObject().(client.Object)
	var conditions *[]metav1.Condition
	switch o := current.(type) {
	case *bitcoin.BitcoinProductionTarget:
		conditions = &o.Status.Conditions
	case *stacks.StacksTransactionProduction:
		conditions = &o.Status.Conditions
	case *stacks.StacksContractSet:
		conditions = &o.Status.Conditions
	case *stacks.StacksStackingParticipant:
		conditions = &o.Status.Conditions
	}
	prior := append([]metav1.Condition(nil), (*conditions)...)
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	if len(message) > 512 {
		message = message[:512]
	}
	meta.SetStatusCondition(conditions, metav1.Condition{Type: "WorkerReady", Status: status, Reason: reason, Message: message, ObservedGeneration: current.GetGeneration()})
	if reflect.DeepEqual(prior, *conditions) {
		return nil
	}
	return r.Client.Status().Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}
