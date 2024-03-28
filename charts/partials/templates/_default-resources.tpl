{{- define "partials.default-resources" -}}
{{- $use_default := true -}}
{{- $resources := index . "resources" -}}
{{- range $_, $resource := $resources -}}
  {{- range $_, $val := $resource -}}
    {{- if $val -}}
      {{- $use_default = false -}}
      {{- break -}}
    {{- end -}}
  {{- end -}}
  {{- if not $use_default -}}
    {{- break -}}
  {{- end -}}
{{- end -}}
{{- if $use_default }}
{{- include "partials.resources" .default }}
{{- else }}
{{- include "partials.resources" .resources }}
{{- end }}
{{- end }}
