//go:build integration

package faucetrequest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestFaucetAPIImmutabilitySSAAndUncertainAdmission(t *testing.T) {
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
		if e := environment.Stop(); e != nil {
			t.Error(e)
		}
	})
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = stacks.AddToScheme(scheme)
	c, err := client.NewWithWatch(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "faucet"}}))
	_, prototype, _, _ := admissionFixture(t)
	prototype.Namespace = "faucet"
	prototype.UID = ""
	prototype.ResourceVersion = ""
	prototype.CreationTimestamp = metav1.Time{}
	prototype.Spec.Destination = stacks.Recipient{Address: ptrAddress("ST000000000000000000002AMW42H")}
	for _, timeout := range []common.Duration{"0s", "2h", "invalid"} {
		bad := prototype.DeepCopy()
		bad.Name = "bad-timeout"
		bad.Spec.Timeout = timeout
		if err := c.Create(ctx, bad); err == nil || !apierrors.IsInvalid(err) {
			t.Fatalf("invalid timeout admitted: %s %v", timeout, err)
		}
	}
	request := prototype.DeepCopy()
	request.Spec.Timeout = "1.000000001s"
	must(c.Create(ctx, request))
	changed := request.DeepCopy()
	changed.Spec.AmountMicroSTX = "2"
	if err := c.Update(ctx, changed); err == nil || !apierrors.IsInvalid(err) {
		t.Fatalf("request spec changed: %v", err)
	}
	expiry, err := Deadline(request)
	must(err)
	admission := baseAdmission(request, expiry, "Admitted", "WorkerBound")
	admission.Faucet = &stacks.FaucetBinding{Kind: "StacksNetworkParticipant", Name: "faucet", UID: "faucet"}
	admission.Worker = &stacks.FaucetBinding{Kind: "Pod", Name: "worker", UID: "pod"}
	admission.ProfileDigest = "sha256:" + strings.Repeat("a", 64)
	admission.SourceAccount = &stacks.FaucetBinding{Kind: "StacksAccount", Name: "source", UID: "source"}
	admission.Target = &stacks.FaucetBinding{Kind: "StacksNetworkParticipant", Name: "node", UID: "node"}
	admission.Destination = *request.Spec.Destination.Address
	admission.AmountMicroSTX = request.Spec.AmountMicroSTX
	admission.FeeMicroSTX = "3000"
	original := request.DeepCopy()
	lost := interceptor.NewClient(
		c,
		interceptor.Funcs{
			SubResourcePatch: func(
				ctx context.Context,
				base client.Client,
				sub string,
				obj client.Object,
				patch client.Patch,
				opts ...client.SubResourcePatchOption,
			) error {
				if err := base.SubResource(sub).Patch(ctx, obj, patch, opts...); err != nil {
					return err
				}
				return errors.New("admission acknowledgement lost")
			},
		},
	)
	if err := ApplyStatus(
		ctx,
		lost,
		request,
		stacks.FaucetRequestStatus{Admission: admission, Phase: "Pending"},
		AdmissionManager,
	); err == nil {
		t.Fatal("lost acknowledgement not injected")
	}
	// The stale negative write cannot overwrite an uncertain successful admission.
	negative := baseAdmission(original, expiry, "Expired", "DeadlineBeforeAdmission")
	if err := ApplyStatus(
		ctx,
		c,
		original,
		stacks.FaucetRequestStatus{Admission: negative, Phase: "Expired"},
		AdmissionManager,
	); err == nil {
		t.Fatal("stale negative write erased granted authority")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(request), request))
	reconciler := &Reconciler{Client: c, Reader: c, Now: func() time.Time { return expiry }}
	_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)})
	must(err)
	must(c.Get(ctx, client.ObjectKeyFromObject(request), request))
	if request.Status.Phase != "Inconclusive" || request.Status.Admission.Decision != "Admitted" ||
		request.Status.Admission.ExpiresAt != expiry.Format(time.RFC3339Nano) {
		t.Fatalf("uncertain admission manufactured expiry: %+v", request.Status)
	}
	execution := &stacks.FaucetExecution{
		Phase:            "Completed",
		Reason:           "Included",
		NetworkUID:       request.Spec.NetworkUID,
		FaucetUID:        "faucet",
		WorkerUID:        "pod",
		ProcessNonce:     "process",
		Destination:      admission.Destination,
		AmountMicroSTX:   admission.AmountMicroSTX,
		TxID:             strings.Repeat("b", 64),
		InclusionBlockID: strings.Repeat("c", 64),
		ObservedAt:       metav1.Now(),
	}
	must(ApplyStatus(ctx, c, request, stacks.FaucetRequestStatus{Execution: execution}, ExecutionManager))
	_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)})
	must(err)
	must(c.Get(ctx, client.ObjectKeyFromObject(request), request))
	if request.Status.Phase != "Completed" || request.Status.Execution.TxID != execution.TxID ||
		request.Status.Admission.Destination != admission.Destination {
		t.Fatal("disjoint SSA writers lost fields")
	}
	managers := map[string]bool{}
	for _, entry := range request.ManagedFields {
		if entry.Subresource == "status" {
			managers[entry.Manager] = true
		}
	}
	if !managers[AdmissionManager] || !managers[ExecutionManager] {
		t.Fatalf("status managers not split: %v", managers)
	}

	// Only matching, allowlisted native refusals may terminate a sent request without inclusion.
	for _, reason := range []string{
		"RejectedFeeTooLow",
		"RejectedBadNonce",
		"RejectedConflictingNonceInMempool",
		"RejectedNotEnoughFunds",
		"RejectedOther",
		"RejectedServerFailureDatabase",
	} {
		refused := execution.DeepCopy()
		refused.Phase, refused.Reason, refused.InclusionBlockID = "Rejected", reason, ""
		err := ApplyStatus(ctx, c, request, stacks.FaucetRequestStatus{Execution: refused}, ExecutionManager)
		if reason == "RejectedOther" || reason == "RejectedServerFailureDatabase" {
			if !apierrors.IsInvalid(err) {
				t.Fatalf("unconfirmed refusal accepted: %s %v", reason, err)
			}
			continue
		}
		must(err)
		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)})
		must(err)
		must(c.Get(ctx, client.ObjectKeyFromObject(request), request))
		if request.Status.Phase != "Rejected" || request.Status.Execution.NoSend ||
			request.Status.Execution.TxID != execution.TxID ||
			request.Status.Admission.Worker.UID != "pod" {
			t.Fatalf("rejection projection lost sent evidence/admission: %+v", request.Status)
		}
	}
	stale := request.DeepCopy()
	must(c.Delete(ctx, request))
	replacement := prototype.DeepCopy()
	replacement.Name = request.Name
	must(c.Create(ctx, replacement))
	if err := ApplyStatus(
		ctx,
		c,
		stale,
		stacks.FaucetRequestStatus{Execution: execution},
		ExecutionManager,
	); err == nil {
		t.Fatal("old worker outcome adopted replacement request")
	}
}

// ptrAddress provides an explicit address destination in API fixtures.
func ptrAddress(address string) *string { return &address }
