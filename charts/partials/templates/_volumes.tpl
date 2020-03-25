{{ define "partials.proxy.volumes.identity" -}}
emptyDir:
  medium: Memory
name: linkerd-identity-end-entity
{{- end -}}

{{ define "partials.proxy.volumes.labels" -}}
name: podinfo
downwardAPI:
  items:
    - path: "labels"
      fieldRef:
        fieldPath: metadata.labels
{{- end -}}