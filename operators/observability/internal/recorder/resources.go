package recorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/telemetry"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const resourcePollInterval = 15 * time.Second

var (
	podResource        = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	podMetricsResource = schema.GroupVersionResource{
		Group:    telemetry.MetricsAPIGroup,
		Version:  "v1beta1",
		Resource: "pods",
	}
)

// observeContainerResources retains Metrics API CPU and memory samples for exact actor identities.
func (r *Recorder) observeContainerResources(ctx context.Context) {
	ticker := time.NewTicker(resourcePollInterval)
	defer ticker.Stop()
	for {
		err := r.collectContainerResources(ctx)
		if err != nil {
			r.sourceError(SourceContainerResource, err)
		} else {
			r.available(SourceContainerResource, true)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// collectContainerResources joins namespace-scoped PodMetrics to authoritative actor Pod identity.
func (r *Recorder) collectContainerResources(ctx context.Context) error {
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pods, err := r.Metadata.Resource(podResource).Namespace(r.Telemetry.Namespace).List(
		readCtx,
		metav1.ListOptions{
			LabelSelector: labels.Set{
				api.LabelNetworkUID: string(r.Telemetry.Spec.NetworkUID),
				api.LabelRole:       api.RoleActor,
			}.AsSelector().
				String(),
		},
	)
	if err != nil {
		return err
	}
	metrics, err := r.Dynamic.Resource(podMetricsResource).Namespace(r.Telemetry.Namespace).List(
		readCtx,
		metav1.ListOptions{},
	)
	if err != nil {
		return err
	}
	actors := map[string]*metav1.PartialObjectMetadata{}
	for index := range pods.Items {
		pod := &pods.Items[index]
		labels := pod.GetLabels()
		if labels[api.LabelNetworkUID] == string(r.Telemetry.Spec.NetworkUID) &&
			labels[api.LabelParticipantUID] != "" && labels[api.LabelRole] == api.RoleActor {
			actors[pod.GetName()] = pod
		}
	}
	var failures []error
	for index := range metrics.Items {
		metric := &metrics.Items[index]
		pod := actors[metric.GetName()]
		if pod == nil {
			continue
		}
		if err := r.recordContainerResources(ctx, pod, metric); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (r *Recorder) recordContainerResources(
	ctx context.Context,
	pod metav1.Object, metric *unstructured.Unstructured,
) error {
	timestamp, found, err := unstructured.NestedString(metric.Object, "timestamp")
	if err != nil || !found {
		return fmt.Errorf("pod metrics omitted timestamp")
	}
	if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		return fmt.Errorf("pod metrics returned invalid timestamp")
	}
	window, _, _ := unstructured.NestedString(metric.Object, "window")
	containers, found, err := unstructured.NestedSlice(metric.Object, "containers")
	if err != nil || !found {
		return fmt.Errorf("pod metrics omitted containers")
	}
	for _, value := range containers {
		container, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("pod metrics returned invalid container")
		}
		name, _ := container["name"].(string)
		usage, _ := container["usage"].(map[string]any)
		cpuText, _ := usage["cpu"].(string)
		memoryText, _ := usage["memory"].(string)
		cpu, cpuErr := resource.ParseQuantity(cpuText)
		memory, memoryErr := resource.ParseQuantity(memoryText)
		if name == "" || cpuErr != nil || memoryErr != nil || cpu.Sign() < 0 || memory.Sign() < 0 {
			return fmt.Errorf("pod metrics returned invalid usage")
		}
		key := string(pod.GetUID()) + "/" + name
		sampleID := timestamp + "/" + cpu.String() + "/" + memory.String()
		if r.resourceSamples[key] == sampleID {
			continue
		}
		body, err := json.Marshal(map[string]any{
			"container":   name,
			"cpuCores":    cpu.AsApproximateFloat64(),
			"memoryBytes": memory.Value(),
			"sampledAt":   timestamp,
			"window":      window,
		})
		if err != nil {
			return fmt.Errorf("encode container resource observation")
		}
		acknowledged := r.emit(ctx, Record{
			Time: time.Now(), NetworkUID: string(r.Telemetry.Spec.NetworkUID),
			ParticipantUID: pod.GetLabels()[api.LabelParticipantUID], PodUID: string(pod.GetUID()),
			ObjectUID: string(pod.GetUID()), Source: SourceContainerResource,
			EventType: EventContainerResource, Body: string(body),
		})
		if acknowledged {
			r.resourceSamples[key] = sampleID
		}
	}
	return nil
}
