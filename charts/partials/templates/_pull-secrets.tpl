{{- define "partials.image-pull-secrets"}}
imagePullSecrets:
{{ toYaml . | indent 2 }}
{{- end -}}
