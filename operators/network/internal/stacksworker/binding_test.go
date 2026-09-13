package stacksworker

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// objectBinding constructs independent literal identities for test fixtures.
func objectBinding(kind string, object metav1.Object) *common.Binding {
	return &common.Binding{Kind: kind, Name: object.GetName(), UID: object.GetUID()}
}
