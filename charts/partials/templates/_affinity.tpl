{{ define "linkerd.node-affinity" -}}
nodeAffinity:
{{- toYaml .Values.nodeAffinity | trim | nindent 2 }}
{{- end }}

{{ define "linkerd.affinity" -}}
{{- if .Values.nodeAffinity -}}
affinity:
{{- include "linkerd.node-affinity" . | nindent 2 }}
{{- end }}
{{- end }}
