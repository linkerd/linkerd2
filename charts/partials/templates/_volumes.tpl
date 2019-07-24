{{ define "partials.proxy-identity-volume" -}}
- name: linkerd-identity-end-entity
  emptyDir:
    medium: Memory
{{- end -}}
