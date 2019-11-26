{{ define "partials.proxy" -}}
env:
- name: LINKERD2_PROXY_LOG
  value: {{.Values.Proxy.LogLevel}}
- name: LINKERD2_PROXY_DESTINATION_SVC_ADDR
  value: {{ternary "localhost.:8086" (printf "linkerd-dst.%s.svc.%s:8086" .Values.Namespace .Values.ClusterDomain) (eq .Values.Proxy.Component "linkerd-destination")}}
- name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
  value: 0.0.0.0:{{.Values.Proxy.Ports.Control}}
- name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
  value: 0.0.0.0:{{.Values.Proxy.Ports.Admin}}
- name: LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR
  value: 127.0.0.1:{{.Values.Proxy.Ports.Outbound}}
- name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
  value: 0.0.0.0:{{.Values.Proxy.Ports.Inbound}}
- name: LINKERD2_PROXY_DESTINATION_GET_SUFFIXES
  {{- $internalProfileSuffix := printf "svc.%s." .Values.ClusterDomain }}
  value: {{ternary "." $internalProfileSuffix .Values.Proxy.EnableExternalProfiles}}
- name: LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES
  {{- $internalProfileSuffix := printf "svc.%s." .Values.ClusterDomain }}
  value: {{ternary "." $internalProfileSuffix .Values.Proxy.EnableExternalProfiles}}
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
{{ if eq .Values.Proxy.Component "linkerd-prometheus" -}}
- name: LINKERD2_PROXY_OUTBOUND_ROUTER_CAPACITY
  value: "10000"
{{ end -}}
{{ if .Values.Proxy.DisableIdentity -}}
- name: LINKERD2_PROXY_IDENTITY_DISABLED
  value: disabled
{{ else -}}
- name: LINKERD2_PROXY_IDENTITY_DIR
  value: /var/run/linkerd/identity/end-entity
- name: LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS
  value: |
  {{- required "Please provide the identity trust anchors" .Values.Identity.TrustAnchorsPEM | trim | nindent 4 }}
- name: LINKERD2_PROXY_IDENTITY_TOKEN_FILE
  value: /var/run/secrets/kubernetes.io/serviceaccount/token
- name: LINKERD2_PROXY_IDENTITY_SVC_ADDR
  {{- $identitySvcAddr := printf "linkerd-identity.%s.svc.%s:8080" .Values.Namespace .Values.ClusterDomain }}
  value: {{ternary "localhost.:8080" $identitySvcAddr (eq .Values.Proxy.Component "linkerd-identity")}}
- name: _pod_sa
  valueFrom:
    fieldRef:
      fieldPath: spec.serviceAccountName
- name: _l5d_ns
  value: {{.Values.Namespace}}
- name: _l5d_trustdomain
  value: {{.Values.Identity.TrustDomain}}
- name: LINKERD2_PROXY_IDENTITY_LOCAL_NAME
  value: $(_pod_sa).$(_pod_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
- name: LINKERD2_PROXY_IDENTITY_SVC_NAME
  value: linkerd-identity.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
- name: LINKERD2_PROXY_DESTINATION_SVC_NAME
  value: linkerd-destination.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
{{ end -}}
{{ if .Values.Proxy.DisableTap -}}
- name: LINKERD2_PROXY_TAP_DISABLED
  value: "true"
{{ else if not .Values.Proxy.DisableIdentity -}}
- name: LINKERD2_PROXY_TAP_SVC_NAME
  value: linkerd-tap.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
{{ end -}}
{{ if .Values.ControlPlaneTracing -}}
- name: LINKERD2_PROXY_TRACE_COLLECTOR_SVC_ADDR
  value: linkerd-collector.{{.Values.Namespace}}.svc.{{.Values.ClusterDomain}}:55678
- name: LINKERD2_PROXY_TRACE_COLLECTOR_SVC_NAME
  value: linkerd-collector.{{.Values.Namespace}}.serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
{{ else if .Values.Proxy.Trace -}}
{{ if .Values.Proxy.Trace.CollectorSvcAddr -}}
- name: LINKERD2_PROXY_TRACE_COLLECTOR_SVC_ADDR
  value: {{ .Values.Proxy.Trace.CollectorSvcAddr }}
- name: LINKERD2_PROXY_TRACE_COLLECTOR_SVC_NAME
  value: {{ .Values.Proxy.Trace.CollectorSvcAccount }}.serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
{{ end -}}
{{ end -}}
image: {{.Values.Proxy.Image.Name}}:{{.Values.Proxy.Image.Version}}
imagePullPolicy: {{.Values.Proxy.Image.PullPolicy}}
livenessProbe:
  httpGet:
    path: /metrics
    port: {{.Values.Proxy.Ports.Admin}}
  initialDelaySeconds: 10
name: linkerd-proxy
ports:
- containerPort: {{.Values.Proxy.Ports.Inbound}}
  name: linkerd-proxy
- containerPort: {{.Values.Proxy.Ports.Admin}}
  name: linkerd-admin
readinessProbe:
  httpGet:
    path: /ready
    port: {{.Values.Proxy.Ports.Admin}}
  initialDelaySeconds: 2
{{- if .Values.Proxy.Resources }}
{{ include "partials.resources" .Values.Proxy.Resources }}
{{- end }}
securityContext:
  allowPrivilegeEscalation: false
  {{- if .Values.Proxy.Capabilities -}}
  {{- include "partials.proxy.capabilities" .Values.Proxy | nindent 2 -}}
  {{- end }}
  readOnlyRootFilesystem: true
  runAsUser: {{.Values.Proxy.UID}}
terminationMessagePolicy: FallbackToLogsOnError
{{- if or (not .Values.Proxy.DisableIdentity) (.Values.Proxy.SAMountPath) }}
volumeMounts:
{{- if not .Values.Proxy.DisableIdentity }}
- mountPath: /var/run/linkerd/identity/end-entity
  name: linkerd-identity-end-entity
{{- end -}}
{{- if .Values.Proxy.SAMountPath }}
- mountPath: {{.Values.Proxy.SAMountPath.MountPath}}
  name: {{.Values.Proxy.SAMountPath.Name}}
  readOnly: {{.Values.Proxy.SAMountPath.ReadOnly}}
{{- end -}}
{{- end -}}
{{- end }}
