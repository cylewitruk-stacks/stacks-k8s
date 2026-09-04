package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion identifies the served topology API version.
	GroupVersion = schema.GroupVersion{Group: "network.stacks.org", Version: "v1alpha1"}
	// SchemeBuilder registers topology objects with a runtime scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	// AddToScheme adds every topology kind to a runtime scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(
		GroupVersion,
		&BitcoinNode{},
		&BitcoinNodeList{},
		&StacksNetwork{},
		&StacksNetworkList{},
		&StacksNode{},
		&StacksNodeList{},
		&StacksSigner{},
		&StacksSignerList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
