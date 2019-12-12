{{- define "partials.resources" -}}
resources:
  {{- if or .cpu.limit .memory.limit }}
  limits:
    {{- with .cpu.limit }}
    cpu: {{. | quote}}
    {{- end }}
    {{- with .memory.limit }}
    memory: {{. | quote}}
    {{- end }}
  {{- end }}
  {{- if or .cpu.request .memory.request }}
  requests:
    {{- with .cpu.request }}
    cpu: {{. | quote}}
    {{- end }}
    {{- with .memory.request }}
    memory: {{. | quote}}
    {{- end }}
  {{- end }}
{{- end }}
