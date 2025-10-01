{{ define "partials.linkerd.trace" -}}
{{ if .Values.controller.tracing.enabled -}}
{{- if empty .Values.controller.tracing.collector.endpoint }}
{{- fail "controller.tracing.collector.endpoint must be set if proxy tracing is enabled" }}
{{- end }}
- -trace-collector={{.Values.controller.tracing.collector.endpoint}}
{{ end -}}
{{- end }}
