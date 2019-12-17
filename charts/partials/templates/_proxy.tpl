{{ define "partials.proxy" -}}
env:
- name: LINKERD2_PROXY_LOG
  value: {{.Values.proxy.logLevel}}
- name: LINKERD2_PROXY_DESTINATION_SVC_ADDR
  value: {{ternary "localhost.:8086" (printf "linkerd-dst.%s.svc.%s:8086" .Values.namespace .Values.clusterDomain) (eq .Values.proxy.component "linkerd-destination")}}
- name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
  value: 0.0.0.0:{{.Values.proxy.ports.control}}
- name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
  value: 0.0.0.0:{{.Values.proxy.ports.admin}}
- name: LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR
  value: 127.0.0.1:{{.Values.proxy.ports.outbound}}
- name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
  value: 0.0.0.0:{{.Values.proxy.ports.inbound}}
- name: LINKERD2_PROXY_DESTINATION_GET_SUFFIXES
  {{- $internalProfileSuffix := printf "svc.%s." .Values.clusterDomain }}
  value: {{ternary "." $internalProfileSuffix .Values.proxy.enableExternalProfiles}}
- name: LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES
  {{- $internalProfileSuffix := printf "svc.%s." .Values.clusterDomain }}
  value: {{ternary "." $internalProfileSuffix .Values.proxy.enableExternalProfiles}}
- name: LINKERD2_PROXY_INBOUND_ACCEPT_KEEPALIVE
  value: 10000ms
- name: LINKERD2_PROXY_OUTBOUND_CONNECT_KEEPALIVE
  value: 10000ms
- name: _pod_ns
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
- name: LINKERD2_PROXY_DESTINATION_CONTEXT
  value: ns:$(_pod_ns)
{{ if eq .Values.proxy.component "linkerd-prometheus" -}}
- name: LINKERD2_PROXY_OUTBOUND_ROUTER_CAPACITY
  value: "10000"
{{ end -}}
{{ if .Values.proxy.disableIdentity -}}
- name: LINKERD2_PROXY_IDENTITY_DISABLED
  value: disabled
{{ else -}}
- name: LINKERD2_PROXY_IDENTITY_DIR
  value: /var/run/linkerd/identity/end-entity
- name: LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS
  value: |
  {{- required "Please provide the identity trust anchors" .Values.identity.trustAnchorsPEM | trim | nindent 4 }}
- name: LINKERD2_PROXY_IDENTITY_TOKEN_FILE
  value: /var/run/secrets/kubernetes.io/serviceaccount/token
- name: LINKERD2_PROXY_IDENTITY_SVC_ADDR
  {{- $identitySvcAddr := printf "linkerd-identity.%s.svc.%s:8080" .Values.namespace .Values.clusterDomain }}
  value: {{ternary "localhost.:8080" $identitySvcAddr (eq .Values.proxy.component "linkerd-identity")}}
- name: _pod_sa
  valueFrom:
    fieldRef:
      fieldPath: spec.serviceAccountName
- name: _l5d_ns
  value: {{.Values.namespace}}
- name: _l5d_trustdomain
  value: {{.Values.identity.trustDomain}}
- name: LINKERD2_PROXY_IDENTITY_LOCAL_NAME
  value: $(_pod_sa).$(_pod_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
- name: LINKERD2_PROXY_IDENTITY_SVC_NAME
  value: linkerd-identity.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
- name: LINKERD2_PROXY_DESTINATION_SVC_NAME
  value: linkerd-destination.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
{{ end -}}
{{ if .Values.proxy.disableTap -}}
- name: LINKERD2_PROXY_TAP_DISABLED
  value: "true"
{{ else if not .Values.proxy.disableIdentity -}}
- name: LINKERD2_PROXY_TAP_SVC_NAME
  value: linkerd-tap.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
{{ end -}}
{{ if .Values.controlPlaneTracing -}}
- name: LINKERD2_PROXY_TRACE_COLLECTOR_SVC_ADDR
  value: linkerd-collector.{{.Values.namespace}}.svc.{{.Values.clusterDomain}}:55678
- name: LINKERD2_PROXY_TRACE_COLLECTOR_SVC_NAME
  value: linkerd-collector.{{.Values.namespace}}.serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
{{ else if .Values.proxy.trace -}}
{{ if .Values.proxy.trace.collectorSvcAddr -}}
- name: LINKERD2_PROXY_TRACE_COLLECTOR_SVC_ADDR
  value: {{ .Values.proxy.trace.collectorSvcAddr }}
- name: LINKERD2_PROXY_TRACE_COLLECTOR_SVC_NAME
  value: {{ .Values.proxy.trace.collectorSvcAccount }}.serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
{{ end -}}
{{ end -}}
image: {{.Values.proxy.image.name}}:{{.Values.proxy.image.version}}
imagePullPolicy: {{.Values.proxy.image.pullPolicy}}
livenessProbe:
  httpGet:
    path: /metrics
    port: {{.Values.proxy.ports.admin}}
  initialDelaySeconds: 10
name: linkerd-proxy
ports:
- containerPort: {{.Values.proxy.ports.inbound}}
  name: linkerd-proxy
- containerPort: {{.Values.proxy.ports.admin}}
  name: linkerd-admin
readinessProbe:
  httpGet:
    path: /ready
    port: {{.Values.proxy.ports.admin}}
  initialDelaySeconds: 2
{{- if .Values.proxy.resources }}
{{ include "partials.resources" .Values.proxy.resources }}
{{- end }}
securityContext:
  allowPrivilegeEscalation: false
  {{- if .Values.proxy.capabilities -}}
  {{- include "partials.proxy.capabilities" . | nindent 2 -}}
  {{- end }}
  readOnlyRootFilesystem: true
  runAsUser: {{.Values.proxy.uid}}
terminationMessagePolicy: FallbackToLogsOnError
{{- if or (not .Values.proxy.disableIdentity) (.Values.proxy.saMountPath) }}
volumeMounts:
{{- if not .Values.proxy.disableIdentity }}
- mountPath: /var/run/linkerd/identity/end-entity
  name: linkerd-identity-end-entity
{{- end -}}
{{- if .Values.proxy.saMountPath }}
- mountPath: {{.Values.proxy.saMountPath.mountPath}}
  name: {{.Values.proxy.saMountPath.name}}
  readOnly: {{.Values.proxy.saMountPath.readOnly}}
{{- end -}}
{{- end -}}
{{- end }}
