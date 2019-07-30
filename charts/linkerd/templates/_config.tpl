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
    "port": {{.Proxy.Port.Admin}}
  },
  "controlPort":{
    "port": {{.Proxy.Port.Control}}
  },
  "disableExternalProfiles": {{not .Proxy.EnableExternalProfile}},
  "ignoreInboundPorts": {{splitList "," .ProxyInit.IgnoreInboundPorts}},
  "ignoreOutboundPorts": {{splitList "," .ProxyInit.IgnoreOutboundPorts}},
  "inboundPort":{
    "port": {{.Proxy.Port.Inbound}}
  },
  "logLevel":{
    "level": "{{.Proxy.LogLevel}}"
  },
  "outboundPort":{
    "port": {{.Proxy.Port.Outbound}}
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
    "limitCpu": "{{.Proxy.ResourceRequirements.CPU.Limit}}",
    "limitMemory": "{{.Proxy.ResourceRequirements.Memory.Limit}}",
    "requestCpu": "{{.Proxy.ResourceRequirements.CPU.Request}}",
    "requestMemory": "{{.Proxy.ResourceRequirements.Memory.Request}}"
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
