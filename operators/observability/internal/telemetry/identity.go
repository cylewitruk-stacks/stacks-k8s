// Package telemetry provisions identity-bound passive recording workloads.
package telemetry

import (
	"crypto/sha256"
	"encoding/hex"

	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
)

const (
	// MetricsAPIGroup identifies the Kubernetes resource-metrics API.
	MetricsAPIGroup = "metrics.k8s.io"
	// ChaosAPIGroup identifies the optional Chaos Mesh observation source.
	ChaosAPIGroup = "chaos-mesh.org"
	// ChaosAPIVersion identifies the qualified Chaos Mesh resource version.
	ChaosAPIVersion = "v1alpha1"
	// ControllerFieldManager owns only admission, table identity and workload conditions.
	ControllerFieldManager = "stacks-telemetry-controller"
	// RecorderFieldManager owns only status.recording.
	RecorderFieldManager = "stacks-telemetry-recorder"
	// DefaultCollectorImage is the independently qualified upstream collector version.
	DefaultCollectorImage = "otel/opentelemetry-collector-contrib:0.160.0"
	// ConfigKey selects the generated collector configuration.
	ConfigKey = "collector.yaml"
	// EndpointKey selects the administrator-provided HTTP base endpoint.
	EndpointKey = "endpoint"
	// AuthorizationKey selects the write-only HTTP authorization header.
	AuthorizationKey = "authorization"
	// ExtractKeys defines indexed identity columns shared by both record producers.
	ExtractKeys = "network_uid,participant_uid,pod_uid,object_uid,event_type,source"
)

// ChaosResources returns the bounded native-fault resource allowlist observed by recorders.
func ChaosResources() []string {
	return []string{"networkchaos", "podchaos", "stresschaos"}
}

// Name binds generated workload names to the telemetry incarnation.
func Name(t *observation.NetworkTelemetry) string {
	sum := sha256.Sum256([]byte(t.UID))
	return "telemetry-" + hex.EncodeToString(sum[:12])
}

// TablePrefix binds independently retained backend tables to a recording UID.
func TablePrefix(t *observation.NetworkTelemetry) string {
	sum := sha256.Sum256([]byte(t.UID))
	return "stacks_" + hex.EncodeToString(sum[:16])
}
