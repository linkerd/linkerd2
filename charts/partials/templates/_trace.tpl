{{ define "partials.linkerd.trace" -}}
{{ if ((.Values.controller.tracing).enabled) -}}
{{- if (.Values.controller.tracing.collector).endpoint }}
- -trace-collector={{.Values.controller.tracing.collector.endpoint}}
{{- else if ((.Values.proxy.tracing).collector).endpoint }}
- -trace-collector={{.Values.proxy.tracing.collector.endpoint}}
{{- else }}
{{- fail "controller.tracing.collector.endpoint or proxy.tracing.collector.endpoint must be set if control plane tracing is enabled" }}
{{- end }}
{{ end -}}
{{- end }}
