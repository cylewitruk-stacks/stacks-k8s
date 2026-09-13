package foundation

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// binding constructs independent literal identities for test fixtures.
func binding(kind string, obj client.Object, digest string) common.Binding {
	return common.Binding{Kind: kind, Name: obj.GetName(), UID: obj.GetUID(), Fingerprint: digest}
}
