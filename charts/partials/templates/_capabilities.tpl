{{- define "partials.proxy.capabilities" -}}
capabilities:
  {{- if .Values.Proxy.Capabilities.Add }}
  add:
  {{- toYaml .Values.Proxy.Capabilities.Add | trim | nindent 4 }}
  {{- end }}
  {{- if .Values.Proxy.Capabilities.Drop }}
  drop:
  {{- toYaml .Values.Proxy.Capabilities.Drop | trim | nindent 4 }}
  {{- end }}
{{- end -}}

{{- define "partials.proxy-init.capabilities.drop" -}}
drop:
{{ toYaml .Values.ProxyInit.Capabilities.Drop | trim }}
{{- end -}}
