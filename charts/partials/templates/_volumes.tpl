{{ define "partials.proxy.volumes.identity" -}}
emptyDir:
  medium: Memory
name: linkerd-identity-end-entity
{{- end -}}

{{ define "partials.proxyInit.volumes.xtables" -}}
emptyDir: {}
name: {{ .Values.proxyInit.xtMountPath.name }}
{{- end -}}

{{- define "partials.proxy.volumes.service-account-token" -}}
name: linkerd-token
projected:
  sources:
  - serviceAccountToken:
      path: linkerd-token
      expirationSeconds: 86400
      audience: linkerd.io
{{- end -}}
