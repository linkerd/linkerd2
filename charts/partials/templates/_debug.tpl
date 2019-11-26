{{- define "partials.debug" -}}
image: {{.Values.Image.Name}}:{{.Values.Image.Version}}
imagePullPolicy: {{.Values.Image.PullPolicy}}
name: linkerd-debug
terminationMessagePolicy: FallbackToLogsOnError
{{- end -}}
