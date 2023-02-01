{{- define "partials.resources" -}}
{{- $ephemeralStorage := index . "ephemeral-storage" -}}
resources:
  {{- if or (.cpu).limit (.memory).limit ($ephemeralStorage).limit }}
  limits:
    {{- with (.cpu).limit }}
    cpu: {{. | quote}}
    {{- end }}
    {{- with (.memory).limit }}
    memory: {{. | quote}}
    {{- end }}
    {{- with ($ephemeralStorage).limit }}
    ephemeral-storage: {{. | quote}}
    {{- end }}
  {{- end }}
  {{- if or (.cpu).request (.memory).request ($ephemeralStorage).request }}
  requests:
    {{- with (.cpu).request }}
    cpu: {{. | quote}}
    {{- end }}
    {{- with (.memory).request }}
    memory: {{. | quote}}
    {{- end }}
    {{- with ($ephemeralStorage).request }}
    ephemeral-storage: {{. | quote}}
    {{- end }}
  {{- end }}
{{- end }}
