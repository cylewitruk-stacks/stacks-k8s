//go:build integration

package participantworkload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestActorAPILifecycleAndStatusIsolation(t *testing.T) {
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
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := bitcoin.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}))
	verifyCandidateNativeAPI(t, ctx, c)
	verifyBitcoinCandidateAPI(t, ctx, c)
	verifyShutdownReconcile(t, ctx, c)
	for _, kind := range []api.ParticipantKind{"BitcoinNode", "StacksNode", "StacksSigner"} {
		t.Run(string(kind), func(t *testing.T) {
			p := participantFixture()
			if kind != "BitcoinNode" {
				p = stacksParticipantFixture(kind)
			}
			admission := p.Status.Admission
			p.UID = ""
			p.ResourceVersion = ""
			p.Status = api.ParticipantStatus{}
			must(c.Create(ctx, p))
			must(
				participantstatus.Apply(
					ctx,
					c,
					p,
					api.ParticipantStatus{
						Admission: admission,
						Conditions: []metav1.Condition{
							{
								Type:               "Resolved",
								Status:             metav1.ConditionTrue,
								Reason:             "Admitted",
								Message:            "Admitted",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
					participantstatus.AggregateManager,
				),
			)
			r := Reconciler{Client: c, Reader: c, Kind: kind}
			render := func() (*appsv1.StatefulSet, error) {
				if kind == "BitcoinNode" {
					return StatefulSet(p, "config", "actor-credentials", 1)
				}
				return actorWorkload(p, nativeRuntimeFixture())
			}
			workload, err := render()
			must(err)
			must(r.reconcileStatefulSet(ctx, p, workload))
			firstGeneration := workload.Generation
			for range 2 {
				desired, err := render()
				must(err)
				must(r.reconcileStatefulSet(ctx, p, desired))
				if desired.Generation != firstGeneration {
					t.Fatal("unchanged reconciliation rolled actor generation")
				}
			}
			state := api.ParticipantRuntimeStatus{
				ObservedGeneration: p.Generation,
				PolicyDigest:       admission.PolicyDigest,
				WorkloadRefs:       []common.Binding{*binding("StatefulSet", workload)},
			}
			if kind != "BitcoinNode" {
				state.ConfigurationDigest = "sha256:configuration"
				state.EventAuthSecretRef = nativeRuntimeFixture().EventAuthSecretRef
			}
			if kind == "StacksNode" {
				state.Protocol = &api.StacksProtocolObservation{
					Available:            true,
					Reason:               "Observed",
					PodUID:               "pod-uid",
					ContainerID:          "containerd://native",
					ConfigurationDigest:  state.ConfigurationDigest,
					GenesisUID:           "genesis-uid",
					ObservedAt:           metav1.Now(),
					LastHeightAdvancedAt: metav1.Now(),
					HighestStacksHeight:  30,
					StacksHeight:         30,
					BurnHeight:           240,
					PoXBurnHeight:        240,
					NetworkID:            0x80000000,
					PreparedSet: &api.PreparedSignerSetObservation{
						Cycle:     12,
						Available: true,
						Threshold: "9007199254740993",
						Signers: []api.PreparedSignerObservation{
							{
								PublicKey:     "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798",
								Weight:        7,
								StackedAmount: "340282366920938463463374607431768211455",
							},
						},
						ObservedAt: metav1.Now(),
					},
				}
			}
			must(
				participantstatus.Apply(
					ctx,
					c,
					p,
					api.ParticipantStatus{
						Runtime: &state,
						Conditions: []metav1.Condition{
							{
								Type:               "WorkloadReady",
								Status:             metav1.ConditionFalse,
								Reason:             "Pending",
								Message:            "Pod pending",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
					r.fieldManager(),
				),
			)
			if kind == "StacksNode" &&
				(p.Status.Runtime.Protocol == nil ||
					p.Status.Runtime.Protocol.PreparedSet.Signers[0].StackedAmount !=
						"340282366920938463463374607431768211455") {
				t.Fatal("API lost exact native protocol facts")
			}
			if p.Status.Admission == nil || p.Status.Admission.PolicyDigest != admission.PolicyDigest {
				t.Fatal("domain status overwrote aggregate admission")
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workload.Name + "-0",
					Namespace:  p.Namespace,
					Labels:     workload.Spec.Template.Labels,
					Finalizers: workload.Spec.Template.Finalizers,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "StatefulSet",
							Name:       workload.Name,
							UID:        workload.UID,
							Controller: ptr.To(true),
						},
					},
				},
				Spec: *workload.Spec.Template.Spec.DeepCopy(),
			}
			pod.Spec.Volumes = append(
				pod.Spec.Volumes,
				corev1.Volume{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "data-" + workload.Name + "-0",
						},
					},
				},
			)
			must(c.Create(ctx, pod))
			state.PodRef = binding("Pod", pod)
			reason, err := r.shutdown(ctx, p, &state, false)
			must(err)
			if reason != "Stopping" || state.Terminated {
				t.Fatal("scale request asserted process termination")
			}
			must(c.Get(ctx, client.ObjectKeyFromObject(workload), workload))
			if *workload.Spec.Replicas != 0 ||
				workload.Spec.PersistentVolumeClaimRetentionPolicy.WhenScaled !=
					appsv1.RetainPersistentVolumeClaimRetentionPolicyType {
				t.Fatal("scale-down lost retained storage")
			}
			// Envtest has no kubelet; model the never-scheduled Pod deletion explicitly.
			must(c.Delete(ctx, pod))
			must(c.Get(ctx, client.ObjectKeyFromObject(pod), pod))
			if !controllerutil.ContainsFinalizer(pod, PodFinalizer) {
				t.Fatal("termination evidence disappeared before controller observation")
			}
			_, err = r.shutdown(ctx, p, &state, false)
			must(err)
			if !state.Terminated {
				t.Fatal("deleting never-scheduled Pod should have no process to terminate")
			}
			// A failed status write must leave the source evidence available for retry.
			must(c.Get(ctx, client.ObjectKeyFromObject(pod), pod))
			if !controllerutil.ContainsFinalizer(pod, PodFinalizer) {
				t.Fatal("released Pod before durable termination status")
			}
			_, err = r.shutdown(ctx, p, &state, false)
			must(err)
			must(c.Get(ctx, client.ObjectKeyFromObject(pod), pod))
			if !controllerutil.ContainsFinalizer(pod, PodFinalizer) {
				t.Fatal("retry released unpersisted evidence")
			}
			must(participantstatus.Apply(ctx, c, p, api.ParticipantStatus{Runtime: &state}, r.fieldManager()))
			_, err = r.shutdown(ctx, p, &state, false)
			must(err)
			if err := c.Get(ctx, client.ObjectKeyFromObject(pod), pod); !apierrors.IsNotFound(err) {
				t.Fatalf("durably accounted Pod not released: %v", err)
			}
			foreign, err := render()
			must(err)
			foreign.OwnerReferences[0].UID = "other-participant"
			base := workload.DeepCopy()
			workload.OwnerReferences = foreign.OwnerReferences
			must(c.Patch(ctx, workload, client.MergeFrom(base)))
			if err := r.reconcileStatefulSet(ctx, p, foreign); err == nil {
				t.Fatal("adopted foreign workload under the deterministic name")
			}
		})
	}
}

// verifyShutdownReconcile tests durable termination and finalizer release through the public reconciler.
func verifyShutdownReconcile(t *testing.T, ctx context.Context, c client.Client) {
	for _, kind := range []api.ParticipantKind{"BitcoinNode", "StacksNode", "StacksSigner"} {
		for _, mode := range []string{"remove", "stop", "suspend", "missing-workload"} {
			t.Run(string(kind)+"-"+mode, func(t *testing.T) {
				must := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				p := participantFixture()
				if kind != "BitcoinNode" {
					p = stacksParticipantFixture(kind)
				}
				p.Namespace = strings.ToLower(string(kind)) + "-" + mode
				must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: p.Namespace}}))
				admission := p.Status.Admission
				root := &api.StacksNetwork{
					ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: p.Namespace},
					Spec: api.StacksNetworkSpec{
						Operation: "Stopped",
						Participants: []api.Participant{
							{
								Name:       p.Spec.ParticipantName,
								Kind:       kind,
								Definition: api.Definition{Inline: &p.Spec.Configuration},
							},
						},
					},
				}
				if mode == "suspend" {
					root.Spec.Operation = "Running"
					root.Spec.Participants[0].Control = &api.Control{Suspended: ptr.To(true)}
				}
				must(c.Create(ctx, root))
				p.UID, p.ResourceVersion = "", ""
				p.Spec.NetworkUID = root.UID
				p.OwnerReferences[0].UID = root.UID
				p.Status = api.ParticipantStatus{}
				r := Reconciler{Client: c, Reader: c, Kind: kind}
				p.Finalizers = []string{r.finalizer()}
				must(c.Create(ctx, p))
				root.Status.Identities = []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: p.UID}}
				must(c.Status().Update(ctx, root))
				must(
					participantstatus.Apply(
						ctx,
						c,
						p,
						api.ParticipantStatus{Admission: admission},
						participantstatus.AggregateManager,
					),
				)
				var workload *appsv1.StatefulSet
				var err error
				if kind == "BitcoinNode" {
					workload, err = StatefulSet(p, "config", "credentials", 0)
				} else {
					workload, err = actorWorkload(p, nativeRuntimeFixture())
					workload.Spec.Replicas = ptr.To[int32](0)
				}
				must(err)
				must(c.Create(ctx, workload))
				workload.Status.ObservedGeneration = workload.Generation
				must(c.Status().Update(ctx, workload))
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:       workload.Name + "-0",
						Namespace:  p.Namespace,
						Labels:     workload.Spec.Template.Labels,
						Finalizers: []string{PodFinalizer},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "apps/v1",
								Kind:       "StatefulSet",
								Name:       workload.Name,
								UID:        workload.UID,
								Controller: ptr.To(true),
							},
						},
					},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "actor", Image: "actor:test"}}},
				}
				must(c.Create(ctx, pod))
				state := &api.ParticipantRuntimeStatus{
					ObservedGeneration: p.Generation,
					PodRef:             binding("Pod", pod),
					WorkloadRefs:       []common.Binding{*binding("StatefulSet", workload)},
				}
				must(participantstatus.Apply(ctx, c, p, api.ParticipantStatus{Runtime: state}, r.fieldManager()))
				// Envtest has no kubelet or GC: explicitly delete the never-scheduled Pod.
				must(c.Delete(ctx, pod))
				removing := mode == "remove" || mode == "missing-workload"
				if removing {
					must(c.Delete(ctx, p))
				}
				if mode == "missing-workload" {
					must(c.Delete(ctx, workload))
				}
				key := client.ObjectKeyFromObject(p)
				result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
				must(err)
				must(c.Get(ctx, key, p))
				must(c.Get(ctx, client.ObjectKeyFromObject(pod), pod))
				condition := meta.FindStatusCondition(p.Status.Conditions, "WorkloadReady")
				if result.RequeueAfter != 5*time.Second || p.Status.Runtime == nil || !p.Status.Runtime.Terminated ||
					condition == nil ||
					condition.Reason != "Stopping" ||
					!controllerutil.ContainsFinalizer(p, r.finalizer()) ||
					!controllerutil.ContainsFinalizer(pod, PodFinalizer) {
					t.Fatalf("first termination pass settled early: result=%+v status=%+v", result, p.Status)
				}
				for range 3 {
					_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
					must(err)
				}
				if err = c.Get(ctx, client.ObjectKeyFromObject(pod), pod); !apierrors.IsNotFound(err) {
					t.Fatalf("Pod evidence stranded: %v", err)
				}
				err = c.Get(ctx, key, p)
				if removing {
					if !apierrors.IsNotFound(err) {
						t.Fatalf("participant not released: %v", err)
					}
				} else {
					must(err)
					expected := "Stopped"
					if mode == "suspend" {
						expected = "Suspended"
					}
					condition = meta.FindStatusCondition(p.Status.Conditions, "WorkloadReady")
					if condition == nil || condition.Reason != expected {
						t.Fatal(fmt.Sprint(p.Status.Conditions))
					}
				}
			})
		}
	}
}
