{{- define "partials.proxy-init" -}}
- args:
  - --incoming-proxy-port
  - {{.Proxy.Port.Inbound | quote}}
  - --outgoing-proxy-port
  - {{.Proxy.Port.Outbound | quote}}
  - --proxy-uid
  - {{.Proxy.UID | quote}}
  - --inbound-ports-to-ignore
  - {{.Proxy.Port.Control}},{{.Proxy.Port.Admin}}{{ternary (printf ",%s" .Proxy.Port.IgnoreInboundPorts) "" (ne .Proxy.Port.IgnoreInboundPorts "")}}
  - --outbound-ports-to-ignore
  - {{.Proxy.Port.IgnoreOutboundPorts | quote}}
  image: {{.Image.Name}}:{{.Image.Version}}
  imagePullPolicy: {{.Image.PullPolicy}}
  name: linkerd-init
  {{- include "partials.resources" .ResourceRequirements | nindent 2 }}
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      add:
      - NET_ADMIN
      - NET_RAW
    privileged: false
    readOnlyRootFilesystem: true
    runAsNonRoot: false
    runAsUser: 0
{{- end -}}
