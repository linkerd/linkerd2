{{- define "partials.network-validator" -}}
name: linkerd-network-validator
image: {{.Values.proxy.image.name}}:{{.Values.proxy.image.version | default .Values.linkerdVersion }}
imagePullPolicy: {{.Values.proxy.image.pullPolicy | default .Values.imagePullPolicy}}
{{ include "partials.resources" .Values.proxy.resources }}
{{- if or .Values.networkValidator.enableSecurityContext }}
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
    - ALL
  readOnlyRootFilesystem: true
  runAsGroup: 65534
  runAsNonRoot: true
  runAsUser: 65534
  seccompProfile:
    type: RuntimeDefault
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
