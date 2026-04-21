{{- define "partials.proxy.config.annotations" -}}
{{- with .cpu }}
{{- with .request -}}
config.linkerd.io/proxy-cpu-request: {{. | quote}}
{{end}}
{{- with .limit -}}
config.linkerd.io/proxy-cpu-limit: {{. | quote}}
{{- end}}
{{- end}}
{{- with .memory }}
{{- with .request }}
config.linkerd.io/proxy-memory-request: {{. | quote}}
{{end}}
{{- with .limit -}}
config.linkerd.io/proxy-memory-limit: {{. | quote}}
{{- end}}
{{- end }}
{{- end }}
