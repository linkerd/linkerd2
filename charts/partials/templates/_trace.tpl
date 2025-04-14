{{ define "partials.linkerd.trace" -}}
{{ if .Values.controlPlaneTracing -}}
- -trace-collector=collector.{{.Values.controlPlaneTracingNamespace}}.svc.{{.Values.clusterDomain}}:4317
{{ end -}}
{{- end }}
