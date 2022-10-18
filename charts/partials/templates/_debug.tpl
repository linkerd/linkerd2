{{- define "partials.debug" -}}
image: "{{.Values.image.registry}}/{{.Values.debugContainer.image.name}}:{{.Values.debugContainer.image.version | default .Values.linkerdVersion}}"
imagePullPolicy: {{.Values.debugContainer.image.pullPolicy | default .Values.imagePullPolicy}}
name: linkerd-debug
terminationMessagePolicy: FallbackToLogsOnError
{{- end -}}
