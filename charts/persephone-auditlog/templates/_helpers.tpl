{{- define "name" -}}
persephone-extension-auditlog
{{- end -}}

{{- define "labels" -}}
app.kubernetes.io/name: {{ include "name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "image" -}}
{{- if hasPrefix "sha256:" .Values.image.tag -}}
{{ .Values.image.repository }}@{{ .Values.image.tag }}
{{- else -}}
{{ .Values.image.repository }}:{{ .Values.image.tag }}
{{- end -}}
{{- end -}}
