package telemetry

import (
	"bytes"
	_ "embed"
	"strconv"
	"text/template"

	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
)

// collectorTemplate is the pinned upstream collector's configuration contract.
//
//go:embed collector.yaml.tmpl
var collectorTemplate string

// CollectorConfig renders namespace/UID selection without accepting arbitrary collector configuration.
func CollectorConfig(t *observation.NetworkTelemetry) (string, error) {
	data := struct {
		Namespace            string
		TelemetryUID         string
		NetworkUID           string
		TablePrefix          string
		Retention            string
		Sources              observation.Sources
		SensitiveNames       string
		SensitiveAssignments string
		ExtractKeys          string
		ActorSource          string
		CollectorSource      string
	}{
		Namespace: t.Namespace, TelemetryUID: string(t.UID), NetworkUID: string(t.Spec.NetworkUID),
		SensitiveNames: SensitiveNamePattern, SensitiveAssignments: SensitiveAssignmentPattern,
		ExtractKeys: ExtractKeys, ActorSource: SourceActorNative, CollectorSource: SourceCollectorInternal,
		TablePrefix: TablePrefix(t), Retention: t.Spec.Retention.Window, Sources: t.Spec.Sources,
	}
	tmpl, err := template.New("collector").Funcs(template.FuncMap{"quote": strconv.Quote}).Parse(collectorTemplate)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = tmpl.Execute(&out, data)
	return out.String(), err
}
