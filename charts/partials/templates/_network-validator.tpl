{{- define "partials.network-validator" -}}
name: linkerd-network-validator
image: {{.Values.proxy.image.name}}:{{.Values.proxy.image.version | default .Values.linkerdVersion }}
imagePullPolicy: {{.Values.proxy.image.pullPolicy | default .Values.imagePullPolicy}}
{{ include "partials.resources" .Values.proxy.resources }}
{{- if or .Values.networkValidator.enableSecurityContext }}
securityContext:
  {{- toYaml .Values.networkValidator.securityContext | trim | nindent 2 }}
{{- end }}
command:
  - /usr/lib/linkerd/linkerd2-network-validator
args:
  - --log-format
  - {{ .Values.networkValidator.logFormat }}
  - --log-level
  - {{ .Values.networkValidator.logLevel }}
  - --connect-addr
    {{- if .Values.networkValidator.connectAddr }}
  - {{ .Values.networkValidator.connectAddr | quote }}
    {{- else if .Values.disableIPv6}}
  - "1.1.1.1:20001"
    {{- else }}
  - "[fd00::1]:20001"
    {{- end }}
  - --listen-addr
    {{- if .Values.networkValidator.listenAddr }}
  - {{ .Values.networkValidator.listenAddr | quote }}
    {{- else if .Values.disableIPv6}}
  - "0.0.0.0:4140"
    {{- else }}
  - "[::]:4140"
    {{- end }}
  - --timeout
  - {{ .Values.networkValidator.timeout }}

{{- end -}}
