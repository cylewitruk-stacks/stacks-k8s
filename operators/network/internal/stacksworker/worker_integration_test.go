//go:build integration

package stacksworker

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestWorkerAPIBindingPublicationAndOrderedDisposal(t *testing.T) {
	environment := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator", "crds")}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.37.0", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest")}
	config, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(clientgoscheme.AddToScheme(scheme))
	must(api.AddToScheme(scheme))
	c, err := client.New(config, client.Options{Scheme: scheme})
	must(err)
	ctx := context.Background()
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}))
	root, p, _, profile := fixture(t)
	root.Status = api.StacksNetworkStatus{}
	root.UID = ""
	root.ResourceVersion = ""
	root.Spec.Participants[0].Definition.Inline = &p.Spec.Configuration
	must(c.Create(ctx, root))
	p.UID = ""
	p.ResourceVersion = ""
	p.Name = foundation.ParticipantName(string(root.UID), p.Spec.ParticipantName)
	p.Spec.NetworkUID = root.UID
	p.OwnerReferences[0].UID = root.UID
	admission := p.Status.Admission
	p.Status = api.ParticipantStatus{}
	must(c.Create(ctx, p))
	must(participantstatus.Apply(ctx, c, p, api.ParticipantStatus{Admission: admission}, participantstatus.AggregateManager))
	configuration := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: profile.Configuration.Name, Namespace: p.Namespace}, Immutable: ptr.To(true), Data: map[string]string{"input.json": "{}"}}
	key := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: profile.Keys[0].Secret.Name, Namespace: p.Namespace}, Immutable: ptr.To(true), Data: map[string][]byte{"privateKey": []byte("not-read-by-runtime")}}
	must(c.Create(ctx, configuration))
	must(c.Create(ctx, key))
	profile.Configuration.UID = configuration.UID
	profile.Keys[0].Secret.UID = key.UID
	domain := Reconciler{Client: c, Reader: c, Kind: p.Spec.Kind}
	must(domain.ensureSupport(ctx, p, profile))
	pod, err := Pod(p, profile)
	must(err)
	must(c.Create(ctx, pod))
	must(c.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	if _, err := profileFromPod(pod); err != nil {
		t.Fatalf("API defaulting changed fixed profile: %v", err)
	}
	root.Status.Identities = []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: p.UID}}
	root.Status.GenesisRef = objectBinding("ConfigMap", configuration)
	must(c.Status().Update(ctx, root))
	state := &api.ParticipantRuntimeStatus{WorkerCandidate: &api.WorkerCandidate{Pod: podBinding(pod), ProfileDigest: profile.Digest()}}
	must(participantstatus.Apply(ctx, c, p, api.ParticipantStatus{Runtime: state}, "stacks-network-domain-stackstransactionproduction"))
	role := &testRole{}
	worker := Runtime{Client: c, Namespace: p.Namespace, ParticipantName: p.Name, NetworkUID: root.UID, ParticipantUID: p.UID, PodUID: pod.UID, PodName: pod.Name, Profile: profile, Role: role, Prerequisites: func(context.Context, Snapshot) error { return nil }}
	_, err = worker.Reconcile(ctx)
	must(err)
	if role.steps != 0 {
		t.Fatal("worker executed before durable binding")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
	fact := ProjectSession(root, p, pod, nil, time.Now())
	if !fact.Changed || fact.Failed {
		t.Fatalf("candidate not bound: %+v", fact)
	}
	must(c.Status().Update(ctx, root))
	corrupt := root.DeepCopy()
	corrupt.Status.Identities[0].Worker.Pod.UID = "replacement"
	if err := c.Status().Update(ctx, corrupt); err == nil {
		t.Fatal("API permitted replacing bound worker UID")
	}
	_, err = worker.Reconcile(ctx)
	must(err)
	if role.steps != 1 {
		t.Fatal("bound worker did not execute")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
	if p.Status.Runtime == nil || p.Status.Admission == nil || p.Status.Execution == nil {
		t.Fatal("execution SSA removed another status owner")
	}
	// The structural schema must retain exact public traffic identity and original progress.
	evidence := p.Status.Execution.DeepCopy()
	evidence.Traffic = &api.TrafficObservation{Available: true, Found: true, Success: true, TxID: strings.Repeat("a", 64), BlockID: strings.Repeat("b", 64), IndexBlockID: strings.Repeat("c", 64), TargetParticipantUID: "target", TargetPodUID: "actor-pod", TargetContainerID: "containerd://actor", TargetConfigurationDigest: foundation.Digest("configuration"), GenesisUID: "genesis", SubmittedBurnHeight: 301, BurnHeight: 302, ObservedAt: metav1.NewTime(time.Unix(1000, 0)), EffectiveIntervalSeconds: 60, ProgressWindowSeconds: 190}
	must(worker.publish(ctx, p, evidence))
	must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
	if p.Status.Execution.Traffic == nil || !reflect.DeepEqual(p.Status.Execution.Traffic, evidence.Traffic) {
		t.Fatal("API pruned canonical traffic evidence")
	}
	root.Spec.Operation = "Stopped"
	must(c.Update(ctx, root))
	fact = ProjectSession(root, p, pod, nil, time.Now())
	if !fact.Changed || root.Status.Identities[0].Worker.Shutdown == nil {
		t.Fatal("shutdown was not recorded")
	}
	must(c.Status().Update(ctx, root))
	_, err = domain.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)})
	must(err)
	must(c.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	if pod.DeletionTimestamp != nil {
		t.Fatal("domain deleted worker before drain disposition")
	}
	_, err = worker.Reconcile(ctx)
	must(err)
	must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
	fact = ProjectSession(root, p, pod, nil, time.Now())
	if !fact.Changed || root.Status.Identities[0].Worker.Disposal == nil {
		t.Fatal("worker drain not acknowledged")
	}
	must(c.Status().Update(ctx, root))
	result, err := worker.Reconcile(ctx)
	must(err)
	if !result.Exit {
		t.Fatal("worker stayed active after durable disposal")
	}
	_, err = domain.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)})
	must(err)
	must(c.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	if pod.DeletionTimestamp == nil || !Terminated(pod) {
		t.Fatal("unscheduled deleted Pod did not preserve termination evidence")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
	fact = ProjectSession(root, p, pod, nil, time.Now())
	if !fact.Changed || !root.Status.Identities[0].Worker.Disposal.Terminated {
		t.Fatal("termination observation not durable")
	}
	must(c.Status().Update(ctx, root))
	_, err = domain.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)})
	must(err)
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), pod); !apierrors.IsNotFound(err) {
		t.Fatalf("termination finalizer retained Pod: %v", err)
	}
	_, err = domain.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)})
	must(err)
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), pod); !apierrors.IsNotFound(err) {
		t.Fatal("disposed bound worker recreated")
	}
}
