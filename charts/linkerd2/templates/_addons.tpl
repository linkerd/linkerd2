{{- define "linkerd.addons.sanitize-config" -}}
{{- if .enabled -}}
{{- $dupValues := . -}}
{{- if kindIs "map" $dupValues -}}
  {{/*
  Remove "global" and "partial" keys from the add-on structs as they are added by helm
  to propogate values.
  */}}
  {{- if (hasKey $dupValues "global") -}}
      {{- $dupValues := unset $dupValues "global" -}}
  {{- end -}}
  {{- if (hasKey $dupValues "partials") -}}
      {{- $dupValues := unset $dupValues "partials" -}}
  {{- end -}}
{{- end -}}
{{- toYaml . | trim | nindent 6 }}
{{- else }}
      enabled: false
{{- end -}}
{{- end -}}