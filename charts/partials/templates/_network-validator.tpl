{{- define "partials.network-validator" -}}
name: linkerd-network-validator
image: {{.Values.proxy.image.name}}:{{.Values.proxy.image.version | default .Values.linkerdVersion }}
imagePullPolicy: {{.Values.proxy.image.pullPolicy | default .Values.imagePullPolicy}}
{{ include "partials.resources" .Values.proxyInit.resources }}
{{- if or .Values.networkValidator.enableSecurityContext }}
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
    - ALL
  readOnlyRootFilesystem: true
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
  - {{ .Values.networkValidator.connectAddr }}
  - --listen-addr
  - {{ .Values.networkValidator.listenAddr }}
  - --timeout
  - {{ .Values.networkValidator.timeout }}

{{- end -}}
