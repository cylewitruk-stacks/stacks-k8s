package main

import (
	"testing"

	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestRecorderSchemeSupportsStatusAndCollectorDiscovery(t *testing.T) {
	scheme, err := recorderScheme()
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range []runtime.Object{&observation.NetworkTelemetry{}, &corev1.PodList{}} {
		if _, _, err := scheme.ObjectKinds(object); err != nil {
			t.Fatal(err)
		}
	}
}
