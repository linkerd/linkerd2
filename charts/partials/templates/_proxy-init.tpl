{{- define "partials.proxy-init" -}}
args:
- --incoming-proxy-port
- {{.Values.proxy.ports.inbound | quote}}
- --outgoing-proxy-port
- {{.Values.proxy.ports.outbound | quote}}
- --proxy-uid
- {{.Values.proxy.uid | quote}}
- --inbound-ports-to-ignore
- "{{.Values.proxy.ports.control}},{{.Values.proxy.ports.admin}}{{ternary (printf ",%s" (.Values.proxyInit.ignoreInboundPorts | toString)) "" (not (empty .Values.proxyInit.ignoreInboundPorts)) }}"
{{- if .Values.proxyInit.ignoreOutboundPorts }}
- --outbound-ports-to-ignore
- {{.Values.proxyInit.ignoreOutboundPorts | quote}}
{{- end }}
{{- if .Values.proxyInit.closeWaitTimeoutSecs }}
- --timeout-close-wait-secs
- {{ .Values.proxyInit.closeWaitTimeoutSecs | quote}}
{{- end }}
image: {{.Values.proxyInit.image.name}}:{{.Values.proxyInit.image.version}}
imagePullPolicy: {{.Values.proxyInit.image.pullPolicy | default .Values.imagePullPolicy}}
name: linkerd-init
{{ include "partials.resources" .Values.proxyInit.resources }}
securityContext:
  {{- if .Values.proxyInit.closeWaitTimeoutSecs }}
  allowPrivilegeEscalation: true
  {{- else }}
  allowPrivilegeEscalation: false
  {{- end }}
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
  {{- if .Values.proxyInit.closeWaitTimeoutSecs }}
  privileged: true
  {{- else }}
  privileged: false
  {{- end }}
  readOnlyRootFilesystem: true
  runAsNonRoot: false
  runAsUser: 0
terminationMessagePolicy: FallbackToLogsOnError
{{- if or (not .Values.cniEnabled) .Values.proxyInit.saMountPath }}
volumeMounts:
{{- end -}}
{{- if not .Values.cniEnabled }}
- mountPath: {{.Values.proxyInit.xtMountPath.mountPath}}
  name: {{.Values.proxyInit.xtMountPath.name}}
{{- end -}}
{{- if .Values.proxyInit.saMountPath }}
- mountPath: {{.Values.proxyInit.saMountPath.mountPath}}
  name: {{.Values.proxyInit.saMountPath.name}}
  readOnly: {{.Values.proxyInit.saMountPath.readOnly}}
{{- end -}}
{{- end -}}
