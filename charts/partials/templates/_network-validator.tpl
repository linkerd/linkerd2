
{{- define "partials.network-validator" -}}
name: linkerd-network-validator
image: {{.Values.proxy.image.name}}:{{.Values.proxy.image.version}}
imagePullPolicy: {{.Values.proxy.image.pullPolicy | default .Values.imagePullPolicy}}
command:
  - /usr/lib/linkerd/linkerd2-network-validator
args:
  - --logFormat
  - {{ .Values.networkValidator.logFormat }}
  - --logLevel
  - {{ .Values.networkValidator.logLevel }}
  - --connectAddr
  - {{ .Values.networkValidator.connectAddr }}
  - --listenAddr
  - {{ .Values.networkValidator.listenAddr }}
  - --timeout
  - {{ .Values.networkValidator.timeout }}

{{- end -}}
