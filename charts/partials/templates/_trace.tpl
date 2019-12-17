{{ define "partials.linkerd.trace" -}}
{{ if .ControlPlaneTracing -}}
- -trace-collector=linkerd-collector.{{.Values.global.namespace}}.svc.{{.Values.global.clusterDomain}}:55678
{{ end -}}
{{- end }}
