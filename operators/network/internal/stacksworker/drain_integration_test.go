//go:build integration

package stacksworker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestStacksActorAPIWaitsForRootWorkerDisposition(t *testing.T) {
	environment := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator", "crds"),
		},
		ErrorIfCRDPathMissing:       true,
		DownloadBinaryAssets:        true,
		DownloadBinaryAssetsVersion: "1.37.0",
		BinaryAssetsDirectory:       filepath.Join(os.TempDir(), "stacks-network-operator-envtest"),
	}
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
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, api.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, kind := range []api.ParticipantKind{"StacksNode", "StacksSigner"} {
		for _, deleting := range []bool{false, true} {
			name := strings.ToLower(string(kind))
			if deleting {
				name += "-delete"
			} else {
				name += "-stop"
			}
			t.Run(name, func(t *testing.T) {
				must := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}))
				root, worker, _, profile := fixture(t)
				root.UID = ""
				root.ResourceVersion = ""
				root.Namespace = name
				root.Status = api.StacksNetworkStatus{}
				root.Finalizers = []string{"network.stacks.org/foundation"}
				root.Spec.Participants[0].Definition.Inline = &worker.Spec.Configuration
				configuration := api.Configuration{StacksNode: &stacks.StacksNodeSpec{}}
				if kind == "StacksSigner" {
					configuration = api.Configuration{StacksSigner: &stacks.StacksSignerSpec{}}
				}
				root.Spec.Participants = append(
					root.Spec.Participants,
					api.Participant{Name: "actor", Kind: kind, Definition: api.Definition{Inline: &configuration}},
				)
				must(c.Create(ctx, root))
				worker.UID = ""
				worker.ResourceVersion = ""
				worker.Namespace = name
				worker.Status = api.ParticipantStatus{}
				worker.Spec.NetworkUID = root.UID
				worker.OwnerReferences = nil
				must(controllerutil.SetControllerReference(root, worker, scheme))
				must(c.Create(ctx, worker))
				pod, err := Pod(worker, profile)
				must(err)
				pod.Spec.NodeName = "node"
				must(c.Create(ctx, pod))
				pod.Status.Phase = corev1.PodRunning
				must(c.Status().Update(ctx, pod))
				actor := &api.StacksNetworkParticipant{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "actor",
						Namespace:  name,
						Finalizers: []string{"network.stacks.org/" + strings.ToLower(string(kind)) + "-workload"},
					},
					Spec: api.StacksNetworkParticipantSpec{
						NetworkUID:      root.UID,
						ParticipantName: "actor",
						Kind:            kind,
						Configuration:   configuration,
					},
				}
				must(controllerutil.SetControllerReference(root, actor, scheme))
				must(c.Create(ctx, actor))
				root.Status.Identities = []api.InstanceIdentity{
					{
						Name:   worker.Spec.ParticipantName,
						UID:    worker.UID,
						Worker: &api.WorkerSession{Pod: podBinding(pod), ProfileDigest: profile.Digest()},
					},
					{Name: "actor", UID: actor.UID},
				}
				must(c.Status().Update(ctx, root))
				labels := participantworkload.Labels(actor, "actor")
				workload := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      participantworkload.Name(actor, "actor"),
						Namespace: name,
						Labels:    labels,
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    ptr.To[int32](1),
						ServiceName: "actor",
						Selector:    &metav1.LabelSelector{MatchLabels: labels},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{Labels: labels},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{Name: "actor", Image: "actor:test"}},
							},
						},
					},
				}
				must(controllerutil.SetControllerReference(actor, workload, scheme))
				must(c.Create(ctx, workload))
				if deleting {
					must(c.Delete(ctx, root))
					must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
					must(c.Delete(ctx, actor))
				} else {
					root.Spec.Operation = "Stopped"
					must(c.Update(ctx, root))
				}
				domain := participantworkload.Reconciler{
					Client: c,
					Reader: c,
					Kind:   kind,
					BeforeStop: func(ctx context.Context, p *api.StacksNetworkParticipant) (bool, error) {
						return CheckActorStop(ctx, c, p)
					},
				}
				request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(actor)}
				_, err = domain.Reconcile(ctx, request)
				must(err)
				must(c.Get(ctx, client.ObjectKeyFromObject(workload), workload))
				if ptr.Deref(workload.Spec.Replicas, 0) != 1 {
					t.Fatal("actor scaled before shutdown was recorded")
				}
				now := time.Now().UTC()
				fact := ProjectSession(root, worker, pod, nil, now)
				if !fact.Changed || root.Status.Identities[0].Worker.Shutdown == nil {
					t.Fatal("shutdown request absent")
				}
				must(c.Status().Update(ctx, root))
				_, err = domain.Reconcile(ctx, request)
				must(err)
				must(c.Get(ctx, client.ObjectKeyFromObject(workload), workload))
				if ptr.Deref(workload.Spec.Replicas, 0) != 1 {
					t.Fatal("actor scaled while worker drain pending")
				}
				// The existing aggregate timeout dispositions uncertainty without pretending the worker exited.
				fact = ProjectSession(root, worker, pod, nil, now.Add(ShutdownBound))
				if !fact.Changed || root.Status.Identities[0].Worker.Disposal == nil ||
					root.Status.Identities[0].Worker.Disposal.Outcome != "Unsettled" ||
					root.Status.Identities[0].Worker.Disposal.Terminated {
					t.Fatal("bounded worker disposition differs")
				}
				must(c.Status().Update(ctx, root))
				_, err = domain.Reconcile(ctx, request)
				must(err)
				must(c.Get(ctx, client.ObjectKeyFromObject(workload), workload))
				if ptr.Deref(workload.Spec.Replicas, 1) != 0 {
					t.Fatal("bounded Unsettled disposition did not permit actor shutdown")
				}
			})
		}
	}
}
