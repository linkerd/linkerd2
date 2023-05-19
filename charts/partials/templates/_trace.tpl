{{ define "partials.linkerd.trace" -}}
{{ if .Values.controlPlaneTracing -}}
- -trace-collector=collector.{{.Values.controlPlaneTracingNamespace}}.svc.{{.Values.clusterDomain}}:55678
{{ end -}}
{{- end }}
