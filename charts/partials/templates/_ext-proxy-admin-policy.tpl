{{- define "partials.ext-proxy-admin-policy" -}}
---
apiVersion: policy.linkerd.io/v1alpha1
kind: Server
metadata:
  namespace: {{.Values.namespace}}
  name: {{.Values.component}}-proxy-admin
  labels:
    {{- range $label, $val := .Values.labels }}
    {{ $label }}: {{ $val }}
    {{- end }}
  annotations:
    {{ include "partials.annotations.created-by" . }}
spec:
  podSelector:
    matchLabels:
      {{- range $label, $val := .Values.labels }}
      {{ $label }}: {{ $val }}
      {{- end }}
  port: linkerd-admin
  proxyProtocol: HTTP/1
---
apiVersion: policy.linkerd.io/v1alpha1
kind: ServerAuthorization
metadata:
  namespace: {{.Values.namespace}}
  name: {{.Values.component}}-proxy-admin
  labels:
    {{- range $label, $val := .Values.labels }}
    {{ $label }}: {{ $val }}
    {{- end }}
  annotations:
    {{ include "partials.annotations.created-by" . }}
spec:
  server:
    name: {{.Values.component}}-proxy-admin
  client:
    unauthenticated: true
{{- end -}}
