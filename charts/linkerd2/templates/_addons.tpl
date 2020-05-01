{- /*
  This function does the following functions for add-on fields:

  - when an add-on is enabled, it removes the `partials` and `global` sub-fields which are automatically added
    by helm to pass configuration to sub-charts, and returns the add-on fields.

  - When the add-on is disabled, only the `enabled` flag is returned as we don't need to store the other configuration
    when an add-on is disabled.
*/ -}}
{{- define "linkerd.addons.sanitize-config" -}}
{{- if .enabled -}}
{{- $dupValues := . -}}
{{- if kindIs "map" $dupValues -}}
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
