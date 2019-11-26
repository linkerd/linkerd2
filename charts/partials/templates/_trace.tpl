{{ define "partials.linkerd.trace" -}}
{{ if .ControlPlaneTracing -}}
- -trace-collector=linkerd-collector.{{.Values.Namespace}}.svc.{{.Values.ClusterDomain}}:55678
{{ end -}}
{{- end }}
