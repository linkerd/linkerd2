{{- define "partials.debug" -}}
image: {{.image.name}}:{{.image.version}}
imagePullPolicy: {{.image.pullPolicy}}
name: linkerd-debug
terminationMessagePolicy: FallbackToLogsOnError
{{- end -}}
