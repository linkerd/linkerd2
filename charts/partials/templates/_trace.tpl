{{ define "partials.linkerd.trace" -}}
{{ if .Enabled -}}
- -trace-collector={{.ProxyTrace.CollectorSvcAddr}}
- -sampling-probability={{.SamplingProbability}}
{{ end -}}
{{- end }}
