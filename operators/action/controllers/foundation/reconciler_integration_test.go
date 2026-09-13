//go:build integration

package foundation

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestActionAPIAdmissionAndReceiptOwnership(t *testing.T) {
	ctx := context.Background()
	base := filepath.Join("..", "..", "..", "..", "charts")
	env := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join(base, "stacks-network-operator", "crds"), filepath.Join(base, "stacks-action-operator", "crds")}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.37.0", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest")}
	config, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, api.AddToScheme, bitcoin.AddToScheme, action.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "actions"}}))
	rendered, err := exec.Command("helm", "template", "action", filepath.Join(base, "stacks-action-operator"), "--namespace", "actions", "--kube-version", "1.37.0", "--set", "bitcoinReorganization.enabled=true").Output()
	must(err)
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered), 4096)
	for {
		object := &unstructured.Unstructured{}
		err := decoder.Decode(object)
		if err == io.EOF {
			break
		}
		must(err)
		switch object.GetKind() {
		case "Role", "RoleBinding", "ServiceAccount":
			object.SetNamespace("actions")
			must(c.Create(ctx, object))
		}
	}
	user, err := env.AddUser(envtest.User{Name: "system:serviceaccount:actions:action", Groups: []string{"system:serviceaccounts", "system:serviceaccounts:actions"}}, config)
	must(err)
	restricted, err := client.New(user.Config(), client.Options{Scheme: scheme})
	must(err)
	denied := restricted.Get(ctx, client.ObjectKey{Namespace: "actions", Name: "private"}, &corev1.Secret{})
	if !apierrors.IsForbidden(denied) {
		t.Fatalf("action controller received Secret authority: %v", denied)
	}

	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "actions"}, Spec: api.StacksNetworkSpec{Operation: "Running"}}
	must(c.Create(ctx, root))
	participant := common.Binding{Kind: "StacksNetworkParticipant", Name: "node", UID: "node-uid"}
	record := &bitcoin.BitcoinExecution{ObjectMeta: metav1.ObjectMeta{Name: "execution", Namespace: root.Namespace}, Spec: bitcoin.BitcoinExecutionSpec{NetworkUID: root.UID, Participant: participant}}
	must(controllerutil.SetControllerReference(root, record, scheme))
	must(c.Create(ctx, record))
	root.Status.Bitcoin = &api.BitcoinRuntimeStatus{ExecutionRefs: []common.Binding{{Kind: "BitcoinExecution", Name: record.Name, UID: record.UID}}}
	must(c.Status().Update(ctx, root))
	for _, kind := range []string{"BitcoinBlockGeneration", "BitcoinReorganization"} {
		t.Run(kind, func(t *testing.T) {
			var prototype client.Object = &action.BitcoinBlockGeneration{}
			if kind == "BitcoinReorganization" {
				prototype = &action.BitcoinReorganization{}
			}
			r := &Reconciler{Client: restricted, Reader: restricted, Prototype: prototype}
			object, status, err := r.object()
			must(err)
			object.SetName("request")
			object.SetNamespace(root.Namespace)
			generation := action.BitcoinBlockGenerationSpec{NetworkUID: root.UID, BitcoinNodeRef: action.LocalReference{Name: "bitcoin"}, Count: 1, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn", Cadence: action.GenerationCadence{Mode: "Immediate"}, Timeout: metav1.Duration{Duration: time.Minute}}
			reorg := action.BitcoinReorganizationSpec{NetworkUID: root.UID, BitcoinNodeRef: generation.BitcoinNodeRef, Depth: 1, Address: generation.Address, Timeout: generation.Timeout, BoundaryPolicy: action.ReorganizationBoundaryPolicy{AllowEpochBoundaryCrossing: true, AllowPreparePhaseBoundaryCrossing: true, AllowRewardCycleBoundaryCrossing: true}}
			switch o := object.(type) {
			case *action.BitcoinBlockGeneration:
				o.Spec = generation
			case *action.BitcoinReorganization:
				o.Spec = reorg
			}
			must(c.Create(ctx, object))
			req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)}
			_, err = r.Reconcile(ctx, req)
			must(err)
			must(c.Get(ctx, req.NamespacedName, object))
			if !controllerutil.ContainsFinalizer(object, action.CleanupFinalizer) {
				t.Fatal("reservation retention absent")
			}
			changed := object.DeepCopyObject().(client.Object)
			switch o := changed.(type) {
			case *action.BitcoinBlockGeneration:
				o.Spec.Count = 2
			case *action.BitcoinReorganization:
				o.Spec.Depth = 2
			}
			if err := c.Update(ctx, changed); !apierrors.IsInvalid(err) {
				t.Fatalf("immutable action changed: %v", err)
			}
			invalid := object.DeepCopyObject().(client.Object)
			invalid.SetName("invalid")
			invalid.SetUID("")
			invalid.SetResourceVersion("")
			switch o := invalid.(type) {
			case *action.BitcoinBlockGeneration:
				o.Spec.Count = 101
			case *action.BitcoinReorganization:
				o.Spec.BoundaryPolicy.AllowEpochBoundaryCrossing = false
			}
			if err := c.Create(ctx, invalid); !apierrors.IsInvalid(err) {
				t.Fatalf("invalid bounded input admitted: %v", err)
			}
			must(c.Get(ctx, client.ObjectKeyFromObject(record), record))
			now := metav1.NewTime(time.Now().UTC().Truncate(time.Second))
			state := &bitcoin.BitcoinActionReservation{Request: common.Binding{Kind: kind, Name: object.GetName(), UID: object.GetUID()}, Network: action.NetworkIdentity{Name: root.Name, UID: string(root.UID), ObservedGeneration: root.Generation}, AdmittedAt: now, ExpiresAt: metav1.NewTime(object.GetCreationTimestamp().Add(time.Minute)), Runtime: bitcoin.BitcoinTargetIdentity{Participant: participant}, Target: action.TargetIdentity{UID: string(participant.UID)}}
			if kind == "BitcoinBlockGeneration" {
				state.Generation = &generation
			} else {
				state.Reorganization = &reorg
			}
			record.Status.Action = state
			record.Status.Reservation = state.Request.DeepCopy()
			must(c.Status().Update(ctx, record))
			_, err = r.Reconcile(ctx, req)
			must(err)
			must(c.Get(ctx, req.NamespacedName, object))
			if status.Phase != "Admitted" || status.AdmittedExecution == nil || status.AdmittedExecution.UID != record.UID {
				t.Fatalf("exact record not admitted: %+v", status)
			}
			managed := false
			for _, entry := range object.GetManagedFields() {
				if entry.Manager == "stacks-action-foundation-"+kind && entry.Subresource == "status" {
					managed = true
				}
			}
			if !managed {
				t.Fatal("fixed action status writer absent")
			}
			stale := record.DeepCopy()
			must(c.Get(ctx, client.ObjectKeyFromObject(record), record))
			record.Status.Action.BlocksGenerated = 1
			record.Status.Action.LastDispatchID = "receipt-one"
			record.Status.Action.LastCompletedAt = now.DeepCopy()
			if kind == "BitcoinReorganization" {
				record.Status.Action.BlocksGenerated = 2
				record.Status.Action.InvalidationAcknowledged = true
				record.Status.Action.CleanupAcknowledged = true
				record.Status.Action.FinalChain = &action.BitcoinChainPoint{Hash: "final", Height: 3, Chainwork: "4"}
			}
			must(c.Status().Update(ctx, record))
			stale.Status.Action.StopReason = "stale"
			if err := c.Status().Update(ctx, stale); !apierrors.IsConflict(err) {
				t.Fatalf("stale execution write accepted: %v", err)
			}
			_, err = r.Reconcile(ctx, req)
			must(err)
			must(c.Get(ctx, req.NamespacedName, object))
			if status.Phase != "Completed" || status.LastDispatchID != "receipt-one" {
				t.Fatalf("receipts not projected: %+v", status)
			}
			must(c.Get(ctx, client.ObjectKeyFromObject(record), record))
			if record.Status.Action == nil || record.Status.Reservation == nil {
				t.Fatal("lifecycle mutated worker authority")
			}
			record.Status.Action = nil
			record.Status.Reservation = nil
			must(c.Status().Update(ctx, record))
			_, err = r.Reconcile(ctx, req)
			must(err)
			must(c.Get(ctx, req.NamespacedName, object))
			if controllerutil.ContainsFinalizer(object, action.CleanupFinalizer) {
				t.Fatal("settled worker exclusion retained finalizer")
			}
		})
	}
}
