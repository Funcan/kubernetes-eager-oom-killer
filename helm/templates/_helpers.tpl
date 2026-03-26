{{- define "eager-oom-killer.name" -}}
eager-oom-killer
{{- end -}}

{{- define "eager-oom-killer.fullname" -}}
{{ .Release.Name }}-eager-oom-killer
{{- end -}}

{{- define "eager-oom-killer.labels" -}}
app.kubernetes.io/name: {{ include "eager-oom-killer.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "eager-oom-killer.selectorLabels" -}}
app.kubernetes.io/name: {{ include "eager-oom-killer.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
