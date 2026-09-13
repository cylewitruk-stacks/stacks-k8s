//go:build integration

package participantworkload

import (
	"context"
	"encoding/json"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyBitcoinCandidateAPI validates real resolver outputs before genesis without actor activation.
func verifyBitcoinCandidateAPI(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	configuration := api.Configuration{
		BitcoinNode: &bitcoin.BitcoinNodeSpec{
			ActorFields: common.ActorFields{
				Config: &common.Config{
					Overrides: &runtime.RawExtension{Raw: []byte(`{"dbcache":256,"regtest":{"maxconnections":32}}`)},
				},
			},
		},
	}
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test"},
		Spec: api.StacksNetworkSpec{
			Operation: "Running",
			Participants: []api.Participant{
				{Name: "core", Kind: "BitcoinNode", Definition: api.Definition{Inline: &configuration}},
			},
		},
	}
	must(c.Create(ctx, root))
	p := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      foundation.ParticipantName(string(root.UID), "core"),
			Namespace: root.Namespace,
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
		Spec: api.StacksNetworkParticipantSpec{
			NetworkUID:      root.UID,
			ParticipantName: "core",
			Kind:            "BitcoinNode",
			Configuration:   configuration,
		},
	}
	must(c.Create(ctx, p))
	root.Status.Identities = []api.InstanceIdentity{{Name: "core", UID: p.UID}}
	must(c.Status().Update(ctx, root))
	p.Status.Admission = &api.Admission{Configuration: configuration, PolicyDigest: digest(configuration)}
	in := foundation.CandidateConfiguration{
		Root:         root,
		Participants: map[string]*api.StacksNetworkParticipant{"core": p},
	}
	r := &Reconciler{Client: c, Reader: metadataOnlyReader{c}, ResolverImage: "resolver:test"}
	ready, err := r.ValidateConfiguration(ctx, in, "core")
	if err != nil || ready {
		t.Fatalf("candidate pending: ready=%v err=%v", ready, err)
	}
	var reports corev1.ConfigMapList
	must(
		c.List(
			ctx,
			&reports,
			client.InNamespace(p.Namespace),
			client.MatchingLabels{"network.stacks.org/participant-uid": string(p.UID)},
		),
	)
	if len(reports.Items) != 1 {
		t.Fatalf("reports=%d", len(reports.Items))
	}
	var input BitcoinConfigInput
	must(json.Unmarshal([]byte(reports.Items[0].Data["input.json"]), &input))
	var jobs batchv1.JobList
	must(
		c.List(
			ctx,
			&jobs,
			client.InNamespace(p.Namespace),
			client.MatchingLabels{"network.stacks.org/participant-uid": string(p.UID)},
		),
	)
	if len(jobs.Items) != 1 ||
		jobs.Items[0].Spec.Template.Spec.Containers[0].Args[1] != "--input-file=/input/input.json" {
		t.Fatal("resolver did not use bounded mounted input")
	}
	// Execute only the private Go resolver against the real API; no Core process is started.
	must(RunBitcoinConfigResolver(ctx, c, input))
	ready, err = r.ValidateConfiguration(ctx, in, "core")
	if err != nil || !ready {
		t.Fatalf("candidate agreement: ready=%v err=%v", ready, err)
	}
	var current api.StacksNetworkParticipant
	must(c.Get(ctx, client.ObjectKeyFromObject(p), &current))
	if current.Status.Admission != nil || current.Status.Runtime == nil || current.Status.Runtime.ConfigRef != nil {
		t.Fatal("candidate resolution published activation authority")
	}
	var workloads appsv1.StatefulSetList
	must(
		c.List(
			ctx,
			&workloads,
			client.InNamespace(p.Namespace),
			client.MatchingLabels{"network.stacks.org/participant-uid": string(p.UID)},
		),
	)
	if len(workloads.Items) != 0 {
		t.Fatal("candidate resolver activated an actor")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
	if root.Status.GenesisRef != nil {
		t.Fatal("candidate resolver published genesis")
	}
	var output corev1.Secret
	must(c.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: input.Config.Name}, &output))
	must(c.Delete(ctx, &output))
	output.UID = ""
	output.ResourceVersion = ""
	must(c.Create(ctx, &output))
	if ready, err = r.ValidateConfiguration(ctx, in, "core"); err == nil || ready {
		t.Fatal("replacement output reused prior validation")
	}
}
