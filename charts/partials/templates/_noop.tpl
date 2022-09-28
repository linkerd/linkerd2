{{- define "partials.noop" -}}
command:
  - /bin/sleep
  - "0"
image: {{.Values.proxy.image.name}}:{{.Values.proxy.image.version | default .Values.linkerdVersion}}
imagePullPolicy: {{.Values.proxy.image.pullPolicy | default .Values.imagePullPolicy}}
name: noop
{{- end -}}
