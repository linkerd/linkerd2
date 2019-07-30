{{- define "linkerd.configs.global" -}}
{
  "autoInjectContext": null,
  "clusterDomain": "{{.ClusterDomain}}",
  "cniEnabled": {{.CNIEnabled}},
  "identityContext":{
    "clockSkewAllowance": "{{.Identity.Issuer.ClockSkewAllowance}}",
    "issuanceLifeTime": "{{.Identity.Issuer.IssuanceLifeTime}}",
    "trustAnchorsPem": "{{.Identity.Issuer.CrtPEM}}",
    "trustDomain": "{{.TrustDomain}}"
  },
  "linkerdNamespace": "{{.Namespace}}",
  "omitWebhookSideEffects": {{.OmitWebhookSideEffects}},
  "version": "{{.LinkerdVersion}}"
}
{{- end -}}

{{- define "linkerd.configs.proxy" -}}
{
  "adminPort":{
    "port": {{.Proxy.Ports.Admin}}
  },
  "controlPort":{
    "port": {{.Proxy.Ports.Control}}
  },
  "disableExternalProfiles": {{not .Proxy.EnableExternalProfile}},
  "ignoreInboundPorts": {{splitList "," .ProxyInit.IgnoreInboundPorts}},
  "ignoreOutboundPorts": {{splitList "," .ProxyInit.IgnoreOutboundPorts}},
  "inboundPort":{
    "port": {{.Proxy.Ports.Inbound}}
  },
  "logLevel":{
    "level": "{{.Proxy.LogLevel}}"
  },
  "outboundPort":{
    "port": {{.Proxy.Ports.Outbound}}
  },
  "proxyImage":{
    "imageName":"{{.Proxy.Image.Name}}",
    "pullPolicy":"{{.Proxy.Image.PullPolicy}}"
  },
  "proxyInitImage":{
    "imageName":"{{.ProxyInit.Image.Name}}",
    "pullPolicy":"{{.ProxyInit.Image.PullPolicy}}"
  },
  "proxyInitImageVersion": "{{.ProxyInit.Image.Version}}",
  "proxyUid": {{.Proxy.UID}},
  "proxyVersion": "{{.Proxy.Image.Version}}",
  "resource":{
    "limitCpu": "{{.Proxy.Resources.CPU.Limit}}",
    "limitMemory": "{{.Proxy.Resources.Memory.Limit}}",
    "requestCpu": "{{.Proxy.Resources.CPU.Request}}",
    "requestMemory": "{{.Proxy.Resources.Memory.Request}}"
  }
}
{{- end -}}

{{- define "linkerd.configs.install" -}}
{
  "uuid":"{{ uuidv4 }}",
  "cliVersion":"{{ .LinkerdVersion }}",
  "flags":[]
}
{{- end -}}
