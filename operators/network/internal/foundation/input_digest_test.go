package foundation

import (
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func TestInputDigestExcludesAllocationIdentity(t *testing.T) {
	fixture := func(uid string) (api.StacksGenesisSpec, map[string]*candidate) {
		p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: ParticipantName(uid, "follower"), UID: types.UID(uid + "-participant")}, Spec: api.StacksNetworkParticipantSpec{ParticipantName: "follower", Kind: "StacksNode", NetworkUID: types.UID(uid)}}
		a := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountName(p.Name, "identity"), UID: types.UID(uid + "-account"), OwnerReferences: []metav1.OwnerReference{{UID: p.UID, Controller: ptr.To(true)}}}}
		c := &candidate{instance: p, source: api.Source{Generation: 8, UID: types.UID(uid + "-source")}, accounts: map[string]*stacks.StacksAccount{a.Name: a}, configuration: api.Configuration{StacksNode: &stacks.StacksNodeSpec{ActorFields: common.ActorFields{Image: ptr.To("node:v1")}, IdentityAccountRef: &common.NameRef{Name: a.Name}}}, dependencies: []common.Binding{binding("StacksAccount", a, "public-fingerprint")}}
		return api.StacksGenesisSpec{Chain: api.Chain{Profile: "profile"}, Source: api.GenesisSource{NetworkUID: types.UID(uid), Dependencies: []common.Binding{{Kind: "StacksEpochSchedule", Name: "epochs", UID: types.UID(uid + "-epochs"), Fingerprint: "epoch-fingerprint"}}}}, map[string]*candidate{"follower": c}
	}
	first, a := fixture("first-network")
	second, b := fixture("second-network")
	want := semanticInputDigest(first, a)
	if got := semanticInputDigest(second, b); got != want {
		t.Fatalf("allocation identity changed digest: %s != %s", got, want)
	}
	if a["follower"].configuration.StacksNode.IdentityAccountRef.Name != DefaultAccountName(a["follower"].instance.Name, "identity") {
		t.Fatal("digest mutated runtime configuration")
	}
	for _, mutation := range []struct {
		name   string
		change func(*api.StacksGenesisSpec, map[string]*candidate)
	}{
		{"image", func(_ *api.StacksGenesisSpec, c map[string]*candidate) {
			c["follower"].configuration.StacksNode.Image = ptr.To("node:v2")
		}},
		{"public key", func(_ *api.StacksGenesisSpec, c map[string]*candidate) {
			c["follower"].dependencies[0].Fingerprint = "different-key"
		}},
		{"chain", func(s *api.StacksGenesisSpec, _ map[string]*candidate) { s.Chain.Profile = "other" }},
		{"schedule", func(s *api.StacksGenesisSpec, _ map[string]*candidate) {
			s.Source.Dependencies[0].Fingerprint = "other"
		}},
		{"logical name", func(_ *api.StacksGenesisSpec, c map[string]*candidate) {
			c["renamed"] = c["follower"]
			delete(c, "follower")
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			spec, c := fixture("first-network")
			mutation.change(&spec, c)
			if semanticInputDigest(spec, c) == want {
				t.Fatal("semantic change did not change digest")
			}
		})
	}
}
