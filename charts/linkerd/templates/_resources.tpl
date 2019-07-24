{{/* Specify resource requests and limits for workloads */}}
{{- define "linkerd.resources" -}}
resources:
  {{- if or .CPU.Request .Memory.Request }}
  requests:
    {{- with .CPU.Request }}
    cpu: {{.}}
    {{- end }}
    {{- with .Memory.Request }}
    memory: {{.}}
    {{- end }}
  {{- end }}
  {{- if or .CPU.Limit .Memory.Limit }}
  limits:
    {{- with .CPU.Limit }}
    cpu: {{.}}
    {{- end }}
    {{- with .Memory.Limit }}
    memory: {{.}}
    {{- end }}
  {{- end }}
{{- end -}}
