//go:build integration

package foundationintegration

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// driveRoot advances real API writes without a manager or protocol runtime.
func driveRoot(t *testing.T, ctx context.Context, c client.Client, r *foundation.Reconciler, request ctrl.Request, root *api.StacksNetwork) {
	t.Helper()
	for i := 0; i < 100; i++ {
		result, err := r.Reconcile(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Requeue {
			break
		}
	}
	if err := c.Get(ctx, request.NamespacedName, root); err != nil {
		t.Fatal(err)
	}
}
func participant(t *testing.T, ctx context.Context, c client.Client, root *api.StacksNetwork, name string) *api.StacksNetworkParticipant {
	t.Helper()
	p := &api.StacksNetworkParticipant{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: foundation.ParticipantName(string(root.UID), name)}, p); err != nil {
		t.Fatal(err)
	}
	return p
}
func requireReason(t *testing.T, p *api.StacksNetworkParticipant, reason string) {
	t.Helper()
	condition := meta.FindStatusCondition(p.Status.Conditions, "Resolved")
	if condition == nil || condition.Reason != reason {
		t.Fatalf("%s: wanted %s, got %+v", p.Spec.ParticipantName, reason, p.Status)
	}
}
func updateObject(t *testing.T, ctx context.Context, c client.Client, obj client.Object) {
	t.Helper()
	if err := c.Update(ctx, obj); err != nil {
		t.Fatal(err)
	}
}

func verifyFrozenPolicyUpdates(t *testing.T, ctx context.Context, c client.Client, r *foundation.Reconciler, request ctrl.Request, root *api.StacksNetwork, genesis *api.StacksGenesis) {
	t.Helper()
	originalSpec := *root.Spec.DeepCopy()
	old := participant(t, ctx, c, root, "traffic")
	var traffic stacks.StacksTransactionProduction
	key := client.ObjectKey{Namespace: root.Namespace, Name: "traffic"}
	if err := c.Get(ctx, key, &traffic); err != nil {
		t.Fatal(err)
	}
	originalTraffic := *traffic.Spec.DeepCopy()
	traffic.Spec.Interval = ptr.To(common.Duration("20s"))
	traffic.Spec.Recipient = &stacks.Recipient{AccountRef: &common.NameRef{Name: "admin-01"}}
	updateObject(t, ctx, c, &traffic)
	for i := range root.Spec.Participants {
		if root.Spec.Participants[i].Name == "traffic" {
			root.Spec.Participants[i].Control = &api.Control{Paused: ptr.To(true)}
		}
	}
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	got := participant(t, ctx, c, root, "traffic")
	requireReason(t, got, "BootstrapPending")
	if got.UID != old.UID || !reflect.DeepEqual(got.Status.Admission, old.Status.Admission) || !meta.IsStatusConditionTrue(got.Status.Conditions, "PolicyDeferred") {
		t.Fatal("whole admitted policy/dependencies were not retained")
	}
	if *got.Spec.Configuration.StacksTransactionProduction.Interval != "20s" || got.Spec.Control == nil || !ptr.Deref(got.Spec.Control.Paused, false) {
		t.Fatal("candidate or independent control was lost")
	}
	// Add two late participants before introducing errors, so removal and admission can be checked independently.
	extra := func(name string) api.Participant {
		return api.Participant{Name: name, Kind: "BitcoinNode", Definition: api.Definition{Ref: &common.NameRef{Name: "btc-08"}}}
	}
	root.Spec.Participants = append(root.Spec.Participants, extra("remove-later"))
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	removed := participant(t, ctx, c, root, "remove-later")
	requireReason(t, removed, "Admitted")
	// Wallet attachment on an already admitted late actor is mutable, without a new UID.
	for i := range root.Spec.Participants {
		if root.Spec.Participants[i].Name == "remove-later" {
			root.Spec.Participants[i].Overrides = &api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{WalletRefs: ptr.To([]common.NameRef{{Name: "production-wallet"}})}}
		}
	}
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	attached := participant(t, ctx, c, root, "remove-later")
	requireReason(t, attached, "Admitted")
	if attached.UID != removed.UID || len(ptr.Deref(attached.Status.Admission.Configuration.BitcoinNode.WalletRefs, nil)) != 1 {
		t.Fatal("mutable wallet attachment was rejected or replaced the actor")
	}

	if err := c.Get(ctx, key, &traffic); err != nil {
		t.Fatal(err)
	}
	for i := range root.Spec.Participants {
		if root.Spec.Participants[i].Name == "traffic" {
			root.Spec.Participants[i].Control = &api.Control{Paused: ptr.To(false)}
		}
	}
	traffic.Spec.TargetNodeRef = &common.NameRef{Name: "signer-node-02"}
	updateObject(t, ctx, c, &traffic)
	root.Spec.Participants = root.Spec.Participants[:len(root.Spec.Participants)-1]
	root.Spec.Participants = append(root.Spec.Participants, extra("healthy-later"))
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	got = participant(t, ctx, c, root, "traffic")
	requireReason(t, got, "RequiresReplacement")
	if got.Spec.Control == nil || got.Spec.Control.Paused == nil || *got.Spec.Control.Paused {
		t.Fatal("protected target change blocked independent control projection")
	}
	if !reflect.DeepEqual(got.Status.Admission, old.Status.Admission) || !meta.IsStatusConditionFalse(got.Status.Conditions, "PolicyDeferred") || root.Status.Phase == "Failed" {
		t.Fatal("protected change was deferred, admitted or made terminal")
	}
	requireReason(t, participant(t, ctx, c, root, "healthy-later"), "Admitted")
	if err := c.Get(ctx, client.ObjectKeyFromObject(removed), removed); !apierrors.IsNotFound(err) {
		t.Fatalf("unrelated removal blocked: %v", err)
	}
	// ResolutionError and reused-name failures must also leave unrelated admission possible.
	for _, test := range []struct {
		name   string
		entry  api.Participant
		reason string
	}{
		{"resolution", api.Participant{Name: "bad-definition", Kind: "BitcoinNode", Definition: api.Definition{Ref: &common.NameRef{Name: "absent"}}}, "InputsUnavailable"},
		{"reuse", extra("remove-later"), "NameAlreadyUsed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := c.Get(ctx, key, &traffic); err != nil {
				t.Fatal(err)
			}
			traffic.Spec = originalTraffic
			updateObject(t, ctx, c, &traffic)
			root.Spec.Participants = append(append([]api.Participant{}, originalSpec.Participants...), test.entry, extra("healthy-"+test.name))
			updateObject(t, ctx, c, root)
			driveRoot(t, ctx, c, r, request, root)
			condition := meta.FindStatusCondition(root.Status.Conditions, "Resolved")
			if condition == nil || condition.Reason != test.reason {
				t.Fatalf("wrong root error: %+v", condition)
			}
			requireReason(t, participant(t, ctx, c, root, "healthy-"+test.name), "Admitted")
		})
	}
	root.Spec.Participants = originalSpec.Participants
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	// Both Bitcoin protected paths are checked before whole-policy deferral.
	var production bitcoin.BitcoinBlockProduction
	productionKey := client.ObjectKey{Namespace: root.Namespace, Name: "blocks"}
	if err := c.Get(ctx, productionKey, &production); err != nil {
		t.Fatal(err)
	}
	originalProduction := *production.Spec.DeepCopy()
	priorProduction := participant(t, ctx, c, root, "blocks").Status.Admission
	for _, field := range []string{"payout", "initialization"} {
		t.Run(field, func(t *testing.T) {
			if err := c.Get(ctx, productionKey, &production); err != nil {
				t.Fatal(err)
			}
			production.Spec = *originalProduction.DeepCopy()
			if field == "payout" {
				production.Spec.PayoutWalletRef = &common.NameRef{Name: "miner-wallet-01"}
			} else {
				production.Spec.Initialization.MatureOutputsPerMiner++
			}
			updateObject(t, ctx, c, &production)
			for i := range root.Spec.Participants {
				if root.Spec.Participants[i].Name == "blocks" {
					root.Spec.Participants[i].Control = &api.Control{Paused: ptr.To(field == "payout")}
				}
			}
			updateObject(t, ctx, c, root)
			driveRoot(t, ctx, c, r, request, root)
			p := participant(t, ctx, c, root, "blocks")
			requireReason(t, p, "RequiresReplacement")
			if p.Spec.Control == nil || p.Spec.Control.Paused == nil || *p.Spec.Control.Paused != (field == "payout") {
				t.Fatal("frozen Bitcoin compatibility rejection blocked independent control")
			}
			if !reflect.DeepEqual(p.Status.Admission, priorProduction) || meta.IsStatusConditionTrue(p.Status.Conditions, "PolicyDeferred") {
				t.Fatal("protected Bitcoin policy changed or deferred")
			}
		})
	}
	if err := c.Get(ctx, productionKey, &production); err != nil {
		t.Fatal(err)
	}
	production.Spec = originalProduction
	updateObject(t, ctx, c, &production)
	var node bitcoin.BitcoinNode
	nodeKey := client.ObjectKey{Namespace: root.Namespace, Name: "btc-08"}
	if err := c.Get(ctx, nodeKey, &node); err != nil {
		t.Fatal(err)
	}
	originalNode := *node.Spec.DeepCopy()
	node.Spec.WalletRefs = ptr.To([]common.NameRef{{Name: "production-wallet"}})
	updateObject(t, ctx, c, &node)
	driveRoot(t, ctx, c, r, request, root)
	requireReason(t, participant(t, ctx, c, root, "btc-08"), "BootstrapPending")
	if err := c.Get(ctx, nodeKey, &node); err != nil {
		t.Fatal(err)
	}
	node.Spec = originalNode
	updateObject(t, ctx, c, &node)
	driveRoot(t, ctx, c, r, request, root)
	if root.Status.Phase != "Initializing" || root.Status.GenesisRef.UID != genesis.UID {
		t.Fatalf("correction did not recover same network: %+v", root.Status)
	}
	var captures api.StacksGenesisList
	if err := c.List(ctx, &captures, client.InNamespace(root.Namespace)); err != nil {
		t.Fatal(err)
	}
	if len(captures.Items) != 1 || !reflect.DeepEqual(captures.Items[0].Spec, genesis.Spec) {
		t.Fatal("policy edits changed or repeated genesis capture")
	}
}

func verifyFundingOptions(t *testing.T, ctx context.Context, c client.Client, ns string, spec api.StacksNetworkSpec, step func(), root *api.StacksNetwork) {
	t.Helper()
	spec.Operation = "Running"
	for i := range spec.Participants {
		p := &spec.Participants[i]
		switch p.Kind {
		case "StacksTransactionProduction":
			p.Overrides = &api.Configuration{StacksTransactionProduction: &stacks.StacksTransactionProductionSpec{AccountRef: &common.NameRef{Name: "sbtc-deployer"}}}
		case "StacksFaucet":
			p.Overrides = &api.Configuration{StacksFaucet: &stacks.StacksFaucetSpec{GenesisBalanceMicroSTX: ptr.To(common.Amount("0"))}}
		}
	}
	fresh := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: ns}, Spec: spec}
	if err := c.Create(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		step()
		if root.Status.GenesisRef != nil {
			break
		}
	}
	if root.Status.GenesisRef == nil {
		t.Fatalf("shared account/zero faucet prevented freeze: %+v", root.Status)
	}
	var artifact api.StacksGenesis
	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: root.Status.GenesisRef.Name}, &artifact); err != nil {
		t.Fatal(err)
	}
	faucet := participant(t, ctx, c, root, "faucet")
	var account stacks.StacksAccount
	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: faucet.Status.Admission.Configuration.StacksFaucet.AccountRef.Name}, &account); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Spec.Chain.Allocations) != 18 {
		t.Fatalf("zero balance still allocated: %d", len(artifact.Spec.Chain.Allocations))
	}
	for _, a := range artifact.Spec.Chain.Allocations {
		if a.Address == account.Status.Identity.Address {
			t.Fatal("zero faucet account present in genesis")
		}
	}
	traffic := participant(t, ctx, c, root, "traffic")
	contracts := participant(t, ctx, c, root, "sbtc")
	if traffic.Status.Admission.Configuration.StacksTransactionProduction.AccountRef.Name != contracts.Status.Admission.Configuration.StacksContractSet.DeployerAccountRef.Name {
		t.Fatal("test did not admit shared sender")
	}
}

func verifyStoppedRemovalAndInstanceLoss(t *testing.T, ctx context.Context, c client.Client, scheme *runtime.Scheme) {
	for _, mode := range []string{"stopped", "lost"} {
		t.Run(mode, func(t *testing.T) {
			ns := "foundation-" + mode
			if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
				t.Fatal(err)
			}
			r := &foundation.Reconciler{Client: c, Reader: c, Scheme: scheme}
			root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: ns}, Spec: api.StacksNetworkSpec{Operation: "Paused", Participants: []api.Participant{{Name: "core", Kind: "BitcoinNode", Definition: api.Definition{Inline: &api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{}}}}}}}
			if err := c.Create(ctx, root); err != nil {
				t.Fatal(err)
			}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(root)}
			driveRoot(t, ctx, c, r, request, root)
			p := participant(t, ctx, c, root, "core")
			requireReason(t, p, "Admitted")
			uid := p.UID
			if mode == "lost" {
				if err := c.Delete(ctx, p); err != nil {
					t.Fatal(err)
				}
				driveRoot(t, ctx, c, r, request, root)
				condition := meta.FindStatusCondition(root.Status.Conditions, "Resolved")
				if root.Status.Phase != "Failed" || condition == nil || condition.Reason != "InstanceLost" {
					t.Fatalf("loss was not terminal: %+v", root.Status)
				}
				for i := 0; i < 3; i++ {
					driveRoot(t, ctx, c, r, request, root)
				}
				if err := c.Get(ctx, client.ObjectKeyFromObject(p), p); !apierrors.IsNotFound(err) {
					t.Fatalf("lost instance recreated: %v", err)
				}
				if root.Status.Identities[0].UID != uid {
					t.Fatal("recorded identity changed")
				}
			} else {
				root.Spec.Operation = "Stopped"
				root.Spec.Participants[0].Name = "never-start"
				updateObject(t, ctx, c, root)
				driveRoot(t, ctx, c, r, request, root)
				if root.Status.Phase != "Stopped" {
					t.Fatalf("phase: %s", root.Status.Phase)
				}
				var instances api.StacksNetworkParticipantList
				if err := c.List(ctx, &instances, client.InNamespace(ns)); err != nil {
					t.Fatal(err)
				}
				if len(instances.Items) != 0 || len(root.Status.Identities) != 1 || root.Status.Identities[0].UID != uid || !root.Status.Identities[0].Removing {
					t.Fatal("stopped removal retained instance or admitted a new one")
				}
			}
		})
	}
}

func verifyResolverFailureAndCache(t *testing.T, ctx context.Context, c client.Client, manager ctrl.Manager, ns string) {
	t.Helper()
	unrelated := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: ns}}
	if err := c.Create(ctx, unrelated); err != nil {
		t.Fatal(err)
	}
	unrelatedJob := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: ns}, Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, Containers: []corev1.Container{{Name: "noop", Image: "noop"}}}}}}
	if err := c.Create(ctx, unrelatedJob); err != nil {
		t.Fatal(err)
	}
	failed := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "failed", Namespace: ns}}
	if err := c.Create(ctx, failed); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	var target *batchv1.Job
	for time.Now().Before(deadline) {
		var jobs batchv1.JobList
		if err := c.List(ctx, &jobs, client.InNamespace(ns)); err != nil {
			t.Fatal(err)
		}
		for i := range jobs.Items {
			if jobs.Items[i].Labels["network.stacks.org/source-uid"] == string(failed.UID) {
				target = &jobs.Items[i]
			}
		}
		if target != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if target == nil {
		t.Fatal("failure test resolver was not created")
	}
	uid := target.UID
	target.Status.StartTime = ptr.To(metav1.Now())
	target.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"}, {Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", Message: "raw detail must not enter account status"}}
	if err := c.Status().Update(ctx, target); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		if err := c.Get(ctx, client.ObjectKeyFromObject(failed), failed); err != nil {
			t.Fatal(err)
		}
		condition := meta.FindStatusCondition(failed.Status.Conditions, "Resolved")
		if condition != nil && condition.Reason == "ResolverFailed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	condition := meta.FindStatusCondition(failed.Status.Conditions, "Resolved")
	if condition == nil || condition.Reason != "ResolverFailed" || condition.Status != metav1.ConditionFalse || strings.Contains(condition.Message, "raw detail") {
		t.Fatalf("failed Job not safely surfaced: %+v", condition)
	}
	r := &foundation.IdentityReconciler{Client: c, Reader: c, Scheme: manager.GetScheme(), Image: "foundation:test"}
	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(failed)})
	if err != nil || result.Requeue || result.RequeueAfter != 0 {
		t.Fatalf("failed job was polled/retried: %v %v", result, err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(target), target); err != nil || target.UID != uid {
		t.Fatal("failed Job was replaced")
	}
	var reports corev1.ConfigMapList
	if err := manager.GetCache().List(ctx, &reports, client.InNamespace(ns)); err != nil {
		t.Fatal(err)
	}
	if len(reports.Items) == 0 {
		t.Fatal("filtered cache missed resolver reports")
	}
	foundReport := false
	for _, report := range reports.Items {
		if owner := metav1.GetControllerOf(&report); owner != nil && owner.UID == failed.UID {
			foundReport = true
		}
		if report.Name == "unrelated" {
			t.Fatal("unrelated ConfigMap cached")
		}
	}
	if !foundReport {
		t.Fatal("filtered cache missed the report created after the unrelated ConfigMap")
	}
	var jobs batchv1.JobList
	if err := manager.GetCache().List(ctx, &jobs, client.InNamespace(ns)); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, job := range jobs.Items {
		if job.Name == "unrelated" {
			t.Fatal("unrelated Job cached")
		}
		found = found || job.UID == uid
	}
	if !found {
		t.Fatal("filtered cache missed failed resolver")
	}
}

// stalePublicClient emulates a cache that has not observed a definition update yet.
type stalePublicClient struct {
	client.Client
	node  *stacks.StacksNode
	reads int
}

func (c *stalePublicClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if node, ok := obj.(*stacks.StacksNode); ok && key == client.ObjectKeyFromObject(c.node) {
		*node = *c.node.DeepCopy()
		c.reads++
		return nil
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func verifyFreshFreezeReads(t *testing.T, ctx context.Context, c client.Client, r *foundation.Reconciler, request ctrl.Request, root *api.StacksNetwork) {
	t.Helper()
	var source stacks.StacksNode
	key := client.ObjectKey{Namespace: root.Namespace, Name: "signer-node-01"}
	if err := c.Get(ctx, key, &source); err != nil {
		t.Fatal(err)
	}
	original := *source.Spec.DeepCopy()
	cached := &stalePublicClient{Client: r.Client, node: source.DeepCopy()}
	source.Spec.Image = ptr.To("node:changed-before-freeze")
	updateObject(t, ctx, c, &source)
	previous, previousReader := r.Client, r.Reader
	fresh := &countDefinitionReader{Reader: r.Reader, key: key}
	r.Client, r.Reader = cached, fresh
	defer func() { r.Client, r.Reader = previous, previousReader }()
	var result ctrl.Result
	var err error
	for i := 0; i < 100 && fresh.reads == 0; i++ {
		result, err = r.Reconcile(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !result.Requeue || cached.reads == 0 || fresh.reads == 0 {
		t.Fatalf("fresh freeze check did not reject stale cache: %+v cached=%d fresh=%d", result, cached.reads, fresh.reads)
	}
	var artifacts api.StacksGenesisList
	if err := c.List(ctx, &artifacts, client.InNamespace(root.Namespace)); err != nil {
		t.Fatal(err)
	}
	if len(artifacts.Items) != 0 {
		t.Fatal("stale cached definition reached genesis")
	}
	if err := c.Get(ctx, key, &source); err != nil {
		t.Fatal(err)
	}
	source.Spec = original
	updateObject(t, ctx, c, &source)
}

func verifyProductionReplacement(t *testing.T, ctx context.Context, c client.Client, r *foundation.Reconciler, request ctrl.Request, root *api.StacksNetwork) {
	t.Helper()
	genesisUID := root.Status.GenesisRef.UID
	for i := range root.Spec.Participants {
		if root.Spec.Participants[i].Kind == "BitcoinBlockProduction" {
			root.Spec.Participants[i].Name = "replacement-blocks"
			root.Spec.Participants[i].Overrides = &api.Configuration{BitcoinBlockProduction: &bitcoin.BitcoinBlockProductionSpec{PayoutWalletRef: &common.NameRef{Name: "miner-wallet-01"}}}
		}
	}
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	p := participant(t, ctx, c, root, "replacement-blocks")
	requireReason(t, p, "RequiresReplacement")
	if p.Status.Admission != nil || root.Status.GenesisRef.UID != genesisUID {
		t.Fatal("a new name bypassed frozen payout identity")
	}
	for i := range root.Spec.Participants {
		if root.Spec.Participants[i].Name == "replacement-blocks" {
			root.Spec.Participants[i].Overrides = nil
		}
	}
	updateObject(t, ctx, c, root)
	driveRoot(t, ctx, c, r, request, root)
	accepted := participant(t, ctx, c, root, "replacement-blocks")
	requireReason(t, accepted, "Admitted")
	if accepted.UID != p.UID || root.Status.GenesisRef.UID != genesisUID {
		t.Fatal("corrected replacement changed instance or genesis")
	}
}

// countDefinitionReader proves the uncached freeze check actually read the changed source.
type countDefinitionReader struct {
	client.Reader
	key   client.ObjectKey
	reads int
}

func (r *countDefinitionReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*stacks.StacksNode); ok && key == r.key {
		r.reads++
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}
