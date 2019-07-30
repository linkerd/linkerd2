{{- define "partials.proxy-init" -}}
- args:
  - --incoming-proxy-port
  - {{.Proxy.Ports.Inbound | quote}}
  - --outgoing-proxy-port
  - {{.Proxy.Ports.Outbound | quote}}
  - --proxy-uid
  - {{.Proxy.UID | quote}}
  - --inbound-ports-to-ignore
  - {{.Proxy.Ports.Control}},{{.Proxy.Ports.Admin}}{{ternary (printf ",%s" .ProxyInit.IgnoreInboundPorts) "" (not (empty .ProxyInit.IgnoreInboundPorts))}}
  - --outbound-ports-to-ignore
  - {{.ProxyInit.IgnoreOutboundPorts | quote}}
  image: {{.ProxyInit.Image.Name}}:{{.ProxyInit.Image.Version}}
  imagePullPolicy: {{.ProxyInit.Image.PullPolicy}}
  name: linkerd-init
  {{- include "partials.resources" .ProxyInit.Resources | nindent 2 }}
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      add:
      - NET_ADMIN
      - NET_RAW
      {{- if .ProxyInit.Capabilities -}}
      {{- if .ProxyInit.Capabilities.Add }}
      {{- toYaml .ProxyInit.Capabilities.Add | trim | nindent 6 }}
      {{- end }}
      {{- if .ProxyInit.Capabilities.Drop -}}
      {{- include "partials.proxy-init.capabilities.drop" .ProxyInit | nindent 6 -}}
      {{- end }}
      {{- end }}
    privileged: false
    readOnlyRootFilesystem: true
    runAsNonRoot: false
    runAsUser: 0
  terminationMessagePolicy: FallbackToLogsOnError
  {{- if .ProxyInit.MountPaths }}
  volumeMounts:
  {{- toYaml .ProxyInit.MountPaths | trim | nindent 2 -}}
  {{- end }}
{{- end -}}
