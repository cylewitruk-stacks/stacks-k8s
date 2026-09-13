package faucetrequest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// admissionFixture represents a complete public faucet admission and a live exact worker Pod.
func admissionFixture(
	t *testing.T,
) (*Reconciler, *stacks.StacksFaucetRequest, *api.StacksNetwork, *api.StacksNetworkParticipant) {
	t.Helper()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	public, _ := identity.FromPrivate(strings.Repeat("0", 63) + "1")
	scheme := runtime.NewScheme()
	_ = api.AddToScheme(scheme)
	_ = stacks.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "root", Generation: 1},
		Spec: api.StacksNetworkSpec{
			Operation: "Running",
			Participants: []api.Participant{
				{Name: "faucet", Kind: "StacksFaucet"},
				{Name: "node", Kind: "StacksNode"},
			},
		},
	}
	owner := metav1.OwnerReference{
		APIVersion: api.GroupVersion.String(),
		Kind:       "StacksNetwork",
		Name:       root.Name,
		UID:        root.UID,
		Controller: ptr.To(true),
	}
	account := &stacks.StacksAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "account", Namespace: "test", UID: "account", Generation: 1},
		Status: common.ResolutionStatus{
			ObservedGeneration: 1,
			Digest:             "account-digest",
			Identity:           &common.PublicIdentity{Address: public.Address, PublicKey: public.PublicKey},
			Conditions:         []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}},
		},
	}
	target := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{
			Name:            foundation.ParticipantName("root", "node"),
			Namespace:       "test",
			UID:             "node",
			OwnerReferences: []metav1.OwnerReference{owner},
		},
		Spec:   api.StacksNetworkParticipantSpec{NetworkUID: "root", Kind: "StacksNode", ParticipantName: "node"},
		Status: api.ParticipantStatus{Admission: &api.Admission{}},
	}
	p := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{
			Name:            foundation.ParticipantName("root", "faucet"),
			Namespace:       "test",
			UID:             "faucet",
			Generation:      1,
			OwnerReferences: []metav1.OwnerReference{owner},
		},
		Spec: api.StacksNetworkParticipantSpec{
			NetworkUID:      "root",
			Kind:            "StacksFaucet",
			ParticipantName: "faucet",
		},
		Status: api.ParticipantStatus{
			Admission: &api.Admission{
				Configuration: api.Configuration{
					StacksFaucet: &stacks.StacksFaucetSpec{
						AccountRef:         &common.NameRef{Name: account.Name},
						TargetNodeRef:      &common.NameRef{Name: "node"},
						MaxRequestMicroSTX: ptr.To(common.Amount("100000")),
					},
				},
				Dependencies: []common.Binding{
					{Kind: "StacksAccount", Name: account.Name, UID: account.UID, Fingerprint: account.Status.Digest},
					{Kind: "StacksNetworkParticipant", Name: target.Name, UID: target.UID},
				},
			},
		},
	}
	p.Status.Admission.PolicyDigest = foundation.Digest(p.Status.Admission.Configuration)
	digest := "sha256:" + strings.Repeat("a", 64)
	worker := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker",
			Namespace: "test",
			UID:       "pod",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: api.GroupVersion.String(),
					Kind:       "StacksNetworkParticipant",
					Name:       p.Name,
					UID:        p.UID,
					Controller: ptr.To(true),
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	root.Status.Identities = []api.InstanceIdentity{
		{
			Name: "faucet",
			UID:  p.UID,
			Worker: &api.WorkerSession{
				Pod:           api.WorkerPodBinding{Kind: "Pod", Name: worker.Name, UID: worker.UID},
				ProfileDigest: digest,
			},
		},
		{Name: "node", UID: target.UID},
	}
	p.Status.Execution = &api.WorkerExecutionStatus{
		PodUID:        worker.UID,
		ProcessNonce:  "process",
		ProfileDigest: digest,
		Phase:         "Active",
	}
	request := &stacks.StacksFaucetRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "request",
			Namespace:         "test",
			UID:               "request",
			CreationTimestamp: metav1.NewTime(now),
		},
		Spec: stacks.StacksFaucetRequestSpec{
			NetworkUID:     root.UID,
			FaucetRef:      common.NameRef{Name: "faucet"},
			Destination:    stacks.Recipient{AccountRef: &common.NameRef{Name: account.Name}},
			AmountMicroSTX: "100000",
			Timeout:        "5m",
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(root, p, target, account, worker, request).Build()
	return &Reconciler{Client: c, Reader: c, Now: func() time.Time { return now }}, request, root, p
}

func TestAdmissionPinsWorkerDeadlineAndInclusiveLimit(t *testing.T) {
	r, request, _, _ := admissionFixture(t)
	admitted, err := r.admit(context.Background(), request)
	if err != nil || admitted.Decision != "Admitted" || admitted.Worker.UID != "pod" ||
		admitted.Faucet.UID != "faucet" ||
		admitted.DestinationAccount.UID != "account" ||
		admitted.FeeMicroSTX != "3000" ||
		admitted.ExpiresAt != "2026-09-11T12:05:00Z" {
		t.Fatalf("complete request not admitted: %+v %v", admitted, err)
	}
	request.Spec.AmountMicroSTX = "100001"
	denied, err := r.admit(context.Background(), request)
	if err != nil || denied.Decision != "Rejected" || denied.Reason != "RequestLimitExceeded" {
		t.Fatalf("limit not enforced: %+v %v", denied, err)
	}
}

func TestAdmissionWaitsForMissingDestinationAndRefusesReplacement(t *testing.T) {
	r, request, root, _ := admissionFixture(t)
	ctx := context.Background()
	request.Spec.Destination.AccountRef.Name = "missing"
	got, err := r.admit(ctx, request)
	if err != nil || got.Decision != "Pending" || got.Reason != "DestinationUnavailable" {
		t.Fatalf("missing destination misclassified: %+v %v", got, err)
	}
	r.Now = func() time.Time { return request.CreationTimestamp.Add(5 * time.Minute) }
	got, err = r.admit(ctx, request)
	if err != nil || got.Decision != "Expired" {
		t.Fatalf("never admitted request did not expire: %+v %v", got, err)
	}
	r.Now = func() time.Time { return request.CreationTimestamp.Time }
	root.Spec.Participants = root.Spec.Participants[1:]
	if err = r.Client.Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	got, err = r.admit(ctx, request)
	if err != nil || got.Decision != "Rejected" || got.Reason != "FaucetRemoved" {
		t.Fatalf("single-use faucet followed removal: %+v %v", got, err)
	}
}

func TestAdmittedExpiryAndLateExactEvidenceRemainDistinct(t *testing.T) {
	r, request, _, _ := admissionFixture(t)
	admitted, err := r.admit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Status.Admission = admitted
	now := request.CreationTimestamp.Add(5 * time.Minute)
	phase, _ := ProjectPhase(request, now)
	if phase != "Inconclusive" {
		t.Fatal("absence of execution invented no-send expiry")
	}
	request.Status.Execution = &stacks.FaucetExecution{
		Phase:            "Completed",
		NetworkUID:       "root",
		FaucetUID:        "faucet",
		WorkerUID:        "pod",
		ProcessNonce:     "process",
		Destination:      admitted.Destination,
		AmountMicroSTX:   admitted.AmountMicroSTX,
		TxID:             strings.Repeat("a", 64),
		InclusionBlockID: strings.Repeat("b", 64),
	}
	phase, _ = ProjectPhase(request, now)
	if phase != "Completed" {
		t.Fatal("late exact inclusion did not refine inconclusive projection")
	}
	request.Status.Execution.WorkerUID = "replacement"
	phase, _ = ProjectPhase(request, now)
	if phase != "Inconclusive" {
		t.Fatal("replacement worker supplied old request evidence")
	}
	request.Status.Execution.WorkerUID = "pod"
	request.Status.Execution.Phase = "Expired"
	request.Status.Execution.TxID = ""
	request.Status.Execution.NoSend = true
	phase, _ = ProjectPhase(request, now)
	if phase != "Expired" {
		t.Fatal("affirmative original worker no-send evidence was lost")
	}
}

func TestCapacityExcludesPersistedTerminalRequests(t *testing.T) {
	r, request, _, _ := admissionFixture(t)
	ctx := context.Background()
	admitted, err := r.admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < Capacity; i++ {
		item := request.DeepCopy()
		item.Name = fmt.Sprintf("active-%04d", i)
		item.UID = types.UID(item.Name)
		item.ResourceVersion = ""
		item.Status.Admission = admitted.DeepCopy()
		if err = r.Client.Create(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.admit(ctx, request)
	if err != nil || got.Decision != "Rejected" || got.Reason != "CapacityExceeded" {
		t.Fatalf("capacity not bounded: %+v %v", got, err)
	}
	var settled stacks.StacksFaucetRequest
	if err = r.Client.Get(ctx, client.ObjectKey{Namespace: "test", Name: "active-0000"}, &settled); err != nil {
		t.Fatal(err)
	}
	settled.Status.Execution = &stacks.FaucetExecution{
		Phase:          "Rejected",
		Reason:         "InsufficientFunds",
		NetworkUID:     "root",
		FaucetUID:      "faucet",
		WorkerUID:      "pod",
		ProcessNonce:   "process",
		Destination:    admitted.Destination,
		AmountMicroSTX: admitted.AmountMicroSTX,
		NoSend:         true,
	}
	if err = r.Client.Update(ctx, &settled); err != nil {
		t.Fatal(err)
	}
	got, err = r.admit(ctx, request)
	if err != nil || got.Decision != "Admitted" {
		t.Fatalf("persisted terminal request retained lifetime capacity: %+v %v", got, err)
	}
}

func TestDeadlinePreservesSubsecondAbsoluteTimeout(t *testing.T) {
	_, request, _, _ := admissionFixture(t)
	request.Spec.Timeout = "1.000000001s"
	expiry, err := Deadline(request)
	if err != nil || expiry.Sub(request.CreationTimestamp.Time) != time.Second+time.Nanosecond {
		t.Fatalf("deadline rounded or extended: %v %v", expiry, err)
	}
}

func TestUncertainGrantConsumesCapacityBeforeVisibleCommit(t *testing.T) {
	r, request, _, _ := admissionFixture(t)
	ctx := context.Background()
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(request), request); err != nil {
		t.Fatal(err)
	}
	base := r.Client
	r.Client = interceptor.NewClient(
		base.(client.WithWatch),
		interceptor.Funcs{
			SubResourcePatch: func(
				context.Context,
				client.Client,
				string,
				client.Object,
				client.Patch,
				...client.SubResourcePatchOption,
			) error {
				return errors.New("grant still in flight")
			},
		},
	)
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)}); err == nil {
		t.Fatal("uncertain grant write not exercised")
	}
	if r.uncertain[request.UID] != "pod" {
		t.Fatal("possibly granted slot was forgotten")
	}
	for i := 1; i < Capacity; i++ {
		r.uncertain[types.UID(fmt.Sprintf("unknown-%d", i))] = "pod"
	}
	next := request.DeepCopy()
	next.UID = "next"
	next.Name = "next"
	admission, err := r.admit(ctx, next)
	if err != nil || admission.Reason != "CapacityExceeded" {
		t.Fatalf("new request raced uncertain grants: %+v %v", admission, err)
	}
	// A later visible grant occupies exactly one slot, never two.
	confirmed, err := (&Reconciler{Client: base, Reader: r.Reader, Now: r.Now}).admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Status.Admission = confirmed
	if err := base.Update(ctx, request); err != nil {
		t.Fatal(err)
	}
	count, err := r.activeCount(ctx, request.Namespace, "pod")
	if err != nil || count != Capacity || r.uncertain[request.UID] != "" {
		t.Fatalf("confirmed grant double counted: %d %v", count, err)
	}
}
