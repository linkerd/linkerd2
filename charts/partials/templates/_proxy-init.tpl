{{- define "partials.proxy-init" -}}
args:
{{- if (.Values.proxyInit.iptablesMode | default "legacy" | eq "nft") }}
- --firewall-bin-path
- "iptables-nft"
- --firewall-save-bin-path
- "iptables-nft-save"
{{- else if not (eq .Values.proxyInit.iptablesMode "legacy") }}
{{ fail (printf "Unsupported value \"%s\" for proxyInit.iptablesMode\nValid values: [\"nft\", \"legacy\"]" .Values.proxyInit.iptablesMode) }}
{{end -}}
{{- if .Values.disableIPv6 }}
- --ipv6=false
{{- end }}
- --incoming-proxy-port
- {{.Values.proxy.ports.inbound | quote}}
- --outgoing-proxy-port
- {{.Values.proxy.ports.outbound | quote}}
- --proxy-uid
- {{.Values.proxy.uid | quote}}
{{- if ge (int .Values.proxy.gid) 0 }}
- --proxy-gid
- {{.Values.proxy.gid | quote}}
{{- end }}
- --inbound-ports-to-ignore
- "{{.Values.proxy.ports.control}},{{.Values.proxy.ports.admin}}{{ternary (printf ",%s" (.Values.proxyInit.ignoreInboundPorts | toString)) "" (not (empty .Values.proxyInit.ignoreInboundPorts)) }}"
{{- if .Values.proxyInit.ignoreOutboundPorts }}
- --outbound-ports-to-ignore
- {{.Values.proxyInit.ignoreOutboundPorts | quote}}
{{- end }}
{{- if .Values.proxyInit.closeWaitTimeoutSecs }}
  {{- if not .Values.proxyInit.runAsRoot }}
{{ fail "proxyInit.runAsRoot must be set to use proxyInit.closeWaitTimeoutSecs" }}
  {{- end }}
- --timeout-close-wait-secs
- {{ .Values.proxyInit.closeWaitTimeoutSecs | quote}}
{{- end }}
{{- if .Values.proxyInit.logFormat }}
- --log-format
- {{ .Values.proxyInit.logFormat }}
{{- end }}
{{- if .Values.proxyInit.logLevel }}
- --log-level
- {{ .Values.proxyInit.logLevel }}
{{- end }}
{{- if .Values.proxyInit.skipSubnets }}
- --subnets-to-ignore
- {{ .Values.proxyInit.skipSubnets | quote }}
{{- end }}
image: {{.Values.proxyInit.image.name}}:{{.Values.proxyInit.image.version}}
imagePullPolicy: {{.Values.proxyInit.image.pullPolicy | default .Values.imagePullPolicy}}
name: linkerd-init
{{ include "partials.resources" .Values.proxy.resources }}
securityContext:
  {{- if or .Values.proxyInit.closeWaitTimeoutSecs .Values.proxyInit.privileged }}
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
  {{- if or .Values.proxyInit.closeWaitTimeoutSecs .Values.proxyInit.privileged }}
  privileged: true
  {{- else }}
  privileged: false
  {{- end }}
  {{- if .Values.proxyInit.runAsRoot }}
  runAsGroup: 0
  runAsNonRoot: false
  runAsUser: 0
  {{- else }}
  runAsNonRoot: true
  runAsUser: {{ .Values.proxyInit.runAsUser | int | eq 0 | ternary 65534 .Values.proxyInit.runAsUser }}
  runAsGroup: {{ .Values.proxyInit.runAsGroup | int | eq 0 | ternary 65534 .Values.proxyInit.runAsGroup }}
  {{- end }}
  readOnlyRootFilesystem: true
  seccompProfile:
    type: RuntimeDefault
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
