{{ define "partials.proxy.volumes.identity" -}}
emptyDir:
  medium: Memory
name: linkerd-identity-end-entity
{{- end -}}

{{ define "partials.proxyInit.volumes.xtables" -}}
emptyDir: {}
name: {{ .Values.proxyInit.xtMountPath.name }}
{{- end -}}
