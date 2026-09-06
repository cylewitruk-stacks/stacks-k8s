{{- define "stacks-action-operator.name" -}}
stacks-action-operator
{{- end }}

{{- define "stacks-action-operator.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "stacks-action-operator.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "stacks-action-operator.labels" -}}
app.kubernetes.io/name: {{ include "stacks-action-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end }}

{{- define "stacks-action-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "stacks-action-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "stacks-action-operator.serviceAccountName" -}}
{{- default (include "stacks-action-operator.fullname" .) .Values.serviceAccount.name -}}
{{- end }}
