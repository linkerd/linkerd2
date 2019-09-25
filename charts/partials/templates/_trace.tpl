{{ define "partials.linkerd.trace" -}}
{{ if .TraceCollector -}}
- -trace-collector={{.TraceCollector}}
- -sampling-rate={{.ProbabilisticSamplingRate}}
{{ end -}}
{{- end }}
