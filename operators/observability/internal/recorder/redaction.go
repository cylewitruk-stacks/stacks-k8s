package recorder

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/telemetry"

	actions "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// MaximumRecordBytes bounds exported object bodies; oversized inputs retain only identity metadata.
const MaximumRecordBytes = 64 * 1024

// sensitiveAssignment removes recognizable credentials from free-form diagnostics.
var sensitiveAssignment = regexp.MustCompile(telemetry.SensitiveAssignmentPattern)

// sensitiveName applies the same vocabulary to structured keys and metric labels.
var sensitiveName = regexp.MustCompile(telemetry.SensitiveNamePattern)

// publicBody excludes arbitrary annotations, Pod environment/volumes and private configuration before export.
func publicBody(object *unstructured.Unstructured) string {
	value := object.DeepCopy().Object
	metadata, _, _ := unstructured.NestedMap(value, "metadata")
	delete(metadata, "managedFields")
	delete(metadata, "annotations")
	labels := map[string]any{}
	for _, key := range []string{
		api.LabelNetworkUID, api.LabelParticipantUID, api.LabelParticipant, api.LabelRole, actions.CorrelationIDLabel,
	} {
		if label := object.GetLabels()[key]; label != "" {
			labels[key] = label
		}
	}
	metadata["labels"] = labels
	value["metadata"] = metadata
	if object.GetKind() == "Pod" {
		// Requested images remain public; other Pod spec data can contain literal credentials.
		spec := map[string]any{}
		containers, _, _ := unstructured.NestedSlice(value, "spec", "containers")
		images := []any{}
		for _, item := range containers {
			if c, ok := item.(map[string]any); ok {
				images = append(images, map[string]any{"name": c["name"], "image": c["image"]})
			}
		}
		spec["containers"] = images
		value["spec"] = spec
	}
	scrub(value)
	if object.GroupVersionKind().Group == telemetry.ChaosAPIGroup {
		scrubOpaqueChaos(value)
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) > MaximumRecordBytes {
		data, _ = json.Marshal(map[string]any{
			"metadata": map[string]any{
				"name":            object.GetName(),
				"namespace":       object.GetNamespace(),
				"uid":             object.GetUID(),
				"resourceVersion": object.GetResourceVersion(),
			},
			"payloadOmitted": "RecordTooLarge",
		})
	}
	return string(data)
}

// scrubOpaqueChaos omits arbitrary fault payloads that cannot be made safe by credential-key matching.
func scrubOpaqueChaos(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			lower := strings.ToLower(key)
			compact := strings.ReplaceAll(strings.ReplaceAll(lower, "_", ""), "-", "")
			if strings.HasSuffix(compact, "headers") || strings.HasSuffix(compact, "queries") {
				delete(v, key)
				continue
			}
			switch compact {
			case "body", "requestbody", "responsebody", "payload", "script", "ruledata":
				delete(v, key)
			default:
				scrubOpaqueChaos(item)
			}
		}
	case []any:
		for _, item := range v {
			scrubOpaqueChaos(item)
		}
	}
}

// scrub removes configured structured key classes and applies the same bounded text policy recursively.
func scrub(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			lower := strings.ToLower(key)
			sensitive := lower == "config" || lower == "data" || lower == "stringdata" || lower == "env" ||
				lower == "envfrom"
			sensitive = sensitive || sensitiveName.MatchString(lower)
			if sensitive {
				delete(v, key)
				continue
			}
			if text, ok := item.(string); ok {
				v[key] = sensitiveAssignment.ReplaceAllString(text, "[REDACTED]")
			} else {
				scrub(item)
			}
		}
	case []any:
		for i, item := range v {
			if text, ok := item.(string); ok {
				v[i] = sensitiveAssignment.ReplaceAllString(text, "[REDACTED]")
			} else {
				scrub(item)
			}
		}
	}
}
