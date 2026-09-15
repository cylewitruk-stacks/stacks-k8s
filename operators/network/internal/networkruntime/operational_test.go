package networkruntime

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// operationalFixture provides current bound observations independently of genesis gate execution.
func operationalFixture(
	t *testing.T,
	now time.Time,
) (*api.StacksNetwork, *api.StacksGenesis, []api.StacksNetworkParticipant, *bitcoin.BitcoinExecution) {
	t.Helper()
	root, g := gateFixture()
	root.Spec.Operation = "Running"
	root.Spec.Participants = nil
	root.Status.Identities = nil
	participants := []api.StacksNetworkParticipant{}
	for _, item := range []struct {
		name string
		kind api.ParticipantKind
	}{
		{"miner", "StacksNode"},
		{"blocks", "BitcoinBlockProduction"},
		{"traffic", "StacksTransactionProduction"},
		{"contracts", "StacksContractSet"},
	} {
		p := participantFixture(root)
		p.Name = item.name
		p.UID = types.UID(item.name)
		p.Spec.ParticipantName = item.name
		p.Spec.Kind = item.kind
		p.Status.Admission = &api.Admission{PolicyDigest: "policy-" + item.name}
		p.Status.Conditions = []metav1.Condition{
			{Type: "WorkloadReady", Status: metav1.ConditionTrue, ObservedGeneration: p.Generation},
			{Type: "ConfigVerified", Status: metav1.ConditionTrue, ObservedGeneration: p.Generation},
		}
		p.Status.Execution = &api.WorkerExecutionStatus{
			PodUID:              types.UID("pod-" + item.name),
			ProcessNonce:        "process",
			ProfileDigest:       "profile",
			AppliedPolicyDigest: p.Status.Admission.PolicyDigest,
			ObservedGeneration:  p.Generation,
			NetworkGeneration:   root.Generation,
			ObservedAt:          metav1.NewTime(now),
			Phase:               "Active",
		}
		root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: item.name, Kind: item.kind})
		root.Status.Identities = append(
			root.Status.Identities,
			api.InstanceIdentity{
				Name: item.name,
				UID:  p.UID,
				Worker: &api.WorkerSession{
					Pod: api.WorkerPodBinding{
						Kind: "Pod",
						Name: "pod-" + item.name,
						UID:  p.Status.Execution.PodUID,
					},
					ProfileDigest: "profile",
				},
			},
		)
		participants = append(participants, *p)
	}
	miner := &participants[0]
	miner.Status.Admission.Configuration.StacksNode = &stacks.StacksNodeSpec{
		Mining: &stacks.Mining{Enabled: ptr.To(true)},
	}
	miner.Status.Runtime = &api.ParticipantRuntimeStatus{
		ObservedGeneration:  miner.Generation,
		PolicyDigest:        miner.Status.Admission.PolicyDigest,
		PodRef:              &common.Binding{UID: "pod-miner"},
		ContainerID:         "container",
		ConfigurationDigest: "config",
		Protocol: &api.StacksProtocolObservation{
			Available:           true,
			FullySynced:         true,
			NetworkID:           0x80000000,
			GenesisUID:          g.UID,
			PodUID:              "pod-miner",
			ContainerID:         "container",
			ConfigurationDigest: "config",
			ObservedAt:          metav1.NewTime(now),
			BurnHeight:          301,
			PoXContract:         "ST000000000000000000002AMW42H.pox-5",
		},
	}
	blocks := &participants[1]
	blocks.Status.Scheduling = &bitcoin.BitcoinSchedulingStatus{
		PolicyDigest: blocks.Status.Admission.PolicyDigest,
		Schedule: &bitcoin.BitcoinBlockScheduleSpec{
			Cadence: bitcoin.Cadence{Mode: "Fixed", Interval: ptr.To(common.Duration("5s"))},
		},
		ObservedAt:            ptr.To(metav1.NewTime(now)),
		LastAcknowledgedAt:    ptr.To(metav1.NewTime(now.Add(-10 * time.Second))),
		EligibleTargets:       1,
		ProgressWindowSeconds: 130,
	}
	traffic := &participants[2]
	traffic.Status.Admission.Configuration.StacksTransactionProduction = &stacks.StacksTransactionProductionSpec{}
	traffic.Status.Execution.Transactions = &api.TransactionExecutionStatus{
		Included: 1,
		LastInclusion: &api.TransactionInclusion{
			TxID:       strings.Repeat("a", 64),
			BlockID:    strings.Repeat("b", 64),
			Success:    true,
			ObservedAt: metav1.NewTime(now.Add(-10 * time.Second)),
		},
	}
	traffic.Status.Execution.Traffic = &api.TrafficObservation{
		Available:                 true,
		Found:                     true,
		Success:                   true,
		TxID:                      strings.Repeat("a", 64),
		BlockID:                   strings.Repeat("b", 64),
		ObservedAt:                metav1.NewTime(now),
		GenesisUID:                g.UID,
		TargetParticipantUID:      miner.UID,
		TargetPodUID:              "pod-miner",
		TargetContainerID:         "container",
		TargetConfigurationDigest: "config",
		EffectiveIntervalSeconds:  10,
		ProgressWindowSeconds:     130,
		BurnHeight:                301,
		SubmittedBurnHeight:       300,
	}
	contracts := &participants[3]
	registry := &stacks.RegistryInitialization{
		Mode:                   "ExplicitTestRegistry",
		SignerAccountRefs:      []common.NameRef{{Name: "a"}, {Name: "b"}},
		AggregateKeyAccountRef: common.NameRef{Name: "aggregate"},
		Threshold:              2,
	}
	contracts.Status.Admission.Configuration.StacksContractSet = &stacks.StacksContractSetSpec{Initialization: registry}
	keys := []string{"02" + strings.Repeat("1", 64), "03" + strings.Repeat("2", 64)}
	g.Spec.Chain.Contracts.Deployer = "ST000000000000000000002AMW42H"
	g.Spec.Chain.Contracts.Bundle = "sbtc-regtest-v1"
	g.Spec.Chain.Contracts.SourceHashes = map[string]string{"source": "hash"}
	principal, err := identity.EncodeAddress(21, [20]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	contracts.Status.Execution.Contracts = &api.ContractSetObservation{
		Complete:           true,
		ObservedAt:         metav1.NewTime(now),
		Deployer:           g.Spec.Chain.Contracts.Deployer,
		Bundle:             g.Spec.Chain.Contracts.Bundle,
		SourceDigest:       foundation.Digest(g.Spec.Chain.Contracts.SourceHashes),
		SignerPublicKeys:   keys,
		AggregatePublicKey: keys[0],
		Threshold:          2,
		SignerPrincipal:    principal,
	}
	g.Spec.Bootstrap.Requirements = []api.BootstrapRequirement{
		{
			Kind:          "StacksNode",
			MiningEnabled: ptr.To(true),
			Participant:   common.Binding{Name: miner.Name, UID: miner.UID},
			PolicyDigest:  miner.Status.Admission.PolicyDigest,
		},
		{
			Kind:                   "StacksContractSet",
			Participant:            common.Binding{Name: contracts.Name, UID: contracts.UID},
			PolicyDigest:           contracts.Status.Admission.PolicyDigest,
			RegistryInitialization: registry,
			Accounts: []api.PublicAccount{
				{Binding: common.Binding{Name: "a"}, Identity: common.PublicIdentity{PublicKey: keys[0]}},
				{Binding: common.Binding{Name: "b"}, Identity: common.PublicIdentity{PublicKey: keys[1]}},
				{Binding: common.Binding{Name: "aggregate"}, Identity: common.PublicIdentity{PublicKey: keys[0]}},
			},
		},
	}
	g.Spec.Bootstrap.Gates = []api.Gate{{Name: "PrepareWaterfall", BitcoinCeiling: 299}}
	root.Status.GenesisRef.Fingerprint = foundation.Digest(g.Spec)
	root.Status.GenesisDigest = foundation.Digest(g.Spec.Chain)
	root.Status.Initialization = &api.InitializationStatus{Completed: true}
	btc := participantFixture(root)
	btc.Name = "btc"
	btc.UID = "btc"
	btc.Spec.ParticipantName = "btc"
	btc.Spec.Kind = api.ParticipantBitcoinNode
	btc.Status.Admission = &api.Admission{PolicyDigest: "policy-btc"}
	btc.Status.Conditions = []metav1.Condition{
		{Type: api.ConditionWorkloadReady, Status: metav1.ConditionTrue, ObservedGeneration: btc.Generation},
		{Type: api.ConditionConfigVerified, Status: metav1.ConditionTrue, ObservedGeneration: btc.Generation},
	}
	btc.Status.Runtime = &api.ParticipantRuntimeStatus{
		ObservedGeneration: btc.Generation,
		PolicyDigest:       btc.Status.Admission.PolicyDigest,
		PodRef:             &common.Binding{Kind: common.KindPod, Name: "pod-btc", UID: "pod-btc"},
		ContainerID:        "container-btc",
		ConfigRef:          &common.Binding{Kind: common.KindSecret, Name: "config-btc", UID: "config-btc"},
		RPCSecretRef:       &common.Binding{Kind: common.KindSecret, Name: "rpc-btc", UID: "rpc-btc"},
		PodIP:              "10.0.0.10",
		Endpoints:          []api.RuntimeEndpoint{{Name: common.EndpointRPC, Port: 18443}},
	}
	root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: btc.Name, Kind: btc.Spec.Kind})
	root.Status.Identities = append(root.Status.Identities, api.InstanceIdentity{Name: btc.Name, UID: btc.UID})
	participants = append(participants, *btc)
	record := &bitcoin.BitcoinExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "execution-btc",
			Namespace: root.Namespace,
			UID:       "execution-btc",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: api.GroupVersion.String(),
				Kind:       api.KindStacksNetwork,
				Name:       root.Name,
				UID:        root.UID,
				Controller: ptr.To(true),
			}},
		},
		Spec: bitcoin.BitcoinExecutionSpec{
			NetworkUID:  root.UID,
			Participant: common.Binding{Kind: api.KindStacksNetworkParticipant, Name: btc.Name, UID: btc.UID},
		},
		Status: bitcoin.BitcoinExecutionStatus{Observation: &bitcoin.BitcoinObservation{
			Height:     301,
			Tip:        strings.Repeat("c", 64),
			ObservedAt: metav1.NewTime(now),
			Target: bitcoin.BitcoinTargetIdentity{
				Participant:   common.Binding{Kind: api.KindStacksNetworkParticipant, Name: btc.Name, UID: btc.UID},
				Pod:           *btc.Status.Runtime.PodRef,
				ContainerID:   btc.Status.Runtime.ContainerID,
				Endpoint:      "http://10.0.0.10:18443",
				Configuration: *btc.Status.Runtime.ConfigRef,
				Credentials:   *btc.Status.Runtime.RPCSecretRef,
				PolicyDigest:  btc.Status.Admission.PolicyDigest,
			},
		}},
	}
	root.Status.Bitcoin = &api.BitcoinRuntimeStatus{
		ExecutionRefs: []common.Binding{{Kind: bitcoin.KindBitcoinExecution, Name: record.Name, UID: record.UID}},
	}
	return root, g, participants, record
}

func TestOperationalSeparatesKnownFailuresFreshnessAndOriginalProgress(t *testing.T) {
	for _, mode := range []string{
		"valid",
		"traffic-paused",
		"bitcoin-paused",
		"stale",
		"old-transfer",
		"old-bitcoin",
		"reorged",
		"ingress-roll",
		"miner-disabled",
		"contract-mismatch",
		"failed-extra",
		"slow-cadence",
		"unbound-miner",
		"burnchain-lag",
		"burnchain-boundary",
		"burnchain-stale",
		"burnchain-identity",
	} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			root, g, ps, record := operationalFixture(t, now)
			want := metav1.ConditionTrue
			switch mode {
			case "traffic-paused":
				root.Spec.Participants[2].Control = &api.Control{Paused: ptr.To(true)}
				want = metav1.ConditionFalse
			case "bitcoin-paused":
				root.Spec.Participants[1].Control = &api.Control{Paused: ptr.To(true)}
				ps[2].Status.Execution = nil
				want = metav1.ConditionFalse
			case "stale":
				ps[2].Status.Execution.Traffic.ObservedAt = metav1.NewTime(now.Add(-17 * time.Second))
				want = metav1.ConditionUnknown
			case "old-transfer":
				ps[2].Status.Execution.Transactions.LastInclusion.ObservedAt = metav1.NewTime(
					now.Add(-131 * time.Second),
				)
				want = metav1.ConditionFalse
			case "old-bitcoin":
				ps[1].Status.Scheduling.LastAcknowledgedAt = ptr.To(metav1.NewTime(now.Add(-131 * time.Second)))
				want = metav1.ConditionFalse
			case "reorged":
				ps[2].Status.Execution.Traffic.Found = false
				want = metav1.ConditionFalse
			case "ingress-roll":
				ps[0].Status.Runtime.ContainerID = "replacement"
				want = metav1.ConditionUnknown
			case "miner-disabled":
				ps[0].Status.Admission.Configuration.StacksNode.Mining.Enabled = ptr.To(false)
				want = metav1.ConditionFalse
			case "contract-mismatch":
				ps[3].Status.Execution.Contracts.Threshold = 1
				want = metav1.ConditionFalse
			case "failed-extra":
				extra := participantFixture(root)
				extra.Spec.Kind = "StacksSigner"
				ps = append(ps, *extra)
			case "slow-cadence":
				ps[2].Status.Execution.Traffic.EffectiveIntervalSeconds = 3600
				ps[2].Status.Execution.Traffic.ProgressWindowSeconds = 10810
				ps[2].Status.Execution.Transactions.LastInclusion.ObservedAt = metav1.NewTime(now.Add(-time.Hour))
			case "unbound-miner":
				root.Status.Identities[0].UID = "replacement"
				want = metav1.ConditionFalse
			case "burnchain-lag":
				record.Status.Observation.Height = 328
			case "burnchain-boundary":
				record.Status.Observation.Height = 327
			case "burnchain-stale":
				record.Status.Observation.ObservedAt = metav1.NewTime(now.Add(-17 * time.Second))
			case "burnchain-identity":
				record.Status.Observation.Target.ContainerID = "replacement"
			}
			got := combinePredicates(
				bitcoinOperational(root, ps, now),
				minerOperational(root, ps),
				trafficOperational(root, g, ps, now),
				contractsOperational(root, g, ps, now),
			)
			if got.status != want {
				t.Fatalf("got %+v want %s", got, want)
			}
		})
	}
}

func TestInitializationNeedsNewPostWaterfallTransferAndDoesNotRewind(t *testing.T) {
	now := time.Now()
	root, g, ps, execution := operationalFixture(t, now)
	record := &bitcoin.BitcoinInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "initialization",
			Namespace:       root.Namespace,
			UID:             "initialization",
			OwnerReferences: g.OwnerReferences,
		},
		Spec: bitcoin.BitcoinInitializationSpec{NetworkUID: root.UID},
	}
	root.Status.Bitcoin = &api.BitcoinRuntimeStatus{
		ExecutionRefs: []common.Binding{{
			Kind: bitcoin.KindBitcoinExecution, Name: execution.Name, UID: execution.UID,
		}},
		InitializationRef: &common.Binding{Kind: "BitcoinInitialization", Name: record.Name, UID: record.UID},
	}
	r := fixtureReconciler(t, g, record, execution)
	ps[2].Status.Execution.Traffic.SubmittedBurnHeight = 299
	if err := r.projectOperation(context.Background(), root, ps, now); err != nil {
		t.Fatal(err)
	}
	if meta.IsStatusConditionTrue(root.Status.Conditions, "Initialized") {
		t.Fatal("old transfer released initialization")
	}
	ps[2].Status.Execution.Traffic.SubmittedBurnHeight = 300
	if err := r.projectOperation(context.Background(), root, ps, now); err != nil {
		t.Fatal(err)
	}
	if !meta.IsStatusConditionTrue(root.Status.Conditions, "Initialized") ||
		!meta.IsStatusConditionTrue(root.Status.Conditions, "Running") ||
		!meta.IsStatusConditionTrue(root.Status.Conditions, "Operational") {
		t.Fatalf("valid post-waterfall facts not accepted: %+v", root.Status.Conditions)
	}
	ps[2].Status.Execution.Traffic.Found = false
	if err := r.projectOperation(context.Background(), root, ps, now); err != nil {
		t.Fatal(err)
	}
	if !meta.IsStatusConditionTrue(root.Status.Conditions, "Initialized") ||
		!meta.IsStatusConditionTrue(root.Status.Conditions, "Running") ||
		!meta.IsStatusConditionFalse(root.Status.Conditions, "Operational") {
		t.Fatal("later canonical divergence rewound initialization or running state")
	}
}

// TestTrafficIntervalOverflowCannotLookOperational rejects values before duration conversion.
func TestTrafficIntervalOverflowCannotLookOperational(t *testing.T) {
	for _, seconds := range []uint64{0, 3601, 1<<55 + 10, ^uint64(0)} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			now := time.Now()
			root, genesis, participants, _ := operationalFixture(t, now)
			participants[2].Status.Execution.Traffic.EffectiveIntervalSeconds = seconds
			// Adding 2^55 seconds wraps to the fixture's valid 10-second duration after multiplication.
			got := trafficOperational(root, genesis, participants, now)
			if got.status != metav1.ConditionUnknown || got.reason != reasonTrafficTimingUnavailable {
				t.Fatalf("out-of-range interval accepted: %+v", got)
			}
		})
	}
}
