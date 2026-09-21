{{- define "eino-lab.name" -}}
{{- printf "%s-%s" .Release.Name .component -}}
{{- end -}}

{{- define "eino-lab.labels" -}}
app.kubernetes.io/name: eino-lab
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: {{ .component }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "eino-lab.fullimage" -}}
{{- if .image.registry -}}
{{- printf "%s/%s:%s" .image.registry .image.name .image.tag -}}
{{- else -}}
{{- printf "%s:%s" .image.name .image.tag -}}
{{- end -}}
{{- end -}}
