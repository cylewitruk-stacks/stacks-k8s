// Package v1alpha1 contains API Schema definitions for trusted observations.
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// GroupVersion identifies the observation API.
var GroupVersion = schema.GroupVersion{Group: "observation.stacks.org", Version: "v1alpha1"}

// SchemeBuilder registers observation resources.
var SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

// AddToScheme installs observation resources into a scheme.
var AddToScheme = SchemeBuilder.AddToScheme
