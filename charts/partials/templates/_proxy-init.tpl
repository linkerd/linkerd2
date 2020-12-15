{{- define "partials.proxy-init" -}}
args:
- --incoming-proxy-port
- {{.Values.global.proxy.ports.inbound | quote}}
- --outgoing-proxy-port
- {{.Values.global.proxy.ports.outbound | quote}}
- --proxy-uid
- {{.Values.global.proxy.uid | quote}}
- --inbound-ports-to-ignore
- "{{.Values.global.proxy.ports.control}},{{.Values.global.proxy.ports.admin}}{{ternary (printf ",%s" .Values.global.proxyInit.ignoreInboundPorts) "" (not (empty .Values.global.proxyInit.ignoreInboundPorts)) }}"
{{- if .Values.global.proxyInit.ignoreOutboundPorts }}
- --outbound-ports-to-ignore
- {{.Values.global.proxyInit.ignoreOutboundPorts | quote}}
{{- end }}
{{- if .Values.global.proxyInit.closeWaitTimeoutSecs }}
- --timeout-close-wait-secs
- {{ .Values.global.proxyInit.closeWaitTimeoutSecs | quote}}
{{- end }}
image: {{.Values.global.proxyInit.image.name}}:{{.Values.global.proxyInit.image.version}}
imagePullPolicy: {{.Values.global.proxyInit.image.pullPolicy}}
name: linkerd-init
{{ include "partials.resources" .Values.global.proxyInit.resources }}
securityContext:
  {{- if .Values.global.proxyInit.closeWaitTimeoutSecs }}
  allowPrivilegeEscalation: true
  {{- else }}
  allowPrivilegeEscalation: false
  {{- end }}
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
  {{- if .Values.global.proxyInit.closeWaitTimeoutSecs }}
  privileged: true
  {{- else }}
  privileged: false
  {{- end }}
  readOnlyRootFilesystem: true
  runAsNonRoot: false
  runAsUser: 0
terminationMessagePolicy: FallbackToLogsOnError
{{- if or (not .Values.global.cniEnabled) .Values.global.proxyInit.saMountPath }}
volumeMounts:
{{- end -}}
{{- if not .Values.global.cniEnabled }}
- mountPath: {{.Values.global.proxyInit.xtMountPath.mountPath}}
  name: {{.Values.global.proxyInit.xtMountPath.name}}
{{- end -}}
{{- if .Values.global.proxyInit.saMountPath }}
- mountPath: {{.Values.global.proxyInit.saMountPath.mountPath}}
  name: {{.Values.global.proxyInit.saMountPath.name}}
  readOnly: {{.Values.global.proxyInit.saMountPath.readOnly}}
{{- end -}}  
{{- end -}}
