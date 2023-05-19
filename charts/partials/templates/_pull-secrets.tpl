{{- define "partials.image-pull-secrets"}}
{{- if . }}
imagePullSecrets:
{{ toYaml . | indent 2 }}
{{- end }}
{{- end -}}
