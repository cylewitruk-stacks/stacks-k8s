package recorder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const sourceCollectors = "collectors"

// collectorHealth records session changes and monotonic failure counters, without inferring exact lost records.
func (r *Recorder) collectorHealth(ctx context.Context) {
	pods := &corev1.PodList{}
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := r.Client.List(readCtx, pods, client.InNamespace(r.Telemetry.Namespace), client.MatchingLabels{
		observation.LabelTelemetryUID: string(r.Telemetry.UID), "app": "collector",
	})
	cancel()
	if err != nil {
		r.gap(ctx, sourceCollectors, "Collector discovery unavailable")
		r.available(sourceCollectors, false)
		return
	}
	actors := &corev1.PodList{}
	readCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
	err = r.Client.List(
		readCtx,
		actors,
		client.InNamespace(r.Telemetry.Namespace),
		client.MatchingLabels{api.LabelNetworkUID: string(r.Telemetry.Spec.NetworkUID)},
	)
	cancel()
	missing := missingCollectorNodes(actors.Items, pods.Items, r.Telemetry.Spec.Sources)
	healthy := len(pods.Items) > 0 && err == nil && len(missing) == 0
	if !healthy {
		r.mu.Lock()
		previous, known := r.states[sourceCollectors]
		r.mu.Unlock()
		if !known || previous.Available {
			r.gap(ctx, sourceCollectors, "Collector coverage unavailable on one or more actor nodes")
		}
	}
	seen := map[string]float64{}
	httpClient := &http.Client{
		Timeout:       3 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	if r.CollectorHTTPClient != nil {
		httpClient = r.CollectorHTTPClient
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning || pod.Status.PodIP == "" || len(pod.Status.ContainerStatuses) != 1 ||
			pod.Status.ContainerStatuses[0].State.Running == nil {
			healthy = false
			continue
		}
		session := string(pod.UID) + "/" + pod.Status.ContainerStatuses[0].ContainerID
		value, err := collectorFailures(ctx, httpClient, pod.Status.PodIP)
		if err != nil {
			healthy = false
			continue
		}
		seen[session] = value
		previous, known := r.collectorSessions[session]
		if !known || value != previous {
			r.gap(
				ctx,
				sourceCollectors,
				"Collector session or failure counters changed; loss is possible, not quantified",
			)
		}
		data, _ := json.Marshal(map[string]any{
			"podUID": pod.UID, "containerID": pod.Status.ContainerStatuses[0].ContainerID,
			"failureCounterSum": value,
		})
		r.emit(ctx, Record{
			Time: time.Now(), NetworkUID: string(r.Telemetry.Spec.NetworkUID), PodUID: string(pod.UID),
			Source: sourceCollectors, EventType: EventHeartbeat, Body: string(data),
		})
	}
	for session := range r.collectorSessions {
		if _, ok := seen[session]; !ok {
			healthy = false
			r.gap(ctx, sourceCollectors, "Collector session disappeared or is unreadable")
		}
	}
	r.collectorSessions = seen
	r.available(sourceCollectors, healthy)
}

// collectorFailures reads only bounded exporter/receiver failure counters from an observed collector Pod.
func collectorFailures(ctx context.Context, c *http.Client, ip string) (float64, error) {
	if net.ParseIP(ip) == nil {
		return 0, fmt.Errorf("invalid collector Pod IP")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(ip, "8888")+"/metrics", nil)
	if err != nil {
		return 0, err
	}
	response, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("collector metrics unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil || len(data) > 4*1024*1024 {
		return 0, fmt.Errorf("collector metrics exceed capture bound")
	}
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("collector metrics malformed")
	}
	if families["otelcol_process_uptime"] == nil && families["otelcol_process_uptime_total"] == nil {
		return 0, fmt.Errorf("collector metrics missing process identity")
	}
	total := float64(0)
	for name, family := range families {
		if strings.HasPrefix(name, "otelcol_") &&
			(strings.Contains(name, "failed") || strings.Contains(name, "refused")) {
			for _, metric := range family.Metric {
				total += metric.GetCounter().GetValue()
			}
		}
	}
	return total, nil
}

// missingCollectorNodes compares scheduled active sources with ready node agents, including tainted nodes.
func missingCollectorNodes(actors, collectors []corev1.Pod, sources observation.Sources) map[string]bool {
	covered := map[string]bool{}
	for _, pod := range collectors {
		if pod.DeletionTimestamp.IsZero() && pod.Status.Phase == corev1.PodRunning {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					covered[pod.Spec.NodeName] = true
				}
			}
		}
	}
	missing := map[string]bool{}
	for _, pod := range actors {
		if pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		selected := sources.Logs
		if sources.Metrics {
			for _, container := range pod.Spec.Containers {
				for _, port := range container.Ports {
					selected = selected || port.Name == "metrics"
				}
			}
		}
		if selected && !covered[pod.Spec.NodeName] {
			missing[pod.Spec.NodeName] = true
		}
	}
	return missing
}
