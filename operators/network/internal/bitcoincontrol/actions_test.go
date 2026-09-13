package bitcoincontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// finiteFixture binds a request through the worker and then simulates lifecycle admission transport.
func finiteFixture(t *testing.T) (*testFixture, *action.BitcoinBlockGeneration) {
	t.Helper()
	f := baselineFixture(t)
	ctx := context.Background()
	if err := action.AddToScheme(f.c.Scheme()); err != nil {
		t.Fatal(err)
	}
	f.worker.Input.ActionsEnabled = true
	f.rpc.height = 234
	f.rpc.exists = true
	f.rpc.loaded = true
	f.rpc.imported = true
	request := &action.BitcoinBlockGeneration{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "finite",
			Namespace:         "test",
			UID:               "finite-uid",
			CreationTimestamp: metav1.NewTime(f.now),
			Finalizers:        []string{action.CleanupFinalizer},
		},
		Spec: action.BitcoinBlockGenerationSpec{
			NetworkUID:     f.root.UID,
			BitcoinNodeRef: action.LocalReference{Name: "bitcoin"},
			Address:        testAddress,
			Count:          2,
			Cadence:        action.GenerationCadence{Mode: "Immediate"},
			Timeout:        metav1.Duration{Duration: time.Minute},
		},
	}
	if err := f.c.Create(ctx, request); err != nil {
		t.Fatal(err)
	}
	a, err := f.worker.authorize(ctx, f.readRecord(t))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := f.worker.selectAction(ctx, a, f.readRecord(t))
	if err != nil || !selected {
		t.Fatalf("select: %v %v", selected, err)
	}
	record := f.readRecord(t)
	state := record.Status.Action
	request.Status.AdmittedAt = state.AdmittedAt.DeepCopy()
	executionBinding := binding("BitcoinExecution", record)
	request.Status.AdmittedExecution = &executionBinding
	request.Status.AdmittedNetwork = &state.Network
	request.Status.AdmittedTarget = &state.Target
	request.Status.Phase = "Admitted"
	if err := f.c.Update(ctx, request); err != nil {
		t.Fatal(err)
	}
	return f, request
}

func TestFiniteGenerationRetainsReceiptUntilLifecycleAcknowledgement(t *testing.T) {
	f, request := finiteFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := f.worker.Step(ctx); err != nil {
			t.Fatal(err)
		}
		f.worker.workers.Wait()
	}
	record := f.readRecord(t)
	if record.Status.Armed != nil || record.Status.Action.BlocksGenerated != 2 || record.Status.Reservation == nil ||
		len(f.rpc.calls) != 2 {
		t.Fatalf("finite receipt accounting: %+v sends%v", record.Status, f.rpc.calls)
	}
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if f.readRecord(t).Status.Action == nil {
		t.Fatal("released before lifecycle receipt acknowledgement")
	}
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(request), request); err != nil {
		t.Fatal(err)
	}
	request.Status.Phase = "Completed"
	request.Status.BlocksGenerated = 2
	request.Status.LastDispatchID = record.Status.Action.LastDispatchID
	if err := f.c.Update(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if f.readRecord(t).Status.Action != nil || f.readRecord(t).Status.Reservation != nil {
		t.Fatal("acknowledged action retained exclusion")
	}
	if !generationAccounted(f.readInitial(t), f.readRecord(t)) {
		t.Fatal("baseline cannot consume released finite receipt")
	}
}

func TestFiniteLostArmWriteNeverSendsOrReplays(t *testing.T) {
	f, _ := finiteFixture(t)
	ctx := context.Background()
	base := f.c
	f.worker.Client = interceptor.NewClient(
		base.(client.WithWatch),
		interceptor.Funcs{
			SubResourceUpdate: func(
				ctx context.Context,
				c client.Client,
				sub string,
				object client.Object,
				opts ...client.SubResourceUpdateOption,
			) error {
				err := c.SubResource(sub).Update(ctx, object, opts...)
				if err == nil {
					if e, ok := object.(*bitcoin.BitcoinExecution); ok && e.Status.Armed != nil {
						return errors.New("lost write acknowledgement")
					}
				}
				return err
			},
		},
	)
	if err := f.worker.Step(ctx); err == nil {
		t.Fatal("expected lost arm acknowledgement")
	}
	f.worker.Client = base
	replacement := &Worker{
		Client:       base,
		Reader:       base,
		RPC:          f.rpc,
		Input:        f.worker.Input,
		Now:          f.worker.Now,
		ProcessNonce: "new-process",
	}
	if err := replacement.Step(ctx); err != nil {
		t.Fatal(err)
	}
	record := f.readRecord(t)
	if len(f.rpc.calls) != 0 || record.Status.Armed == nil || !record.Status.Action.EffectUncertain {
		t.Fatalf("unknown Armed reopened: %+v sends%v", record.Status, f.rpc.calls)
	}
}

func TestFinitePauseMaintainsReadsAndNeverSends(t *testing.T) {
	f, _ := finiteFixture(t)
	ctx := context.Background()
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.root), f.root); err != nil {
		t.Fatal(err)
	}
	f.root.Spec.Operation = "Paused"
	if err := f.c.Update(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		f.now = f.now.Add(10 * time.Second)
		if err := f.worker.Step(ctx); err != nil {
			t.Fatal(err)
		}
		if err := f.worker.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	record := f.readRecord(t)
	if len(f.rpc.calls) != 0 || record.Status.Armed != nil || record.Status.Control == nil ||
		f.now.Sub(record.Status.Control.HeartbeatAt.Time) > 5*time.Second ||
		!record.Status.Observation.ObservedAt.Time.Equal(f.now) {
		t.Fatalf("paused finite activity %+v sends%v", record.Status, f.rpc.calls)
	}
}

// deniedActionReader models installation RBAC removal without granting an implicit cancellation signal.
type deniedActionReader struct{ client.Reader }

func (r deniedActionReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	object client.Object,
	opts ...client.GetOption,
) error {
	switch object.(type) {
	case *action.BitcoinBlockGeneration, *action.BitcoinReorganization:
		return errors.New("action read forbidden")
	}
	return r.Reader.Get(ctx, key, object, opts...)
}

func TestFiniteDisablingReadAccessRetainsUnobservedReservation(t *testing.T) {
	f, _ := finiteFixture(t)
	before := f.readRecord(t)
	f.worker.Input.ActionsEnabled = false
	f.worker.Reader = deniedActionReader{Reader: f.c}
	if err := f.worker.Step(context.Background()); err == nil {
		t.Fatal("lost action permission was accepted as cancellation")
	}
	after := f.readRecord(t)
	if after.Status.Action == nil || after.Status.Reservation == nil || after.Status.Action.CleanupAcknowledged ||
		after.Status.Action.StopReason != before.Status.Action.StopReason ||
		len(f.rpc.calls) != 0 {
		t.Fatalf("unobserved reservation changed: %+v", after.Status)
	}
}

func TestFiniteTerminalDrainRetainsUnknownAuthority(t *testing.T) {
	f, _ := finiteFixture(t)
	ctx := context.Background()
	record := f.readRecord(t)
	record.Status.Armed = &bitcoin.BitcoinArmedRPC{
		ID:          "unacknowledged",
		Method:      "Generate",
		Action:      record.Status.Action.Request.DeepCopy(),
		Reservation: *record.Status.Reservation,
	}
	if err := f.c.Status().Update(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.root), f.root); err != nil {
		t.Fatal(err)
	}
	f.root.Spec.Operation = "Stopped"
	if err := f.c.Update(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := f.worker.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	record = f.readRecord(t)
	if record.Status.Drain == nil || record.Status.Drain.Outcome != "Uncertain" || record.Status.Armed == nil ||
		record.Status.Action == nil ||
		!record.Status.Action.EffectUncertain ||
		len(f.rpc.calls) != 0 {
		t.Fatalf("terminal action falsely drained: %+v", record.Status)
	}
}

func TestFiniteReceiptTimeNeverTruncatesLateCompletionOntoDeadline(t *testing.T) {
	f, _ := finiteFixture(t)
	record := f.readRecord(t)
	deadline := record.Status.Action.ExpiresAt.Time
	request := bitcoin.BitcoinArmedRPC{
		ID:     "late",
		Method: "Generate",
		Action: record.Status.Action.Request.DeepCopy(),
	}
	receipt := &bitcoin.BitcoinRPCReceipt{
		Request:    request,
		BlockHash:  "0000000000000000000000000000000000000000000000000000000000000001",
		ReceivedAt: metav1.NewTime(deadline.Add(time.Millisecond)),
	}
	if err := accountActionReceipt(record, receipt); err != nil {
		t.Fatal(err)
	}
	if !record.Status.Action.LastCompletedAt.After(deadline) {
		t.Fatal("late receipt truncated onto deadline")
	}
}
