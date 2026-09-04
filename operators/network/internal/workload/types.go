// Package workload owns the common actor workload lifecycle.
package workload

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
)

// Descriptor is the normalized desired state consumed by the workload engine.
type Descriptor struct {
	Owner            client.Object
	Kind             string
	Network          string
	Actor            string
	Role             string
	Image            string
	ImagePullPolicy  corev1.PullPolicy
	ImagePullSecrets []networkv1alpha1.LocalObjectReference
	Config           networkv1alpha1.ConfigSource
	SpecDigest       string
	ServiceMap       map[string]string
	Command          []string
	Args             []string
	Env              []corev1.EnvVar
	Ports            []networkv1alpha1.PortSpec
	Dependencies     []Dependency
	DependencyImage  string
	Workload         networkv1alpha1.WorkloadSpec
	Container        *networkv1alpha1.ContainerOverride
	Suspended        bool
}

// Dependency is a startup reachability requirement.
type Dependency struct {
	Host string
	Port int32
}
