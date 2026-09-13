//go:build integration

package foundationintegration

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// observedRootReader exposes queue quiescence without invoking a reconcile from the test.
type observedRootReader struct {
	client.Reader
	reads atomic.Int64
}

func (r *observedRootReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	object client.Object,
	opts ...client.GetOption,
) error {
	if _, ok := object.(*api.StacksNetwork); ok {
		r.reads.Add(1)
	}
	return r.Reader.Get(ctx, key, object, opts...)
}

// verifyInstalledPolicyRelease exercises only installed informer routing after its initial queue drains.
func verifyInstalledPolicyRelease(
	t *testing.T,
	ctx context.Context,
	c client.Client,
	cfg *rest.Config,
	scheme *runtime.Scheme,
	root *api.StacksNetwork,
	stacker *stacks.StacksStacker,
) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	options := foundation.CacheOptions()
	options.DefaultNamespaces = map[string]cache.Config{root.Namespace: {}}
	manager, err := ctrl.NewManager(
		cfg,
		ctrl.Options{
			Scheme:                 scheme,
			Cache:                  options,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Controller:             controllerconfig.Controller{SkipNameValidation: ptr.To(true)},
			Client: client.Options{
				Cache: &client.CacheOptions{
					DisableFor: []client.Object{
						&corev1.Secret{},
						&corev1.ServiceAccount{},
						&rbacv1.Role{},
						&rbacv1.RoleBinding{},
					},
				},
			},
		},
	)
	must(err)
	reader := &observedRootReader{Reader: manager.GetAPIReader()}
	controller := &foundation.Reconciler{Client: manager.GetClient(), Reader: reader, Scheme: scheme}
	//nolint:contextcheck // Manager setup registers lifetime indexes before the manager starts serving requests.
	must(controller.SetupWithManager(manager))
	running, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- manager.Start(running) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			must(err)
		case <-time.After(10 * time.Second):
			t.Error("policy manager did not stop")
		}
	}()
	syncContext, syncCancel := context.WithTimeout(running, 30*time.Second)
	defer syncCancel()
	if !manager.GetCache().WaitForCacheSync(syncContext) {
		t.Fatal("policy manager cache did not synchronize")
	}
	// A second amount edit is observed while the final affected gate remains incomplete.
	stacker.Spec.AmountMicroSTX = ptr.To(common.Amount("999999999999998"))
	must(c.Update(ctx, stacker))
	key := client.ObjectKey{
		Namespace: root.Namespace,
		Name:      foundation.ParticipantName(string(root.UID), "stacker-01"),
	}
	awaitPolicy(t, ctx, func() bool {
		p := &api.StacksNetworkParticipant{}
		if c.Get(ctx, key, p) != nil {
			return false
		}
		condition := meta.FindStatusCondition(p.Status.Conditions, "Resolved")
		return condition != nil && condition.Reason == "BootstrapPending" &&
			p.Spec.Configuration.StacksStacker != nil &&
			p.Spec.Configuration.StacksStacker.AmountMicroSTX != nil &&
			*p.Spec.Configuration.StacksStacker.AmountMicroSTX == *stacker.Spec.AmountMicroSTX
	})
	// BootstrapPending is a settled topology result, with no scheduled topology retry.
	// Require all initial/source events to drain before the sole gate status mutation.
	stableSince := time.Now()
	previous := reader.reads.Load()
	awaitPolicy(t, ctx, func() bool {
		current := reader.reads.Load()
		if current != previous {
			previous = current
			stableSince = time.Now()
		}
		return time.Since(stableSince) >= time.Second
	})
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
	if root.Status.Phase == "ResolutionError" {
		t.Fatal("unresolved topology would mask the event path with a scheduled retry")
	}
	generation := root.Generation
	root.Status.Initialization.Gates[len(root.Status.Initialization.Gates)-1].CompletedAt = ptr.To(metav1.Now())
	root.Status.Initialization.GateIndex++
	root.Status.Initialization.Completed = true
	must(c.Status().Update(ctx, root))
	awaitPolicy(t, ctx, func() bool {
		p := &api.StacksNetworkParticipant{}
		if c.Get(ctx, key, p) != nil {
			return false
		}
		condition := meta.FindStatusCondition(p.Status.Conditions, "Resolved")
		return condition != nil && condition.Reason == "Admitted" && p.Status.Admission != nil &&
			*p.Status.Admission.Configuration.StacksStacker.AmountMicroSTX == *stacker.Spec.AmountMicroSTX &&
			!meta.IsStatusConditionTrue(p.Status.Conditions, "PolicyDeferred")
	})
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
	if root.Generation != generation || !root.Status.Initialization.Completed {
		t.Fatal("status-only gate release changed desired root generation")
	}
	verifyInstalledBoundPlacement(t, ctx, c, root, stacker)
}

// verifyInstalledBoundPlacement requires a real admitted worker pin to survive rejected placement and live controls.
func verifyInstalledBoundPlacement(
	t *testing.T,
	ctx context.Context,
	c client.Client,
	root *api.StacksNetwork,
	stacker *stacks.StacksStacker,
) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	p := participant(t, ctx, c, root, "stacker-01")
	prior := p.Status.Admission.DeepCopy()
	participantUID := p.UID
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: foundation.RuntimeName(
				string(p.Spec.NetworkUID),
				string(p.UID),
				string(p.Spec.Kind),
				p.Spec.ParticipantName,
				"worker",
			),
			Namespace: root.Namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers:    []corev1.Container{{Name: "worker", Image: "worker:test"}},
		},
	}
	must(controllerutil.SetControllerReference(p, pod, c.Scheme()))
	must(c.Create(ctx, pod))
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
	session := &api.WorkerSession{
		Pod:           api.WorkerPodBinding{Kind: "Pod", Name: pod.Name, UID: pod.UID},
		ProfileDigest: foundation.Digest("installed-placement-profile"),
	}
	found := false
	for i := range root.Status.Identities {
		if root.Status.Identities[i].UID == p.UID {
			root.Status.Identities[i].Worker = session.DeepCopy()
			found = true
		}
	}
	if !found {
		t.Fatal("stacker ledger identity missing")
	}
	must(c.Status().Update(ctx, root))
	originalPlacement := stacker.Spec.WorkerPlacement.DeepCopy()
	stacker.Spec.WorkerPlacement = &common.Placement{NodeSelector: map[string]string{"pool": "another"}}
	must(c.Update(ctx, stacker))
	// Control remains independently projectable even while the source candidate is rejected.
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
	for i := range root.Spec.Participants {
		if root.Spec.Participants[i].Name == p.Spec.ParticipantName {
			root.Spec.Participants[i].Control = &api.Control{Paused: ptr.To(true)}
		}
	}
	must(c.Update(ctx, root))
	awaitPolicy(t, ctx, func() bool {
		if c.Get(ctx, client.ObjectKeyFromObject(p), p) != nil {
			return false
		}
		condition := meta.FindStatusCondition(p.Status.Conditions, "Resolved")
		return condition != nil && condition.Reason == "RequiresReplacement" && p.Spec.Control != nil &&
			ptr.Deref(p.Spec.Control.Paused, false)
	})
	if p.UID != participantUID || !reflect.DeepEqual(p.Status.Admission, prior) {
		t.Fatal("placement rejection changed the whole prior admission")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
	found = false
	for _, identity := range root.Status.Identities {
		if identity.Name == p.Spec.ParticipantName {
			found = true
			if identity.UID != participantUID || !reflect.DeepEqual(identity.Worker, session) {
				t.Fatal("placement edit rebound the retained worker")
			}
		}
	}
	if !found {
		t.Fatal("placement edit removed the retained identity")
	}

	// Restore the candidate for the surrounding schema tests; the immutable worker pin remains.
	must(c.Get(ctx, client.ObjectKeyFromObject(stacker), stacker))
	stacker.Spec.WorkerPlacement = originalPlacement
	must(c.Update(ctx, stacker))
	awaitPolicy(t, ctx, func() bool {
		if c.Get(ctx, client.ObjectKeyFromObject(p), p) != nil {
			return false
		}
		condition := meta.FindStatusCondition(p.Status.Conditions, "Resolved")
		return condition != nil && condition.Reason == "Admitted"
	})
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
	for i := range root.Spec.Participants {
		if root.Spec.Participants[i].Name == p.Spec.ParticipantName {
			root.Spec.Participants[i].Control = nil
		}
	}
	must(c.Update(ctx, root))
	awaitPolicy(
		t,
		ctx,
		func() bool { return c.Get(ctx, client.ObjectKeyFromObject(p), p) == nil && p.Spec.Control == nil },
	)
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
}

// awaitPolicy bounds only test observation; the production controller owns every reconcile.
func awaitPolicy(t *testing.T, ctx context.Context, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("installed policy controller did not converge")
}
