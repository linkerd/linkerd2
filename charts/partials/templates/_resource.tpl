{{- define "partials.resource" -}}
resources:
  {{- if or .CPU.Limit .Memory.Limit }}
  limits:
    {{- with .CPU.Limit }}
    cpu: {{.}}
    {{- end }}
    {{- with .Memory.Limit }}
    memory: {{.}}
    {{- end }}
  {{- end }}
  {{- if or .CPU.Request .Memory.Request }}
  requests:
    {{- with .CPU.Request }}
    cpu: {{.}}
    {{- end }}
    {{- with .Memory.Request }}
    memory: {{.}}
    {{- end }}
  {{- end }}
{{- end }}
