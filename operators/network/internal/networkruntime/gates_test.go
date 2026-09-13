package networkruntime

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// gateFixture supplies an exact immutable artifact and two supported gate boundaries.
func gateFixture() (*api.StacksNetwork, *api.StacksGenesis) {
	root := rootFixture()
	g := &api.StacksGenesis{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "genesis",
			Namespace: root.Namespace,
			UID:       "genesis",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: api.GroupVersion.String(),
					Kind:       "StacksNetwork",
					Name:       root.Name,
					UID:        root.UID,
					Controller: ptr.To(true),
				},
			},
		},
		Spec: api.StacksGenesisSpec{
			Source: api.GenesisSource{NetworkUID: root.UID},
			Bootstrap: api.Bootstrap{
				Gates: []api.Gate{
					{Name: "PrepareBitcoin", BitcoinCeiling: 203},
					{Name: "EnrollPoX4", BitcoinCeiling: 234, TargetCycle: ptr.To[int64](12)},
					{Name: "PrepareNakamoto", BitcoinCeiling: 251},
				},
			},
		},
	}
	root.Status.GenesisRef = &common.Binding{
		Kind:        "StacksGenesis",
		Name:        g.Name,
		UID:         g.UID,
		Fingerprint: foundation.Digest(g.Spec),
	}
	root.Status.GenesisDigest = foundation.Digest(g.Spec.Chain)
	return root, g
}

func TestGateProgressRejectsChangedIdentityOrderAndCeiling(t *testing.T) {
	for _, mode := range []string{"identity", "ceiling", "order", "completion"} {
		t.Run(mode, func(t *testing.T) {
			root, g := gateFixture()
			if err := initializeGates(root, g); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "identity":
				root.Status.Initialization.GenesisUID = "other"
			case "ceiling":
				root.Status.Initialization.AuthorizedCeiling++
			case "order":
				root.Status.Initialization.GateIndex++
			case "completion":
				root.Status.Initialization.Completed = true
			}
			if err := initializeGates(root, g); err == nil {
				t.Fatal("corrupt progress accepted")
			}
		})
	}
}

func TestPreparedReceiptRecoveryKeepsOriginalGateTime(t *testing.T) {
	root, g := gateFixture()
	r := fixtureReconciler(t, g)
	first := metav1.NewTime(time.Now().Add(-10 * time.Minute))
	prepared := metav1.NewTime(first.Add(time.Minute))
	record := &bitcoin.BitcoinInitialization{
		Status: bitcoin.BitcoinInitializationStatus{FirstCeilingObservedAt: &first, PreparedAt: &prepared},
	}
	failed, err := r.projectGates(context.Background(), root, record, nil)
	if err != nil || failed {
		t.Fatalf("prepared receipt recovery failed: %v", err)
	}
	progress := root.Status.Initialization
	if progress.GateIndex != 1 || progress.AuthorizedCeiling != 234 || progress.Completed ||
		!progress.Gates[0].CompletedAt.Equal(&prepared) {
		t.Fatalf("incorrect retained evidence: %+v", progress)
	}
	if progress.Gates[1].CompletedAt != nil {
		t.Fatal("later gate fabricated")
	}
}

func TestPauseDoesNotExtendUnfinishedGateDeadline(t *testing.T) {
	root, g := gateFixture()
	root.Spec.Operation = "Paused"
	r := fixtureReconciler(t, g)
	first := metav1.NewTime(time.Now().Add(-3 * time.Minute))
	record := &bitcoin.BitcoinInitialization{
		Status: bitcoin.BitcoinInitializationStatus{FirstCeilingObservedAt: &first},
	}
	failed, err := r.projectGates(context.Background(), root, record, nil)
	if err != nil || !failed || root.Status.Phase != "Failed" {
		t.Fatalf("deadline extended: failed=%v err=%v", failed, err)
	}
	if root.Status.Initialization.GateIndex != 0 {
		t.Fatal("expired gate advanced")
	}
}

// unavailableGenesisReader simulates an API outage without changing captured objects.
type unavailableGenesisReader struct{ client.Reader }

func (r unavailableGenesisReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if _, ok := obj.(*api.StacksGenesis); ok {
		return fmt.Errorf("temporary API read unavailable")
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func TestTransientGateReadCannotEraseCompletedInitialization(t *testing.T) {
	root, g := gateFixture()
	root.Spec.Operation = "Running"
	root.Status.Phase = "Running"
	set(root, "Initialized", metav1.ConditionTrue, "BootstrapCompleted", "complete")
	set(root, "Running", metav1.ConditionTrue, "Running", "running")
	original := *meta.FindStatusCondition(root.Status.Conditions, "Initialized")
	r := fixtureReconciler(t, g)
	r.Reader = unavailableGenesisReader{r.Reader}
	if _, err := r.reconcileGates(context.Background(), root, &bitcoin.BitcoinInitialization{}, nil); err == nil {
		t.Fatal("outage not exercised")
	}
	got := meta.FindStatusCondition(root.Status.Conditions, "Initialized")
	if !reflect.DeepEqual(got, &original) || root.Status.Phase != "Running" ||
		!meta.IsStatusConditionTrue(root.Status.Conditions, "Running") ||
		!meta.IsStatusConditionPresentAndEqual(root.Status.Conditions, "Operational", metav1.ConditionUnknown) {
		t.Fatalf("history/control lost during current observation outage: %+v", root.Status)
	}
}
