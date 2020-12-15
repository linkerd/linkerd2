{{ define "partials.linkerd.trace" -}}
{{ if .Values.global.controlPlaneTracing -}}
- -trace-collector=collector.{{.Values.global.controlPlaneTracingNamespace}}.svc.{{.Values.global.clusterDomain}}:55678
{{ end -}}
{{- end }}
