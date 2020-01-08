{{- define "linkerd.configs.global" -}}
{
  "linkerdNamespace": "{{.Values.global.namespace}}",
  "cniEnabled": false,
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
    {{- $ports := splitList "," .Values.global.proxyInit.ignoreInboundPorts -}}
    {{- if gt (len $ports) 1}}
    {{- $last := sub (len $ports) 1 -}}
    {{- range $i,$port := $ports -}}
    {"port":{{$port}}}{{ternary "," "" (ne $i $last)}}
    {{- end -}}
    {{- end -}}
  ],
  "ignoreOutboundPorts":[
    {{- $ports := splitList "," .Values.global.proxyInit.ignoreOutboundPorts -}}
    {{- if gt (len $ports) 1}}
    {{- $last := sub (len $ports) 1 -}}
    {{- range $i,$port := $ports -}}
    {"port":{{$port}}}{{ternary "," "" (ne $i $last)}}
    {{- end -}}
    {{- end -}}
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
  "proxyInitImageVersion": "{{.Values.global.proxyInit.image.version}}"
}
{{- end -}}

{{- define "linkerd.configs.install" -}}
{
  "cliVersion":"{{ .Values.global.linkerdVersion }}",
  "flags":[]
}
{{- end -}}
