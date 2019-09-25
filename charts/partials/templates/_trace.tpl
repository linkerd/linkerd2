{{ define "partials.linkerd.trace" -}}
{{ if .TraceCollector -}}
- -trace-collector={{.TraceCollector}}
- -sampling-probability={{.ProbabilisticSamplingRate}}
{{ end -}}
{{- end }}
