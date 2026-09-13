package foundation

import (
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	managedByLabel    = api.LabelManagedBy
	foundationManager = api.ManagedByNetworkOperator
)

// CacheOptions excludes unrelated runtime objects without filtering user declarations.
// Secret watches remain metadata-only; labels select notifications, never authorize ownership.
func CacheOptions() cache.Options {
	selector := labels.SelectorFromSet(labels.Set{managedByLabel: foundationManager})
	return cache.Options{ByObject: map[client.Object]cache.ByObject{
		&corev1.Pod{}:         {Label: selector},
		&corev1.Service{}:     {Label: selector},
		&appsv1.StatefulSet{}: {Label: selector},
		&appsv1.Deployment{}:  {Label: selector},
		&batchv1.Job{}:        {Label: selector},
		&corev1.ConfigMap{}:   {Label: selector},
	}}
}
