{{- define "partials.proxy-init" -}}
args:
- --incoming-proxy-port
- {{.Values.proxy.ports.inbound | quote}}
- --outgoing-proxy-port
- {{.Values.proxy.ports.outbound | quote}}
- --proxy-uid
- {{.Values.proxy.uid | quote}}
- --inbound-ports-to-ignore
- {{.Values.proxy.ports.control}},{{.Values.proxy.ports.admin}}{{ternary (printf ",%s" .Values.proxyInit.ignoreInboundPorts) "" (not (empty .Values.proxyInit.ignoreInboundPorts)) }}
{{- if hasPrefix "linkerd-" .Values.proxy.component }}
- --outbound-ports-to-ignore
- {{ternary (printf "443,%s" .Values.proxyInit.ignoreOutboundPorts) (quote "443") (not (empty .Values.proxyInit.ignoreOutboundPorts)) }}
{{- else if .Values.proxyInit.ignoreOutboundPorts }}
- --outbound-ports-to-ignore
- {{.Values.proxyInit.ignoreOutboundPorts | quote}}
{{- end }}
image: {{.Values.proxyInit.image.name}}:{{.Values.proxyInit.image.version}}
imagePullPolicy: {{.Values.proxyInit.image.pullPolicy}}
name: linkerd-init
{{ include "partials.resources" .Values.proxyInit.resources }}
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    add:
    - NET_ADMIN
    - NET_RAW
    {{- if .Values.proxyInit.capabilities -}}
    {{- if .Values.proxyInit.capabilities.add }}
    {{- toYaml .Values.proxyInit.capabilities.add | trim | nindent 4 }}
    {{- end }}
    {{- if .Values.proxyInit.capabilities.drop -}}
    {{- include "partials.proxy-init.capabilities.drop" . | nindent 4 -}}
    {{- end }}
    {{- end }}
  privileged: false
  readOnlyRootFilesystem: true
  runAsNonRoot: false
  runAsUser: 0
terminationMessagePolicy: FallbackToLogsOnError
{{- if .Values.proxyInit.saMountPath }}
volumeMounts:
- mountPath: {{.Values.proxyInit.saMountPath.mountPath}}
  name: {{.Values.proxyInit.saMountPath.name}}
  readOnly: {{.Values.proxyInit.saMountPath.readOnly}}
{{- end -}}
{{- end -}}
