
{{- define "partials.network-validator" -}}
name: linkerd-network-validator
image: {{.Values.proxyInit.image.name}}:{{.Values.proxyInit.image.version}}
imagePullPolicy: {{.Values.proxyInit.image.pullPolicy | default .Values.imagePullPolicy}}
env:
  {{- if .Values.proxyInit.logLevel -}}
  - name: LINKERD_NETWORK_VALIDATOR_LOG_LEVEL
    value: {{ .Values.networkValidator.logLevel }}
  {{- end -}}
  {{- if .Values.networkValidator.LogFormat -}}
    - name: LINKERD_NETWORK_VALIDATOR_LOG_FORMAT
      value: {{.Values.networkValidator.logFormat}}
  {{- end -}}
command: /usr/lib/linkerd/linkerd2-network-validator
args:
  - name: connectAddr
    value: {{.Values.networkValidator.connectAddr}}
  - name: listenAddr
    value: {{.Values.networkValidator.listenAddr}}
  - name: timeout
    value: {{.Values.networkValidator.timeout}}

{{- end -}}
