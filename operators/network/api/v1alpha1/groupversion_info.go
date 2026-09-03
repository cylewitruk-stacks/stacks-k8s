package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion identifies the served topology API version.
	GroupVersion = schema.GroupVersion{Group: "network.stacks.org", Version: "v1alpha1"}
	// SchemeBuilder registers topology objects with a runtime scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	// AddToScheme adds every topology kind to a runtime scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
