{{ define "partials.linkerd.trace" -}}
{{ if .ControlPlaneTracing -}}
- -trace-collector=linkerd-collector.{{.Values.namespace}}.svc.{{.Values.clusterDomain}}:55678
{{ end -}}
{{- end }}
