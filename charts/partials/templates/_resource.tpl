{{- define "partials.proxy.resource" -}}
limits:
  cpu: "1"
  memory: 250Mi
requests:
  cpu: 100m
  memory: 20Mi
{{- end -}}

{{- define "partials.proxy-init.resource" -}}
limits:
  cpu: 100m
  memory: 50Mi
requests:
  cpu: 10m
  memory: 10Mi
{{- end -}}
