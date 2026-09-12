package stacksoperation

import (
	"context"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// publicFixture contains only API identity facts and no private Secret objects.
func publicFixture(t *testing.T) (PublicInputs, stacksworker.Snapshot, *api.StacksNetworkParticipant) {
	t.Helper()
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "root"}}
	root.Spec.Participants = []api.Participant{{Name: "node", Kind: "StacksNode"}}
	root.Status.Identities = []api.InstanceIdentity{{Name: "node", UID: "node-uid"}}
	owner := []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetwork", Name: root.Name, UID: root.UID, Controller: ptr.To(true)}}
	genesis := &api.StacksGenesis{ObjectMeta: metav1.ObjectMeta{Name: "genesis", Namespace: root.Namespace, UID: "genesis-uid", OwnerReferences: owner}}
	genesis.Spec.Chain.Epochs = []api.Epoch{{Name: "3.0", StartHeight: 231}}
	root.Status.GenesisRef = &common.Binding{Name: genesis.Name, UID: genesis.UID}
	root.Status.GenesisDigest = foundation.Digest(genesis.Spec.Chain)
	address := "ST000000000000000000002AMW42H"
	account := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "sender", Namespace: root.Namespace, UID: "account", Generation: 1}, Status: common.ResolutionStatus{ObservedGeneration: 1, Digest: "fingerprint", Identity: &common.PublicIdentity{Address: address}, Conditions: []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}}}}
	target := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: root.Namespace, UID: "node-uid", Generation: 1, OwnerReferences: owner}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: root.UID, ParticipantName: "node", Kind: "StacksNode"}, Status: api.ParticipantStatus{Admission: &api.Admission{PolicyDigest: "target-policy"}, Runtime: &api.ParticipantRuntimeStatus{ObservedGeneration: 1, PolicyDigest: "target-policy", PodRef: &common.Binding{UID: "pod"}, ContainerID: "process", PodIP: "10.0.0.10", ConfigurationDigest: "config", Endpoints: []api.RuntimeEndpoint{{Name: "rpc", Host: "node.test.svc", Port: 20443}}}, Conditions: []metav1.Condition{{Type: "ConfigVerified", Status: metav1.ConditionTrue, ObservedGeneration: 1}, {Type: "WorkloadReady", Status: metav1.ConditionTrue, ObservedGeneration: 1}}}}
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "traffic", Namespace: root.Namespace}, Status: api.ParticipantStatus{Admission: &api.Admission{Dependencies: []common.Binding{{Kind: "StacksAccount", Name: account.Name, UID: account.UID, Fingerprint: account.Status.Digest}, {Kind: "StacksNetworkParticipant", Name: target.Name, UID: target.UID}}, Configuration: api.Configuration{StacksTransactionProduction: &stacks.StacksTransactionProductionSpec{AccountRef: &common.NameRef{Name: account.Name}, TargetNodeRef: &common.NameRef{Name: "node"}, Recipient: &stacks.Recipient{Address: &address}, AmountMicroSTX: ptr.To(common.Amount("1")), FeeMicroSTX: ptr.To(common.Amount("3000")), Interval: ptr.To(common.Duration("5s"))}}}}}
	scheme := runtime.NewScheme()
	_ = api.AddToScheme(scheme)
	_ = stacks.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(genesis, account, target).WithStatusSubresource(target).Build()
	return PublicInputs{Reader: c, Sender: address}, stacksworker.Snapshot{Network: root, Participant: p}, target
}

func TestPublicInputsRequireCurrentPinnedIdentities(t *testing.T) {
	for _, mode := range []string{"valid", "removed", "uid", "unverified", "stale-runtime", "sender", "genesis"} {
		t.Run(mode, func(t *testing.T) {
			inputs, snapshot, target := publicFixture(t)
			switch mode {
			case "removed":
				snapshot.Network.Spec.Participants = nil
			case "uid":
				snapshot.Network.Status.Identities[0].UID = "replacement"
			case "unverified":
				target.Status.Conditions[0].Status = metav1.ConditionFalse
			case "stale-runtime":
				target.Status.Runtime.ObservedGeneration = 0
			case "sender":
				inputs.Sender = "other"
			case "genesis":
				snapshot.Network.Status.GenesisDigest = "changed"
			}
			if mode == "unverified" || mode == "stale-runtime" {
				if err := inputs.Reader.(client.Client).Status().Update(context.Background(), target); err != nil {
					t.Fatal(err)
				}
			}
			value, err := inputs.Transfer(context.Background(), snapshot)
			if mode == "valid" {
				if err != nil || value.StartHeight != 231 || value.Amount != 1 || value.Fee != 3000 {
					t.Fatalf("%+v %v", value, err)
				}
			} else if err == nil {
				t.Fatal("invalid identity admitted")
			}
		})
	}
}
