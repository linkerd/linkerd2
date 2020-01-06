{{- define "partials.proxy.capabilities" -}}
capabilities:
  {{- if .Values.global.proxy.capabilities.add }}
  add:
  {{- toYaml .Values.global.proxy.capabilities.add | trim | nindent 4 }}
  {{- end }}
  {{- if .Values.global.proxy.capabilities.drop }}
  drop:
  {{- toYaml .Values.global.proxy.capabilities.drop | trim | nindent 4 }}
  {{- end }}
{{- end -}}

{{- define "partials.proxy-init.capabilities.drop" -}}
drop:
{{ toYaml .Values.global.proxyInit.capabilities.drop | trim }}
{{- end -}}
