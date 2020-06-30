{{- define "linkerd.tolerations" -}}
tolerations:
{{ toYaml .Values.tolerations | trim | indent 2 }}
{{- end -}}
