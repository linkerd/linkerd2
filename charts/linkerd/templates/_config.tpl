{{- define "linkerd.configs.global" -}}
{
  "linkerdNamespace": "{{.Namespace}}",
  "cniEnabled": {{.CNIEnabled}},
  "version": "{{.LinkerdVersion}}",
  "identityContext":{
    "trustDomain": "{{.Identity.TrustDomain}}",
    "trustAnchorsPem": "{{.Identity.Issuer.CrtPEM | replace "\n" "\\n"}}",
    "issuanceLifeTime": "{{.Identity.Issuer.IssuanceLifeTime}}",
    "clockSkewAllowance": "{{.Identity.Issuer.ClockSkewAllowance}}"
  },
  "autoInjectContext": null,
  "omitWebhookSideEffects": {{.OmitWebhookSideEffects}},
  "clusterDomain": "{{.ClusterDomain}}"
}
{{- end -}}

{{- define "linkerd.configs.proxy" -}}
{
  "proxyImage":{
    "imageName":"{{.Proxy.Image.Name}}",
    "pullPolicy":"{{.Proxy.Image.PullPolicy}}"
  },
  "proxyInitImage":{
    "imageName":"{{.ProxyInit.Image.Name}}",
    "pullPolicy":"{{.ProxyInit.Image.PullPolicy}}"
  },
  "controlPort":{
    "port": {{.Proxy.Ports.Control}}
  },
  "ignoreInboundPorts": {{splitList "," .ProxyInit.IgnoreInboundPorts}},
  "ignoreOutboundPorts": {{splitList "," .ProxyInit.IgnoreOutboundPorts}},
  "inboundPort":{
    "port": {{.Proxy.Ports.Inbound}}
  },
  "adminPort":{
    "port": {{.Proxy.Ports.Admin}}
  },
  "outboundPort":{
    "port": {{.Proxy.Ports.Outbound}}
  },
  "resource":{
    "requestCpu": "{{ternary .Proxy.Resources.CPU.Request "" (eq .HighAvailability true)}}",
    "requestMemory": "{{ternary .Proxy.Resources.Memory.Request "" (eq .HighAvailability true)}}"
    "limitCpu": "{{ternary .Proxy.Resources.CPU.Limit "" (eq .HighAvailability true)}}",
    "limitMemory": "{{ternary .Proxy.Resources.Memory.Limit "" (eq .HighAvailability true)}}",
  }
  "proxyUid": {{.Proxy.UID}},
  "logLevel":{
    "level": "{{.Proxy.LogLevel}}"
  },
  "disableExternalProfiles": {{not .Proxy.EnableExternalProfile}},
  "proxyVersion": "{{.Proxy.Image.Version}}",
  "proxyInitImageVersion": "{{.ProxyInit.Image.Version}}",
}
{{- end -}}

{{- define "linkerd.configs.install" -}}
{
  "uuid":"{{ uuidv4 }}",
  "cliVersion":"{{ .LinkerdVersion }}",
  "flags":[]
}
{{- end -}}
