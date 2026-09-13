package stacksworker

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// outageClient independently fails current reads and minimal execution publication.
type outageClient struct {
	client.Client
	readErr, patchErr error
	participantOnly   bool
}

func (c *outageClient) Get(ctx context.Context, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
	if c.readErr != nil {
		if _, ok := object.(*api.StacksNetworkParticipant); !c.participantOnly || ok {
			return c.readErr
		}
	}
	return c.Client.Get(ctx, key, object, opts...)
}
func (c *outageClient) Status() client.SubResourceWriter {
	return &outageWriter{SubResourceWriter: c.Client.Status(), parent: c}
}

// outageWriter leaves status untouched on a simulated transport outage.
type outageWriter struct {
	client.SubResourceWriter
	parent *outageClient
}

func (w *outageWriter) Patch(ctx context.Context, o client.Object, p client.Patch, opts ...client.SubResourcePatchOption) error {
	if w.parent.patchErr != nil {
		return w.parent.patchErr
	}
	return w.SubResourceWriter.Patch(ctx, o, p, opts...)
}

// baselineRole simulates a locally validated baseline and records every explicitly authorized send.
type baselineRole struct {
	steps, sends int
	cached       bool
	applied      string
	first        metav1.Time
}

func (r *baselineRole) Step(ctx context.Context, s Snapshot) (RoleResult, error) {
	r.steps++
	r.cached = s.CachedApplied
	if !s.CachedApplied {
		r.applied = s.Participant.Status.Admission.PolicyDigest
		s.RememberApplied(r.applied)
	}
	if !s.Paused && s.Authorize(ctx) == nil {
		r.sends++
	}
	if r.first.IsZero() {
		r.first = metav1.NewTime(time.Unix(100, 0))
	}
	return RoleResult{AppliedPolicyDigest: r.applied, Reason: "Observed", Transactions: &api.TransactionExecutionStatus{Offered: uint64(r.sends), Accepted: uint64(r.sends), Included: uint64(r.sends), LastInclusion: &api.TransactionInclusion{TxID: "original", ObservedAt: r.first}}, RequeueAfter: time.Second}, nil
}
func (r *baselineRole) Drain(context.Context, Snapshot) (DrainResult, error) {
	return DrainResult{Done: true, Settled: true}, nil
}

// baselineFixture activates the process and confirms one complete applied baseline report.
func baselineFixture(t *testing.T) (*Runtime, *outageClient, *baselineRole) {
	t.Helper()
	root, p, pod, profile := fixture(t)
	ProjectSession(root, p, pod, nil, time.Now())
	c := &outageClient{Client: &statusClient{Client: fakeClient(t, root, p, pod)}}
	role := &baselineRole{}
	r := &Runtime{Client: c, Namespace: p.Namespace, ParticipantName: p.Name, NetworkUID: root.UID, ParticipantUID: p.UID, PodName: pod.Name, PodUID: pod.UID, Profile: profile, Role: role, Prerequisites: func(context.Context, Snapshot) error { return nil }}
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if role.sends != 1 || r.appliedSnapshot == nil || !r.processAcknowledged {
		t.Fatal("initial activation did not publish/validate")
	}
	return r, c, role
}
func TestTransientClassificationDoesNotHideDefiniteDenial(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, io.EOF, apierrors.NewServiceUnavailable("offline"), apierrors.NewTimeoutError("offline", 1)} {
		if !TransientAPIError(err) {
			t.Fatalf("availability error rejected: %v", err)
		}
	}
	for _, err := range []error{nil, context.Canceled, errors.New("invalid identity"), apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "p", errors.New("denied")), apierrors.NewUnauthorized("denied"), apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "p")} {
		if TransientAPIError(err) {
			t.Fatalf("definite denial cached: %v", err)
		}
	}
}
func TestSurvivingBaselineCoalescesAPIOutageWithoutLosingOriginalProgress(t *testing.T) {
	r, c, role := baselineFixture(t)
	ctx := context.Background()
	nonce := r.nonce
	c.readErr = apierrors.NewServiceUnavailable("offline")
	c.patchErr = c.readErr
	for i := 0; i < 3; i++ {
		if _, err := r.Reconcile(ctx); err == nil {
			t.Fatal("publication outage hidden")
		}
	}
	if role.sends != 4 || !role.cached || r.pendingReport.Transactions.Included != 4 || r.nonce != nonce {
		t.Fatalf("baseline stopped/lost accounting: sends=%d report=%+v", role.sends, r.pendingReport)
	}
	original := r.pendingReport.Transactions.LastInclusion.ObservedAt
	c.readErr = nil
	c.patchErr = nil
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if role.sends != 4 || r.pendingReport != nil {
		t.Fatal("publication recovery performed another operation")
	}
	var p api.StacksNetworkParticipant
	if err := c.Get(ctx, client.ObjectKey{Namespace: r.Namespace, Name: r.ParticipantName}, &p); err != nil {
		t.Fatal(err)
	}
	if p.Status.Execution.Transactions.Included != 4 || !p.Status.Execution.Transactions.LastInclusion.ObservedAt.Equal(&original) {
		t.Fatal("coalescing regressed counters or freshened original inclusion")
	}
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if role.sends != 5 {
		t.Fatal("recovery baseline stalled")
	}
}
func TestPartialPauseObservationPermanentlyHoldsCachedSendsUntilLiveValidation(t *testing.T) {
	r, c, role := baselineFixture(t)
	ctx := context.Background()
	var root api.StacksNetwork
	key := client.ObjectKey{Namespace: r.Namespace, Name: "network"}
	if err := c.Client.Get(ctx, key, &root); err != nil {
		t.Fatal(err)
	}
	root.Spec.Participants[0].Control = &api.Control{Paused: ptr.To(true)}
	root.Generation++
	if err := c.Client.Update(ctx, &root); err != nil {
		t.Fatal(err)
	}
	c.readErr = apierrors.NewServiceUnavailable("participant unavailable")
	c.participantOnly = true
	c.patchErr = c.readErr
	_, _ = r.Reconcile(ctx)
	c.participantOnly = false
	_, _ = r.Reconcile(ctx)
	if role.sends != 1 || !r.cacheHeld {
		t.Fatal("partially observed pause revived cached sends")
	}
	c.readErr = nil
	c.patchErr = nil
	_, _ = r.Reconcile(ctx)
	_, _ = r.Reconcile(ctx)
	if role.sends != 1 {
		t.Fatal("paused live worker sent")
	}
}
func TestDefiniteReadDenialCannotBecomeCachedAuthorityOnLaterOutage(t *testing.T) {
	for _, denial := range []error{apierrors.NewUnauthorized("revoked"), apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "worker"), errors.New("UID changed")} {
		r, c, role := baselineFixture(t)
		c.readErr = denial
		_, _ = r.Reconcile(context.Background())
		c.readErr = apierrors.NewServiceUnavailable("offline")
		c.patchErr = c.readErr
		_, _ = r.Reconcile(context.Background())
		if role.steps != 1 || role.sends != 1 {
			t.Fatal("definite denial later revived cached execution")
		}
	}
}
func TestColdProcessAndFaucetNeverReceiveCachedBaselineAuthority(t *testing.T) {
	r, c, role := baselineFixture(t)
	cold := &Runtime{Client: c, Namespace: r.Namespace, ParticipantName: r.ParticipantName, NetworkUID: r.NetworkUID, ParticipantUID: r.ParticipantUID, PodName: r.PodName, PodUID: r.PodUID, Profile: r.Profile, Role: &baselineRole{}, Prerequisites: r.Prerequisites}
	c.readErr = apierrors.NewServiceUnavailable("offline")
	c.patchErr = c.readErr
	if _, err := cold.Reconcile(context.Background()); err == nil || cold.appliedSnapshot != nil {
		t.Fatal("new process recovered cached authority")
	}
	r.appliedSnapshot.Participant.Spec.Kind = "StacksFaucet"
	_, _ = r.Reconcile(context.Background())
	if role.steps != 1 {
		t.Fatal("request worker used baseline cache")
	}
}
func TestCounterRegressionIsRejected(t *testing.T) {
	previous := &api.TransactionExecutionStatus{Offered: 3, Included: 2}
	if executionCountersAdvance(previous, &api.TransactionExecutionStatus{Offered: 2, Included: 2}) || executionCountersAdvance(previous, nil) {
		t.Fatal("regressing summary accepted")
	}
}

func TestKnownNewAdmissionCannotAuthorizePreviouslyPreparedSnapshot(t *testing.T) {
	r, c, role := baselineFixture(t)
	ctx := context.Background()
	prepared, err := r.fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var p api.StacksNetworkParticipant
	key := client.ObjectKey{Namespace: r.Namespace, Name: r.ParticipantName}
	if err := c.Client.Get(ctx, key, &p); err != nil {
		t.Fatal(err)
	}
	p.Status.Admission.PolicyDigest = "new-policy"
	if err := c.Client.Status().Update(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Authorize(ctx); err == nil {
		t.Fatal("old prepared bytes passed newer admission")
	}
	c.readErr = apierrors.NewServiceUnavailable("offline")
	c.patchErr = c.readErr
	_, _ = r.Reconcile(ctx)
	if role.sends != 1 {
		t.Fatal("observed policy mismatch became cached send authority")
	}
}
