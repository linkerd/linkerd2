{{ define "linkerd.affinity" -}}
{{- if .Values.nodeAffinity -}}
affinity:
  nodeAffinity:
  {{- toYaml .Values.nodeAffinity | trim | nindent 4 }}
{{- end }}
{{- end }}
