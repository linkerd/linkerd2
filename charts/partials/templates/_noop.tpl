{{- define "partials.noop" -}}
args:
- -v
image: gcr.io/google_containers/pause:3.2
name: noop
resources:
  limits:
    cpu: "50m"
    memory: "25Mi"
  requests:
    cpu: "50m"
    memory: "25Mi"
securityContext:
  runAsUser: {{ .Values.proxyInit.runAsUser | int | eq 0 | ternary 65534 .Values.proxyInit.runAsUser }}
{{- end -}}
