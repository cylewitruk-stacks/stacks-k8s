package networkruntime

import (
	"context"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBitcoinPauseRequiresFreshHeartbeatIndependentOfOriginalAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name               string
		heartbeat          time.Duration
		absentNonce, armed bool
		want               bool
	}{{name: "fresh", want: true}, {name: "stale", heartbeat: -foundation.ObservationFreshness() - time.Second}, {name: "future", heartbeat: time.Minute}, {name: "missing process", absentNonce: true}, {name: "outstanding arm", armed: true}} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			root := rootFixture()
			root.Spec.Operation = "Paused"
			root.Spec.Participants = []api.Participant{{Name: "btc", Kind: "BitcoinNode"}}
			root.Status.Identities = []api.InstanceIdentity{{Name: "btc", UID: "participant"}}
			root.Status.Bitcoin = &api.BitcoinRuntimeStatus{ExecutionRefs: []common.Binding{{Kind: "BitcoinExecution", Name: "execution", UID: "execution-uid"}}}
			record := &bitcoin.BitcoinExecution{ObjectMeta: metav1.ObjectMeta{Name: "execution", Namespace: root.Namespace, UID: "execution-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetwork", Name: root.Name, UID: root.UID, Controller: ptr.To(true)}}}, Spec: bitcoin.BitcoinExecutionSpec{NetworkUID: root.UID, Participant: common.Binding{Kind: "StacksNetworkParticipant", Name: "node", UID: "participant"}}, Status: bitcoin.BitcoinExecutionStatus{Control: &bitcoin.BitcoinControlAcknowledgement{NetworkGeneration: root.Generation, ProcessNonce: "current-process", Operation: "Paused", ObservedAt: metav1.NewTime(now.Add(-time.Hour)), HeartbeatAt: metav1.NewTime(now.Add(tc.heartbeat))}}}
			if tc.absentNonce {
				record.Status.Control.ProcessNonce = ""
			}
			if tc.armed {
				record.Status.Armed = &bitcoin.BitcoinArmedRPC{ID: "unknown"}
			}
			scheme := runtime.NewScheme()
			if err := bitcoin.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(record).Build()
			r := &Reconciler{Reader: c}
			got, err := r.bitcoinPaused(context.Background(), root)
			if err != nil || got != tc.want {
				t.Fatalf("pause=%v want%v error%v", got, tc.want, err)
			}
		})
	}
}
