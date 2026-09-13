package foundation

import (
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestUnverifiedActorCannotSatisfyManagedTarget(t *testing.T) {
	target := &candidate{instance: &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "node", UID: "node-uid"}, Spec: api.StacksNetworkParticipantSpec{Kind: "StacksNode"}}, configuration: api.Configuration{StacksNode: &stacks.StacksNodeSpec{ActorFields: common.ActorFields{Config: &common.Config{Compatibility: ptr.To("Unverified")}}}}}
	all := map[string]*candidate{"node": target}
	caller := &candidate{}
	if err := caller.participant(all, &common.NameRef{Name: "node"}, "StacksNode"); err == nil {
		t.Fatal("unverified actor accepted as managed mutation target")
	}
	caller.configuration = target.configuration
	if err := caller.participant(all, &common.NameRef{Name: "node"}, "StacksNode"); err != nil {
		t.Fatal(err)
	}
	if len(caller.dependencies) != 1 || caller.dependencies[0].UID != "node-uid" {
		t.Fatal("unverified caller lost exact target identity")
	}
}

func TestPublicCustomizationRejectsProtectedPathsBeforeRendering(t *testing.T) {
	if err := validateCustomization("StacksNode", &common.Config{Overrides: &runtime.RawExtension{Raw: []byte(`{"node":{"seed":"private"}}`)}}); err == nil {
		t.Fatal("protected override accepted")
	}
	if err := validateCustomization("StacksNode", &common.Config{Overrides: &runtime.RawExtension{Raw: []byte(`{"node":{"mine_microblocks":false}}`)}}); err != nil {
		t.Fatal(err)
	}
	if err := validateCustomization("BitcoinNode", &common.Config{Overrides: &runtime.RawExtension{Raw: []byte(`{"dbcache":256}`)}}); err != nil {
		t.Fatal(err)
	}
	if err := validateCustomization("BitcoinNode", &common.Config{Overrides: &runtime.RawExtension{Raw: []byte(`{"regtest":{"norpcwhitelistdefault":true}}`)}}); err == nil {
		t.Fatal("protected Bitcoin alias reached renderer")
	}
}
