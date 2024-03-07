{{ define "partials.linkerd.trace" -}}
{{ if .Values.controlPlaneTracing -}}
- -trace-collector={{.Values.controlPlaneTracingCollector.name}}.{{.Values.controlPlaneTracingCollector.namespace}}.svc.{{.Values.clusterDomain}}:{{.Values.controlPlaneTracingCollector.port}}
{{ end -}}
{{- end }}
