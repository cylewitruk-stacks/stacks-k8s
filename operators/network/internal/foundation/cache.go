package foundation

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	managedByLabel    = "app.kubernetes.io/managed-by"
	foundationManager = "stacks-network-foundation"
)

// CacheOptions excludes unrelated Jobs and ConfigMaps without filtering user declarations.
// Secret watches remain metadata-only; labels select notifications, never authorize ownership.
func CacheOptions() cache.Options {
	selector := labels.SelectorFromSet(labels.Set{managedByLabel: foundationManager})
	return cache.Options{ByObject: map[client.Object]cache.ByObject{
		&batchv1.Job{}:      {Label: selector},
		&corev1.ConfigMap{}: {Label: selector},
	}}
}
