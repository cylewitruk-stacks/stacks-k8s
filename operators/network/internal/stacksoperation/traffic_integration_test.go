//go:build integration

package stacksoperation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestTrafficUnobservedInclusionStatusRemainsPublishable exercises role output against the served schema.
func TestTrafficUnobservedInclusionStatusRemainsPublishable(t *testing.T) {
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
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	scheme := runtime.NewScheme()
	must(api.AddToScheme(scheme))
	must(corev1.AddToScheme(scheme))
	c, err := client.New(config, client.Options{Scheme: scheme})
	must(err)
	ctx := context.Background()
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "traffic-test"}}))
	p := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{Name: "traffic", Namespace: "traffic-test"},
		Spec: api.StacksNetworkParticipantSpec{
			NetworkUID:      "network-uid",
			ParticipantName: "traffic",
			Kind:            "StacksTransactionProduction",
			Configuration:   api.Configuration{StacksTransactionProduction: &stacks.StacksTransactionProductionSpec{}},
		},
	}
	must(c.Create(ctx, p))
	r, node, s, _, now := transferFixture(t)
	for index := uint64(0); index < 2; index++ {
		s.Paused = false
		node.account.Nonce = 7 + index
		*now = now.Add(10 * time.Second)
		offer, err := r.Step(ctx, s)
		must(err)
		if offer.Reason != "Accepted" {
			t.Fatalf("offer: %+v", offer)
		}
		node.inclusion = rpc.Inclusion{Found: true, Success: true, BlockID: strings.Repeat("b", 64)}
		node.viewErr = errors.New("canonical recheck unavailable")
		*now = now.Add(time.Second)
		result, err := r.Step(ctx, s)
		must(err)
		owned := api.ParticipantStatus{
			Execution: &api.WorkerExecutionStatus{
				PodUID:              "worker-pod",
				ProcessNonce:        "process",
				ProfileDigest:       foundation.Digest("profile"),
				AppliedPolicyDigest: result.AppliedPolicyDigest,
				ObservedGeneration:  p.Generation,
				NetworkGeneration:   1,
				Phase:               "Active",
				Reason:              result.Reason,
				ObservedAt:          metav1.NewTime(*now),
				Pending:             result.Pending,
				Transactions:        result.Transactions,
				Traffic:             result.Traffic,
			},
		}
		invalid := owned.DeepCopy()
		invalid.Execution.Traffic = r.traffic.DeepCopy()
		if err := participantstatus.Apply(
			ctx,
			c,
			p,
			*invalid,
			"stacks-network-worker-execution",
		); !apierrors.IsInvalid(
			err,
		) {
			t.Fatalf("schema accepted unobserved canonical evidence: %v", err)
		}
		must(participantstatus.Apply(ctx, c, p, owned, "stacks-network-worker-execution"))
		must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
		if p.Status.Execution.Traffic != nil || p.Status.Execution.Transactions.Included != index+1 ||
			// #nosec G115 -- Small deterministic fixture counters/values are bounded by the test setup.
			uint64(node.sends) != index+1 {
			t.Fatal("unobserved report lost receipt or retained obsolete traffic")
		}
		inclusion := p.Status.Execution.Transactions.LastInclusion.DeepCopy()
		node.viewErr = nil
		s.Paused = true
		*now = now.Add(2 * time.Second)
		recovered, err := r.Step(ctx, s)
		must(err)
		owned.Execution.Traffic = recovered.Traffic
		owned.Execution.Transactions = recovered.Transactions
		owned.Execution.ObservedAt = metav1.NewTime(*now)
		must(participantstatus.Apply(ctx, c, p, owned, "stacks-network-worker-execution"))
		must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
		if p.Status.Execution.Traffic == nil || !p.Status.Execution.Traffic.Available ||
			p.Status.Execution.Traffic.ObservedAt.IsZero() ||
			!reflect.DeepEqual(inclusion, p.Status.Execution.Transactions.LastInclusion) ||
			// #nosec G115 -- Small deterministic fixture counters/values are bounded by the test setup.
			uint64(node.sends) != index+1 {
			t.Fatal("native recovery changed original receipt or resubmitted")
		}
	}
}
