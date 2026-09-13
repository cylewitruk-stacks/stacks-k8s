package bitcoincontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoinrpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RunWorker loads only mounted RPC credentials before starting the scoped loop.
func RunWorker(ctx context.Context, c client.Client, reader client.Reader, input WorkerInput) error {
	username, e := os.ReadFile("/rpc/username")
	if e != nil {
		return fmt.Errorf("control RPC username unavailable")
	}
	password, e := os.ReadFile("/rpc/password")
	if e != nil {
		return fmt.Errorf("control RPC password unavailable")
	}
	if len(username) == 0 || len(password) == 0 || len(username) > 256 || len(password) > 4096 {
		return fmt.Errorf("control RPC credentials malformed")
	}
	rpc := bitcoinrpc.New(bitcoinrpc.Credentials{Username: string(username), Password: string(password)})
	clear(username)
	clear(password)
	worker := &Worker{Client: c, Reader: reader, RPC: rpc, Input: input}
	return worker.Run(ctx)
}

// WorkloadReconciler provisions scoped per-node control Deployments.
type WorkloadReconciler struct {
	// ActionsEnabled enables finite generation requests in scoped workers.
	ActionsEnabled bool
	// ReorganizationEnabled enables bounded suffix replacement in scoped workers.
	ReorganizationEnabled bool
	// Client writes participant-owned support resources.
	Client client.Client
	// Reader validates current enrollment.
	Reader client.Reader
	// Image contains the installed Go control binary.
	Image string
}

// SetupWithManager installs the workload controller independently of actors.
func (r *WorkloadReconciler) SetupWithManager(m ctrl.Manager) error {
	return r.registerWorkloadWatches(m)
}

// enqueue routes exact root allocations without reading a namespace inventory.
func (r *WorkloadReconciler) enqueue(_ context.Context, obj client.Object) []ctrl.Request {
	root, ok := obj.(*api.StacksNetwork)
	if !ok {
		return nil
	}
	return workloadRequests(root)
}

// Reconcile requires durable record enrollment before creating control Pods.
func (r *WorkloadReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	p := &api.StacksNetworkParticipant{}
	if e := r.Reader.Get(ctx, request.NamespacedName, p); e != nil {
		return ctrl.Result{}, client.IgnoreNotFound(e)
	}
	if p.Spec.Kind != api.ParticipantBitcoinNode {
		return ctrl.Result{}, nil
	}
	root := &api.StacksNetwork{}
	if e := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "network"}, root); e != nil {
		return ctrl.Result{}, e
	}
	if root.UID != p.Spec.NetworkUID {
		return ctrl.Result{}, fmt.Errorf("owning network identity differs")
	}
	lifecycleResult, e := r.ReconcileControlLifecycle(ctx, p, root)
	if e != nil {
		return ctrl.Result{}, e
	}
	if stopReason(root, p) != "" {
		return r.stopControlWorkload(ctx, root, p)
	}
	if root.Status.Bitcoin == nil || root.Status.Bitcoin.InitializationRef == nil || p.Status.Runtime == nil ||
		p.Status.Runtime.RPCSecretRef == nil ||
		p.Status.Runtime.ConfigRef == nil ||
		p.Status.Runtime.PodRef == nil {
		return lifecycleResult, nil
	}
	var record *bitcoin.BitcoinExecution
	for _, ref := range root.Status.Bitcoin.ExecutionRefs {
		candidate := &bitcoin.BitcoinExecution{}
		if e := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: ref.Name}, candidate); e != nil {
			return ctrl.Result{}, e
		}
		if candidate.UID != ref.UID {
			return ctrl.Result{}, fmt.Errorf("pinned control record replaced")
		}
		if candidate.Spec.Participant.UID == p.UID {
			record = candidate
			break
		}
	}
	if record == nil || record.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(record, root) {
		return lifecycleResult, nil
	}
	init := &bitcoin.BitcoinInitialization{}
	if e := r.Reader.Get(
		ctx,
		client.ObjectKey{Namespace: p.Namespace, Name: root.Status.Bitcoin.InitializationRef.Name},
		init,
	); e != nil {
		return ctrl.Result{}, e
	}
	if init.UID != root.Status.Bitcoin.InitializationRef.UID {
		return ctrl.Result{}, fmt.Errorf("initialization record replaced")
	}
	replicas := int32(1)
	var genesis api.StacksGenesis
	if e := r.Reader.Get(
		ctx,
		client.ObjectKey{Namespace: p.Namespace, Name: init.Spec.Genesis.Name},
		&genesis,
	); e != nil {
		return ctrl.Result{}, e
	}
	if genesis.UID != init.Spec.Genesis.UID || !metav1.IsControlledBy(&genesis, root) {
		return ctrl.Result{}, fmt.Errorf("frozen worker cohort unavailable")
	}
	reads, e := publicReadScope(ctx, r.Reader, p, init)
	if e != nil {
		return ctrl.Result{}, e
	}
	objects, e := workerResourcesWithActions(
		p,
		record,
		init,
		r.Image,
		replicas,
		reads,
		r.ActionsEnabled,
		r.ReorganizationEnabled,
		&genesis,
	)
	if e != nil {
		return ctrl.Result{}, e
	}
	for _, desired := range objects {
		current := desired.DeepCopyObject().(client.Object)
		e := r.Reader.Get(ctx, client.ObjectKeyFromObject(desired), current)
		if apierrors.IsNotFound(e) {
			if stopReason(root, p) != "" {
				continue
			}
			if e = r.Client.Create(ctx, desired); e != nil {
				return ctrl.Result{}, e
			}
			continue
		}
		if e != nil {
			return ctrl.Result{}, e
		}
		if !metav1.IsControlledBy(current, p) {
			return ctrl.Result{}, fmt.Errorf("control workload ownership differs")
		}
		base := current.DeepCopyObject().(client.Object)
		current.SetLabels(desired.GetLabels())
		switch got := current.(type) {
		case *appsv1.Deployment:
			want := desired.(*appsv1.Deployment)
			got.Spec.Template = want.Spec.Template
			if stopReason(root, p) != "" && ptr.Deref(got.Spec.Replicas, 1) == 0 {
				want.Spec.Replicas = ptr.To[int32](0)
			}
			got.Spec.Replicas = want.Spec.Replicas
			got.Spec.Strategy = want.Spec.Strategy
			if !equality.Semantic.DeepEqual(got.Spec.Selector, want.Spec.Selector) {
				return ctrl.Result{}, fmt.Errorf("control Deployment selector differs")
			}
		case *rbacv1.Role:
			got.Rules = desired.(*rbacv1.Role).Rules
		case *rbacv1.RoleBinding:
			want := desired.(*rbacv1.RoleBinding)
			if got.RoleRef != want.RoleRef {
				return ctrl.Result{}, fmt.Errorf("control role binding changed")
			}
			got.Subjects = want.Subjects
		case *corev1.ServiceAccount:
			got.AutomountServiceAccountToken = desired.(*corev1.ServiceAccount).AutomountServiceAccountToken
		}
		if !equality.Semantic.DeepEqual(base, current) {
			if e = r.Client.Patch(
				ctx,
				current,
				client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}),
			); e != nil {
				return ctrl.Result{}, e
			}
		}
	}
	return r.ReconcileControlLifecycle(ctx, p, root)
}

// WorkerResources renders exact-name permissions and one immutable credential mount.
func WorkerResources(
	p *api.StacksNetworkParticipant,
	record *bitcoin.BitcoinExecution,
	init *bitcoin.BitcoinInitialization,
	image string,
	replicas int32,
	genesis ...*api.StacksGenesis,
) ([]client.Object, error) {
	return workerResources(p, record, init, image, replicas, nil, genesis...)
}

// workerResources includes controller-resolved public read scope without adding secret authority.
func workerResources(
	p *api.StacksNetworkParticipant,
	record *bitcoin.BitcoinExecution,
	init *bitcoin.BitcoinInitialization,
	image string,
	replicas int32,
	reads []common.Binding,
	genesis ...*api.StacksGenesis,
) ([]client.Object, error) {
	return workerResourcesWithActions(p, record, init, image, replicas, reads, false, false, genesis...)
}

// workerResourcesWithActions grants only explicitly enabled finite request reads.
func workerResourcesWithActions(
	p *api.StacksNetworkParticipant,
	record *bitcoin.BitcoinExecution,
	init *bitcoin.BitcoinInitialization,
	image string,
	replicas int32,
	reads []common.Binding,
	generation, reorganization bool,
	genesis ...*api.StacksGenesis,
) ([]client.Object, error) {
	rt := p.Status.Runtime
	if rt == nil || rt.RPCSecretRef == nil || rt.ConfigRef == nil || rt.PodRef == nil || image == "" ||
		p.Status.Admission == nil ||
		p.Status.Admission.Configuration.BitcoinNode == nil {
		return nil, fmt.Errorf("control workload inputs unavailable")
	}
	name := naming.RuntimeName(
		string(p.Spec.NetworkUID),
		string(p.UID),
		string(api.ParticipantBitcoinNode),
		p.Spec.ParticipantName,
		"control",
	)
	meta := metav1.ObjectMeta{
		Name:      name,
		Namespace: p.Namespace,
		Labels:    labels(p, api.RoleSupport),
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion:         api.GroupVersion.String(),
				Kind:               api.KindStacksNetworkParticipant,
				Name:               p.Name,
				UID:                p.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(false),
			},
		},
	}
	input := WorkerInput{
		ActionsEnabled:        generation,
		ReorganizationEnabled: reorganization,
		Namespace:             p.Namespace,
		RecordName:            record.Name,
		RecordUID:             record.UID,
		CredentialsName:       rt.RPCSecretRef.Name,
		CredentialsUID:        rt.RPCSecretRef.UID,
	}
	raw, _ := json.Marshal(input)
	rules := []rbacv1.PolicyRule{}
	for resource, enabled := range map[string]bool{
		action.ResourceBitcoinBlockGeneration: generation,
		action.ResourceBitcoinReorganization:  reorganization,
	} {
		if enabled {
			rules = append(
				rules,
				rbacv1.PolicyRule{
					APIGroups: []string{action.GroupVersion.Group},
					Resources: []string{resource},
					Verbs:     []string{"get", "list"},
				},
			)
		}
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Resources[0] < rules[j].Resources[0] })
	rule := func(group, resource string, names []string, verbs ...string) {
		names = sortedUnique(names)
		if len(names) > 0 {
			rules = append(
				rules,
				rbacv1.PolicyRule{
					APIGroups:     []string{group},
					Resources:     []string{resource},
					ResourceNames: names,
					Verbs:         verbs,
				},
			)
		}
	}
	rule(api.GroupVersion.Group, api.ResourceStacksNetwork, []string{"network"}, "get")
	participants := []string{p.Name, productionBinding(init).Name}
	if len(genesis) > 1 {
		return nil, fmt.Errorf("multiple frozen worker cohorts")
	}
	if len(genesis) == 1 && genesis[0] != nil {
		if genesis[0].UID != init.Spec.Genesis.UID {
			return nil, fmt.Errorf("frozen worker cohort differs")
		}
		for _, required := range genesis[0].Spec.Bootstrap.Requirements {
			if required.Participant.UID == init.Spec.Production.UID {
				continue
			}
			participants = append(participants, required.Participant.Name)
		}
	}
	rule(api.GroupVersion.Group, api.ResourceStacksNetworkParticipant, participants, "get")
	rule(api.GroupVersion.Group, api.ResourceStacksGenesis, []string{init.Spec.Genesis.Name}, "get")
	rule(bitcoin.GroupVersion.Group, bitcoin.ResourceBitcoinExecution, []string{record.Name}, "get")
	rule(bitcoin.GroupVersion.Group, bitcoin.ResourceBitcoinExecution+"/status", []string{record.Name}, "update")
	rule(bitcoin.GroupVersion.Group, bitcoin.ResourceBitcoinInitialization, []string{init.Name}, "get")
	rule("", "secrets", []string{rt.RPCSecretRef.Name, rt.ConfigRef.Name}, "get")
	rule("", "pods", []string{rt.PodRef.Name}, "get")
	workloads := []string{}
	for _, ref := range rt.WorkloadRefs {
		if ref.Kind == common.KindStatefulSet {
			workloads = append(workloads, ref.Name)
		}
	}
	rule("apps", "statefulsets", workloads, "get")
	services := []string{}
	for _, endpoint := range rt.Endpoints {
		if endpoint.Name == common.EndpointRPC {
			services = append(services, strings.Split(endpoint.Host, ".")[0])
		}
	}
	rule("", "services", services, "get")
	wallets := []string{}
	for _, ref := range ptr.Deref(p.Status.Admission.Configuration.BitcoinNode.WalletRefs, nil) {
		wallets = append(wallets, ref.Name)
	}
	rule(bitcoin.GroupVersion.Group, bitcoin.ResourceBitcoinWallet, wallets, "get")

	if active := init.Status.Override; active != nil {
		rule(
			bitcoin.GroupVersion.Group,
			bitcoin.ResourceBitcoinBlockScheduleOverride,
			[]string{active.Override.Name},
			"get",
		)
		if active.ScheduleRef != nil {
			rule(
				bitcoin.GroupVersion.Group,
				bitcoin.ResourceBitcoinBlockSchedule,
				[]string{active.ScheduleRef.Name},
				"get",
			)
		}
	}
	for _, ref := range reads {
		switch ref.Kind {
		case api.KindStacksNetworkParticipant:
			rule(api.GroupVersion.Group, api.ResourceStacksNetworkParticipant, []string{ref.Name}, "get")
		case string(api.ParticipantBitcoinNode):
			rule(bitcoin.GroupVersion.Group, bitcoin.ResourceBitcoinNode, []string{ref.Name}, "get")
		case string(api.ParticipantBitcoinBlockProduction):
			rule(bitcoin.GroupVersion.Group, bitcoin.ResourceBitcoinBlockProduction, []string{ref.Name}, "get")
		case bitcoin.KindBitcoinBlockSchedule:
			rule(bitcoin.GroupVersion.Group, bitcoin.ResourceBitcoinBlockSchedule, []string{ref.Name}, "get")
		case bitcoin.KindBitcoinWallet:
			rule(bitcoin.GroupVersion.Group, bitcoin.ResourceBitcoinWallet, []string{ref.Name}, "get")
		case stacks.KindStacksAccount:
			rule(stacks.GroupVersion.Group, stacks.ResourceStacksAccount, []string{ref.Name}, "get")
		}
	}
	sa := &corev1.ServiceAccount{ObjectMeta: meta, AutomountServiceAccountToken: ptr.To(true)}
	role := &rbacv1.Role{ObjectMeta: meta, Rules: rules}
	rb := &rbacv1.RoleBinding{
		ObjectMeta: meta,
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: common.KindRole, Name: name},
		Subjects:   []rbacv1.Subject{{Kind: common.KindServiceAccount, Name: name, Namespace: p.Namespace}},
	}
	selector := labels(p, api.RoleSupport)
	selector[workerRoleLabel] = workerRoleControl
	deployment := &appsv1.Deployment{
		ObjectMeta: meta,
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      selector,
					Finalizers:  []string{ControlPodFinalizer},
					Annotations: map[string]string{controlInputAnnotation: foundation.Digest(input)},
				},
				Spec: corev1.PodSpec{
					RestartPolicy:                 corev1.RestartPolicyAlways,
					DNSPolicy:                     corev1.DNSClusterFirst,
					SchedulerName:                 corev1.DefaultSchedulerName,
					ServiceAccountName:            name,
					DeprecatedServiceAccount:      name,
					TerminationGracePeriodSeconds: ptr.To[int64](45),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser:      ptr.To[int64](65532),
						RunAsGroup:     ptr.To[int64](65532),
						RunAsNonRoot:   ptr.To(true),
						FSGroup:        ptr.To[int64](65532),
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{
						{
							Name:                     "control",
							Image:                    image,
							TerminationMessagePath:   corev1.TerminationMessagePathDefault,
							TerminationMessagePolicy: corev1.TerminationMessageReadFile,
							ImagePullPolicy:          corev1.PullIfNotPresent,
							Args: []string{
								"--mode=" + ModeBitcoinControl,
								"--input=" + string(raw),
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								ReadOnlyRootFilesystem:   ptr.To(true),
								Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: rpcVolumeName, MountPath: "/rpc", ReadOnly: true},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: rpcVolumeName,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  rt.RPCSecretRef.Name,
									DefaultMode: ptr.To[int32](0o440),
								},
							},
						},
					},
				},
			},
		},
	}
	if placement := p.Status.Admission.Configuration.BitcoinNode.WorkerPlacement; placement != nil {
		deployment.Spec.Template.Spec.NodeSelector = placement.NodeSelector
		deployment.Spec.Template.Spec.Tolerations = ptr.Deref(placement.Tolerations, nil)
	}
	return []client.Object{sa, role, rb, deployment}, nil
}

// sortedUnique stabilizes exact-name RBAC rendering.
func sortedUnique(values []string) []string {
	sort.Strings(values)
	out := []string{}
	for _, v := range values {
		if v != "" && (len(out) == 0 || out[len(out)-1] != v) {
			out = append(out, v)
		}
	}
	return out
}

// stopControlWorkload changes only replica intent after acknowledgement, without re-rendering inputs.
func (r *WorkloadReconciler) stopControlWorkload(
	ctx context.Context,
	root *api.StacksNetwork,
	p *api.StacksNetworkParticipant,
) (ctrl.Result, error) {
	result := ctrl.Result{RequeueAfter: 2 * time.Second}
	var deployment appsv1.Deployment
	if err := r.Reader.Get(
		ctx,
		client.ObjectKey{Namespace: p.Namespace, Name: controlDeploymentName(p)},
		&deployment,
	); err != nil {
		return result, client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(&deployment, p) {
		return result, fmt.Errorf("control workload ownership differs")
	}
	if ptr.Deref(deployment.Spec.Replicas, 1) == 0 {
		return r.ReconcileControlLifecycle(ctx, p, root)
	}
	ready, err := CheckDrained(ctx, r.Reader, p)
	if err != nil || !ready {
		return result, err
	}
	base := deployment.DeepCopy()
	deployment.Spec.Replicas = ptr.To[int32](0)
	return result, r.Client.Patch(
		ctx,
		&deployment,
		client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}),
	)
}
