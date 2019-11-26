{{- define "partials.proxy.capabilities" -}}
capabilities:
  {{- if .Values.Capabilities.Add }}
  add:
  {{- toYaml .Values.Capabilities.Add | trim | nindent 4 }}
  {{- end }}
  {{- if .Values.Capabilities.Drop }}
  drop:
  {{- toYaml .Values.Capabilities.Drop | trim | nindent 4 }}
  {{- end }}
{{- end -}}

{{- define "partials.proxy-init.capabilities.drop" -}}
drop:
{{ toYaml .Values.Capabilities.Drop | trim }}
{{- end -}}
