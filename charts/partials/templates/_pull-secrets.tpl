{{- define "partials.image-pull-secrets" -}}
{{- if .Values.imagePullSecrets }}
imagePullSecrets:
{{ toYaml .Values.imagePullSecrets | indent 2 }}
{{- end }}
{{- end -}}
