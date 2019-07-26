{{ define "partials.proxy-identity-volume" -}}
- emptyDir:
    medium: Memory
  name: linkerd-identity-end-entity
{{- end -}}
