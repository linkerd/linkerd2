{- /*
  This function does the following functions for add-on fields:

  - when an add-on is enabled, it removes the `partials` and `global` sub-fields which are automatically added
    by helm to pass configuration to sub-charts, and returns the add-on fields.

  - When the add-on is disabled, only the `enabled` flag is returned as we don't need to store the other configuration.
*/ -}}
{{- define "linkerd.addons.sanitize-config" -}}
{{- if .enabled -}}
{{ $dup := omit . "global" "partials" }}
{{- toYaml $dup | trim | nindent 6 }}
{{- else }}
      enabled: false
{{- end -}}
{{- end -}}
