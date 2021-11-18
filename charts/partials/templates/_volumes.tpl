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
name: linkerd-identity-token
projected:
  sources:
  - serviceAccountToken:
      path: linkerd-identity-token
      expirationSeconds: 86400 {{- /* # 24 hours */}}
      audience: identity.l5d.io
{{- end -}}
