{{- define "partials.proxy-init" -}}
args:
- --incoming-proxy-port
- {{.Values.global.proxy.ports.inbound | quote}}
- --outgoing-proxy-port
- {{.Values.global.proxy.ports.outbound | quote}}
- --proxy-uid
- {{.Values.global.proxy.uid | quote}}
- --inbound-ports-to-ignore
- {{.Values.global.proxy.ports.control}},{{.Values.global.proxy.ports.admin}}{{ternary (printf ",%s" .Values.global.proxyInit.ignoreInboundPorts) "" (not (empty .Values.global.proxyInit.ignoreInboundPorts)) }}
{{- if hasPrefix "linkerd-" .Values.global.proxy.component }}
- --outbound-ports-to-ignore
- {{ternary (printf "443,%s" .Values.global.proxyInit.ignoreOutboundPorts) (quote "443") (not (empty .Values.global.proxyInit.ignoreOutboundPorts)) }}
{{- else if .Values.global.proxyInit.ignoreOutboundPorts }}
- --outbound-ports-to-ignore
- {{.Values.global.proxyInit.ignoreOutboundPorts | quote}}
{{- end }}
image: {{.Values.global.proxyInit.image.name}}:{{.Values.global.proxyInit.image.version}}
imagePullPolicy: {{.Values.global.proxyInit.image.pullPolicy}}
name: linkerd-init
{{ include "partials.resources" .Values.global.proxyInit.resources }}
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    add:
    - NET_ADMIN
    - NET_RAW
    {{- if .Values.global.proxyInit.capabilities -}}
    {{- if .Values.global.proxyInit.capabilities.add }}
    {{- toYaml .Values.global.proxyInit.capabilities.add | trim | nindent 4 }}
    {{- end }}
    {{- if .Values.global.proxyInit.capabilities.drop -}}
    {{- include "partials.proxy-init.capabilities.drop" . | nindent 4 -}}
    {{- end }}
    {{- end }}
  privileged: false
  readOnlyRootFilesystem: true
  runAsNonRoot: false
  runAsUser: 0
terminationMessagePolicy: FallbackToLogsOnError
{{- if .Values.global.proxyInit.saMountPath }}
volumeMounts:
- mountPath: {{.Values.global.proxyInit.saMountPath.mountPath}}
  name: {{.Values.global.proxyInit.saMountPath.name}}
  readOnly: {{.Values.global.proxyInit.saMountPath.readOnly}}
{{- end -}}
{{- end -}}
