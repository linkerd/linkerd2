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
      {{- if .Capabilities -}}
      {{- if .Capabilities.Add }}
      {{- toYaml .Capabilities.Add | trim | nindent 6 }}
      {{- end }}
      {{- if .Capabilities.Drop -}}
      {{- include "partials.proxy-init.capabilities.drop" . | nindent 6 -}}
      {{- end }}
      {{- end }}
    privileged: false
    readOnlyRootFilesystem: true
    runAsNonRoot: false
    runAsUser: 0
  terminationMessagePolicy: FallbackToLogsOnError
  {{- if .MountPaths }}
  volumeMounts:
  {{- toYaml .MountPaths | trim | nindent 2 -}}
  {{- end }}
{{- end -}}
