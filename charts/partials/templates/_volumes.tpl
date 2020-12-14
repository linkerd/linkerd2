{{ define "partials.proxy.volumes.identity" -}}
emptyDir:
  medium: Memory
name: linkerd-identity-end-entity
{{- end -}}

{{ define "partials.proxyInit.volumes.xtables" -}}
emptyDir: {}
name: {{ .Values.global.proxyInit.xtMountPath.name }}
{{- end -}}
