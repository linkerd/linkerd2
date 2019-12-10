{{- define "partials.proxy-init" -}}
args:
- --incoming-proxy-port
- {{.Values.Proxy.Ports.Inbound | quote}}
- --outgoing-proxy-port
- {{.Values.Proxy.Ports.Outbound | quote}}
- --proxy-uid
- {{.Values.Proxy.UID | quote}}
- --inbound-ports-to-ignore
- {{.Values.Proxy.Ports.Control}},{{.Values.Proxy.Ports.Admin}}{{ternary (printf ",%s" .Values.ProxyInit.IgnoreInboundPorts) "" (not (empty .Values.ProxyInit.IgnoreInboundPorts)) }}
{{- if hasPrefix "linkerd-" .Values.Proxy.Component }}
- --outbound-ports-to-ignore
- {{ternary (printf "443,%s" .Values.ProxyInit.IgnoreOutboundPorts) (quote "443") (not (empty .Values.ProxyInit.IgnoreOutboundPorts)) }}
{{- else if .Values.ProxyInit.IgnoreOutboundPorts }}
- --outbound-ports-to-ignore
- {{.Values.ProxyInit.IgnoreOutboundPorts | quote}}
{{- end }}
image: {{.Values.ProxyInit.Image.Name}}:{{.Values.ProxyInit.Image.Version}}
imagePullPolicy: {{.Values.ProxyInit.Image.PullPolicy}}
name: linkerd-init
{{ include "partials.resources" .Values.ProxyInit.Resources }}
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    add:
    - NET_ADMIN
    - NET_RAW
    {{- if .Values.ProxyInit.Capabilities -}}
    {{- if .Values.ProxyInit.Capabilities.Add }}
    {{- toYaml .Values.ProxyInit.Capabilities.Add | trim | nindent 4 }}
    {{- end }}
    {{- if .Values.ProxyInit.Capabilities.Drop -}}
    {{- include "partials.proxy-init.capabilities.drop" . | nindent 4 -}}
    {{- end }}
    {{- end }}
  privileged: false
  readOnlyRootFilesystem: true
  runAsNonRoot: false
  runAsUser: 0
terminationMessagePolicy: FallbackToLogsOnError
{{- if .Values.ProxyInit.SAMountPath }}
volumeMounts:
- mountPath: {{.Values.ProxyInit.SAMountPath.MountPath}}
  name: {{.Values.ProxyInit.SAMountPath.Name}}
  readOnly: {{.Values.ProxyInit.SAMountPath.ReadOnly}}
{{- end -}}
{{- end -}}
