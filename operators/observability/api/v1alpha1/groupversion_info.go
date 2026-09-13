// Package v1alpha1 contains API Schema definitions for trusted observations.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion identifies the observation API.
var GroupVersion = schema.GroupVersion{Group: "observation.stacks.org", Version: "v1alpha1"}

// SchemeBuilder registers observation resources.
var SchemeBuilder = runtime.NewSchemeBuilder(func(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &NetworkObservation{}, &NetworkObservationList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
})

// AddToScheme installs observation resources into a scheme.
var AddToScheme = SchemeBuilder.AddToScheme
