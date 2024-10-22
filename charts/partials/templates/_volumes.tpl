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

{{- define "partials.volumes.manual-mount-service-account-token" -}}
name: kube-api-access
projected:
  defaultMode: 420
  sources:
  - serviceAccountToken:
      expirationSeconds: 3607
      path: token
  - configMap:
      items:
      - key: ca.crt
        path: ca.crt
      name: kube-root-ca.crt
  - downwardAPI:
      items:
      - fieldRef:
          apiVersion: v1
          fieldPath: metadata.namespace
        path: namespace
{{- end -}}