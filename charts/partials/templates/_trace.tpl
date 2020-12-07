{{ define "partials.linkerd.trace" -}}
{{ if .Values.controlPlaneTracing -}}
- -trace-collector=linkerd-collector.{{.Values.namespace}}.svc.{{.Values.clusterDomain}}:55678
{{ end -}}
{{- end }}

{{ define "partials.setControlPlaneTracing.proxy" -}}
{{ if .Values.controlPlaneTracing -}}
{{ $_ := set .Values.proxy.trace "collectorSvcAddr" (printf "linkerd-collector.%s.svc.%s:55678" .Values.namespace .Values.clusterDomain) -}}
{{ $_ := set .Values.proxy.trace "collectorSvcAccount" (printf "linkerd-collector.%s" .Values.namespace) -}}
{{ end -}}
{{- end }}
