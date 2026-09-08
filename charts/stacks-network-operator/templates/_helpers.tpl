{{- define "stacks-network-operator.name" -}}
stacks-network-operator
{{- end }}

{{- define "stacks-network-operator.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "stacks-network-operator.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "stacks-network-operator.labels" -}}
app.kubernetes.io/name: {{ include "stacks-network-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end }}

{{- define "stacks-network-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "stacks-network-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "stacks-network-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "stacks-network-operator.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- required "serviceAccount.name is required when serviceAccount.create=false" .Values.serviceAccount.name -}}
{{- end -}}
{{- end }}
