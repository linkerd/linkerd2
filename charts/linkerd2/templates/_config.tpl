{{- define "linkerd.configs.global" -}}
{
  "linkerdNamespace": "{{.Values.global.namespace}}",
  "cniEnabled": {{ default false .Values.cniEnabled }},
  "version": "{{.Values.global.linkerdVersion}}",
  "identityContext":{
    "trustDomain": "{{.Values.global.identityTrustDomain}}",
    "trustAnchorsPem": "{{required "Please provide the identity trust anchors" .Values.global.identityTrustAnchorsPEM | trim | replace "\n" "\\n"}}",
    "issuanceLifetime": "{{.Values.identity.issuer.issuanceLifetime}}",
    "clockSkewAllowance": "{{.Values.identity.issuer.clockSkewAllowance}}",
    "scheme": "{{.Values.identity.issuer.scheme}}"
  },
  "autoInjectContext": null,
  "omitWebhookSideEffects": {{.Values.omitWebhookSideEffects}},
  "clusterDomain": "{{.Values.global.clusterDomain}}"
}
{{- end -}}

{{- define "linkerd.configs.proxy" -}}
{
  "proxyImage":{
    "imageName":"{{.Values.global.proxy.image.name}}",
    "pullPolicy":"{{.Values.global.proxy.image.pullPolicy}}"
  },
  "proxyInitImage":{
    "imageName":"{{.Values.global.proxyInit.image.name}}",
    "pullPolicy":"{{.Values.global.proxyInit.image.pullPolicy}}"
  },
  "controlPort":{
    "port": {{.Values.global.proxy.ports.control}}
  },
  "ignoreInboundPorts":[
    {{- include "partials.splitStringListToPorts" .Values.global.proxyInit.ignoreInboundPorts -}}
  ],
  "ignoreOutboundPorts":[
    {{- include "partials.splitStringListToPorts" .Values.global.proxyInit.ignoreOutboundPorts -}}
  ],
  "inboundPort":{
    "port": {{.Values.global.proxy.ports.inbound}}
  },
  "adminPort":{
    "port": {{.Values.global.proxy.ports.admin}}
  },
  "outboundPort":{
    "port": {{.Values.global.proxy.ports.outbound}}
  },
  "resource":{
    "requestCpu": "{{.Values.global.proxy.resources.cpu.request}}",
    "limitCpu": "{{.Values.global.proxy.resources.cpu.limit}}",
    "requestMemory": "{{.Values.global.proxy.resources.memory.request}}",
    "limitMemory": "{{.Values.global.proxy.resources.memory.limit}}"
  },
  "proxyUid": {{.Values.global.proxy.uid}},
  "logLevel":{
    "level": "{{.Values.global.proxy.logLevel}}"
  },
  "disableExternalProfiles": {{not .Values.global.proxy.enableExternalProfiles}},
  "proxyVersion": "{{.Values.global.proxy.image.version}}",
  "proxyInitImageVersion": "{{.Values.global.proxyInit.image.version}}",
  "debugImage":{
    "imageName":"{{.Values.debugContainer.image.name}}",
    "pullPolicy":"{{.Values.debugContainer.image.pullPolicy}}"
  },
  "debugImageVersion": "{{.Values.debugContainer.image.version}}"
}
{{- end -}}

{{- define "linkerd.configs.install" -}}
{
  "cliVersion":"{{ .Values.global.linkerdVersion }}",
  "flags":[]
}
{{- end -}}
