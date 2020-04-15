{{- define "partials.proxy.annotations" -}}
linkerd.io/identity-mode: {{ternary "default" "disabled" (not .disableIdentity)}}
linkerd.io/proxy-version: {{.image.version}}
{{- end -}}

{{- define "partials.proxy.labels" -}}
linkerd.io/proxy-{{.workloadKind}}: {{.component}}
{{- end -}}
