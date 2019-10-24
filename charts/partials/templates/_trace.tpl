{{ define "partials.linkerd.trace" -}}
{{ if .ControlPlaneTracing -}}
- -trace-collector=linkerd-collector.{{.Namespace}}.svc.{{.ClusterDomain}}:55678
{{ end -}}
{{- end }}
