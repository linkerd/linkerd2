{{- define "partials.proxy.capabilities" -}}
capabilities:
  {{- if .Values.proxy.capabilities.add }}
  add:
  {{- toYaml .Values.proxy.capabilities.add | trim | nindent 4 }}
  {{- end }}
  {{- if .Values.proxy.capabilities.drop }}
  drop:
  {{- toYaml .Values.proxy.capabilities.drop | trim | nindent 4 }}
  {{- end }}
{{- end -}}

{{- define "partials.proxy-init.capabilities.drop" -}}
drop:
{{ toYaml .Values.proxyInit.capabilities.drop | trim }}
{{- end -}}
