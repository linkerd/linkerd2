{{ define "partials.proxy.volumes.identity" -}}
emptyDir:
  medium: Memory
name: linkerd-identity-end-entity
{{- end -}}

{{ define "partials.proxy.volumes.labels" -}}
downwardAPI:
  items:
  - fieldRef:
      fieldPath: metadata.labels
    path: "labels"
name: podinfo
{{- end -}}