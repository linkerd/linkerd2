{{- define "linkerd.configs.global" -}}
{
  "linkerdNamespace": "{{.Values.Namespace}}",
  "cniEnabled": false,
  "version": "{{.Values.LinkerdVersion}}",
  "identityContext":{
    "trustDomain": "{{.Values.Identity.TrustDomain}}",
    "trustAnchorsPem": "{{required "Please provide the identity trust anchors" .Values.Identity.TrustAnchorsPEM | trim | replace "\n" "\\n"}}",
    "issuanceLifeTime": "{{.Values.Identity.Issuer.IssuanceLifeTime}}",
    "clockSkewAllowance": "{{.Values.Identity.Issuer.ClockSkewAllowance}}",
    "scheme": "{{.Values.Identity.Issuer.Scheme}}"
  },
  "autoInjectContext": null,
  "omitWebhookSideEffects": {{.Values.OmitWebhookSideEffects}},
  "clusterDomain": "{{.Values.ClusterDomain}}"
}
{{- end -}}

{{- define "linkerd.configs.proxy" -}}
{
  "proxyImage":{
    "imageName":"{{.Values.Proxy.Image.Name}}",
    "pullPolicy":"{{.Values.Proxy.Image.PullPolicy}}"
  },
  "proxyInitImage":{
    "imageName":"{{.Values.ProxyInit.Image.Name}}",
    "pullPolicy":"{{.Values.ProxyInit.Image.PullPolicy}}"
  },
  "controlPort":{
    "port": {{.Values.Proxy.Ports.Control}}
  },
  "ignoreInboundPorts":[
    {{- $ports := splitList "," .Values.ProxyInit.IgnoreInboundPorts -}}
    {{- if gt (len $ports) 1}}
    {{- $last := sub (len $ports) 1 -}}
    {{- range $i,$port := $ports -}}
    {"port":{{$port}}}{{ternary "," "" (ne $i $last)}}
    {{- end -}}
    {{- end -}}
  ],
  "ignoreOutboundPorts":[
    {{- $ports := splitList "," .Values.ProxyInit.IgnoreOutboundPorts -}}
    {{- if gt (len $ports) 1}}
    {{- $last := sub (len $ports) 1 -}}
    {{- range $i,$port := $ports -}}
    {"port":{{$port}}}{{ternary "," "" (ne $i $last)}}
    {{- end -}}
    {{- end -}}
  ],
  "inboundPort":{
    "port": {{.Values.Proxy.Ports.Inbound}}
  },
  "adminPort":{
    "port": {{.Values.Proxy.Ports.Admin}}
  },
  "outboundPort":{
    "port": {{.Values.Proxy.Ports.Outbound}}
  },
  "resource":{
    "requestCpu": "{{.Values.Proxy.Resources.CPU.Request}}",
    "limitCpu": "{{.Values.Proxy.Resources.CPU.Limit}}",
    "requestMemory": "{{.Values.Proxy.Resources.Memory.Request}}",
    "limitMemory": "{{.Values.Proxy.Resources.Memory.Limit}}"
  },
  "proxyUid": {{.Values.Proxy.UID}},
  "logLevel":{
    "level": "{{.Values.Proxy.LogLevel}}"
  },
  "disableExternalProfiles": {{not .Values.Proxy.EnableExternalProfiles}},
  "proxyVersion": "{{.Values.Proxy.Image.Version}}",
  "proxyInitImageVersion": "{{.Values.ProxyInit.Image.Version}}"
}
{{- end -}}

{{- define "linkerd.configs.install" -}}
{
  "cliVersion":"{{ .Values.LinkerdVersion }}",
  "flags":[]
}
{{- end -}}
