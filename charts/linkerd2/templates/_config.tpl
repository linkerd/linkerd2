{{- define "linkerd.configs.global" -}}
{
  "linkerdNamespace": "{{.Values.namespace}}",
  "cniEnabled": false,
  "version": "{{.Values.linkerdVersion}}",
  "identityContext":{
    "trustDomain": "{{.Values.identity.trustDomain}}",
    "trustAnchorsPem": "{{required "Please provide the identity trust anchors" .Values.identity.trustAnchorsPEM | trim | replace "\n" "\\n"}}",
    "issuanceLifeTime": "{{.Values.identity.issuer.issuanceLifeTime}}",
    "clockSkewAllowance": "{{.Values.identity.issuer.clockSkewAllowance}}",
    "scheme": "{{.Values.identity.issuer.scheme}}"
  },
  "autoInjectContext": null,
  "omitWebhookSideEffects": {{.Values.omitWebhookSideEffects}},
  "clusterDomain": "{{.Values.clusterDomain}}"
}
{{- end -}}

{{- define "linkerd.configs.proxy" -}}
{
  "proxyImage":{
    "imageName":"{{.Values.proxy.image.name}}",
    "pullPolicy":"{{.Values.proxy.image.pullPolicy}}"
  },
  "proxyInitImage":{
    "imageName":"{{.Values.proxyInit.image.name}}",
    "pullPolicy":"{{.Values.proxyInit.image.pullPolicy}}"
  },
  "controlPort":{
    "port": {{.Values.proxy.ports.control}}
  },
  "ignoreInboundPorts":[
    {{- $ports := splitList "," .Values.proxyInit.ignoreInboundPorts -}}
    {{- if gt (len $ports) 1}}
    {{- $last := sub (len $ports) 1 -}}
    {{- range $i,$port := $ports -}}
    {"port":{{$port}}}{{ternary "," "" (ne $i $last)}}
    {{- end -}}
    {{- end -}}
  ],
  "ignoreOutboundPorts":[
    {{- $ports := splitList "," .Values.proxyInit.ignoreOutboundPorts -}}
    {{- if gt (len $ports) 1}}
    {{- $last := sub (len $ports) 1 -}}
    {{- range $i,$port := $ports -}}
    {"port":{{$port}}}{{ternary "," "" (ne $i $last)}}
    {{- end -}}
    {{- end -}}
  ],
  "inboundPort":{
    "port": {{.Values.proxy.ports.inbound}}
  },
  "adminPort":{
    "port": {{.Values.proxy.ports.admin}}
  },
  "outboundPort":{
    "port": {{.Values.proxy.ports.outbound}}
  },
  "resource":{
    "requestCpu": "{{.Values.proxy.resources.cpu.request}}",
    "limitCpu": "{{.Values.proxy.resources.cpu.limit}}",
    "requestMemory": "{{.Values.proxy.resources.memory.request}}",
    "limitMemory": "{{.Values.proxy.resources.memory.limit}}"
  },
  "proxyUid": {{.Values.proxy.uid}},
  "logLevel":{
    "level": "{{.Values.proxy.logLevel}}"
  },
  "disableExternalProfiles": {{not .Values.proxy.enableExternalProfiles}},
  "proxyVersion": "{{.Values.proxy.image.version}}",
  "proxyInitImageVersion": "{{.Values.proxyInit.image.version}}"
}
{{- end -}}

{{- define "linkerd.configs.install" -}}
{
  "cliVersion":"{{ .Values.linkerdVersion }}",
  "flags":[]
}
{{- end -}}
