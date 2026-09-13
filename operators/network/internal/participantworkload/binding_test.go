package participantworkload

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// binding constructs independent literal identities for test fixtures.
func binding(kind string, obj metav1.Object) *common.Binding {
	return &common.Binding{Kind: kind, Name: obj.GetName(), UID: obj.GetUID()}
}
