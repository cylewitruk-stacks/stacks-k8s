package recorder

import (
	actions "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/telemetry"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// EventConfiguration retains admitted recording policy without backend credentials.
	EventConfiguration = "Configuration"
	// EventSnapshot labels current-state lists; snapshots are not a complete event history.
	EventSnapshot = "Snapshot"
	// EventGap marks an interval without provable source continuity.
	EventGap = "CaptureGap"
	// EventHeartbeat exposes recorder liveness in retained evidence.
	EventHeartbeat = "Heartbeat"
	// EventContainerResource records a Metrics API sample for an exact actor container.
	EventContainerResource = "ContainerResource"
	// SourceRecorder names recorder health evidence.
	SourceRecorder = "recorder"
	// SourceContainerResource names namespace-scoped Metrics API observations.
	SourceContainerResource = "container-resource"
)

// sources is a fixed allowlist. Missing optional APIs affect coverage, not startup.
var sources = func() []schema.GroupVersionResource {
	result := []schema.GroupVersionResource{
		api.GroupVersion.WithResource(api.ResourceStacksNetwork),
		api.GroupVersion.WithResource(api.ResourceStacksNetworkParticipant),
		api.GroupVersion.WithResource(api.ResourceStacksGenesis),
		bitcoin.GroupVersion.WithResource(bitcoin.ResourceBitcoinExecution),
		actions.GroupVersion.WithResource(actions.ResourceBitcoinBlockGeneration),
		actions.GroupVersion.WithResource(actions.ResourceBitcoinReorganization),
	}
	for _, resource := range telemetry.ChaosResources() {
		result = append(result, schema.GroupVersionResource{
			Group: telemetry.ChaosAPIGroup, Version: telemetry.ChaosAPIVersion, Resource: resource,
		})
	}
	return append(result,
		schema.GroupVersionResource{Version: "v1", Resource: "pods"},
		schema.GroupVersionResource{Version: "v1", Resource: "events"},
	)
}()

// sourceID is the durable resource.group/version evidence encoding; core denotes the empty API group.
func sourceID(source schema.GroupVersionResource) string {
	group := source.Group
	if group == "" {
		group = "core"
	}
	return source.Resource + "." + group + "/" + source.Version
}
