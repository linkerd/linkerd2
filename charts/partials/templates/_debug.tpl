{{- define "partials.debug" -}}
image: {{.Values.debugContainer.image.name}}:{{.Values.debugContainer.image.version | default .Values.linkerdVersion}}
imagePullPolicy: {{.Values.debugContainer.image.pullPolicy | default .Values.imagePullPolicy}}
name: linkerd-debug
terminationMessagePolicy: FallbackToLogsOnError
{{- end -}}
