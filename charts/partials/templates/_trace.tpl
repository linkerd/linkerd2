{{ define "partials.linkerd.trace" -}}
{{ if .Enabled -}}
- -trace-collector={{.ProxyTrace.CollectorSvcAddr}}
{{ end -}}
{{- end }}
