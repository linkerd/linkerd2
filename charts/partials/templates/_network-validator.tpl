
{{- define "partials.network-validator" -}}
name: linkerd-network-validator
image: {{.Values.proxy.image.name}}:{{.Values.proxy.image.version}}
imagePullPolicy: {{.Values.proxy.image.pullPolicy | default .Values.imagePullPolicy}}
command:
  - /usr/lib/linkerd/linkerd2-network-validator
args:
  - --log-format
  - {{ .Values.networkValidator.logFormat }}
  - --log-level
  - {{ .Values.networkValidator.logLevel }}
  - --connect-addr
  - {{ .Values.networkValidator.connectAddr }}
  - --listen-addr
  - {{ .Values.networkValidator.listenAddr }}
  - --timeout
  - {{ .Values.networkValidator.timeout }}

{{- end -}}
