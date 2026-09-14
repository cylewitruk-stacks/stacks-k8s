{{- define "stacks-observability-operator.name" -}}
stacks-observability-operator
{{- end }}

{{- define "stacks-observability-operator.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "stacks-observability-operator.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "stacks-observability-operator.labels" -}}
app.kubernetes.io/name: {{ include "stacks-observability-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end }}

{{- define "stacks-observability-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "stacks-observability-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "stacks-observability-operator.serviceAccountName" -}}
{{- default (include "stacks-observability-operator.fullname" .) .Values.serviceAccount.name -}}
{{- end }}

{{- define "stacks-observability-operator.watchNamespaces" -}}
{{- if .Values.watchNamespaces -}}{{ join "," .Values.watchNamespaces }}{{- else -}}{{ .Release.Namespace }}{{- end -}}
{{- end -}}
