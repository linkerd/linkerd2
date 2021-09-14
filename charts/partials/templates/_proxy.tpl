{{ define "partials.proxy" -}}
{{- $trustDomain := (.Values.identityTrustDomain | default .Values.clusterDomain) -}}
env:
- name: _pod_name
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: _pod_ns
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
- name: _pod_nodeName
  valueFrom:
    fieldRef:
      fieldPath: spec.nodeName
{{- if .Values.proxy.cores }}
- name: LINKERD2_PROXY_CORES
  value: {{.Values.proxy.cores | quote}}
{{- end }}
{{ if .Values.proxy.requireIdentityOnInboundPorts -}}
- name: LINKERD2_PROXY_INBOUND_PORTS_REQUIRE_IDENTITY
  value: {{.Values.proxy.requireIdentityOnInboundPorts | quote}}
{{ end -}}
{{ if .Values.proxy.requireTLSOnInboundPorts -}}
- name: LINKERD2_PROXY_INBOUND_PORTS_REQUIRE_TLS
  value: {{.Values.proxy.requireTLSOnInboundPorts | quote}}
{{ end -}}
- name: LINKERD2_PROXY_LOG
  value: {{.Values.proxy.logLevel | quote}}
- name: LINKERD2_PROXY_LOG_FORMAT
  value: {{.Values.proxy.logFormat | quote}}
- name: LINKERD2_PROXY_DESTINATION_SVC_ADDR
  value: {{ternary "localhost.:8086" (printf "linkerd-dst-headless.%s.svc.%s.:8086" .Values.namespace .Values.clusterDomain) (eq (toString .Values.proxy.component) "linkerd-destination")}}
- name: LINKERD2_PROXY_DESTINATION_PROFILE_NETWORKS
  value: {{.Values.clusterNetworks | quote}}
- name: LINKERD2_PROXY_POLICY_SVC_ADDR
  value: {{ternary "localhost.:8090" (printf "linkerd-policy.%s.svc.%s.:8090" .Values.namespace .Values.clusterDomain) (eq (toString .Values.proxy.component) "linkerd-destination")}}
- name: LINKERD2_PROXY_POLICY_WORKLOAD
  value: "$(_pod_ns):$(_pod_name)"
- name: LINKERD2_PROXY_INBOUND_DEFAULT_POLICY
  value: {{.Values.proxy.defaultInboundPolicy | default .Values.policyController.defaultAllowPolicy}}
- name: LINKERD2_PROXY_POLICY_CLUSTER_NETWORKS
  value: {{.Values.clusterNetworks | quote}}
{{ if .Values.proxy.inboundConnectTimeout -}}
- name: LINKERD2_PROXY_INBOUND_CONNECT_TIMEOUT
  value: {{.Values.proxy.inboundConnectTimeout | quote}}
{{ end -}}
{{ if .Values.proxy.outboundConnectTimeout -}}
- name: LINKERD2_PROXY_OUTBOUND_CONNECT_TIMEOUT
  value: {{.Values.proxy.outboundConnectTimeout | quote}}
{{ end -}}
- name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
  value: 0.0.0.0:{{.Values.proxy.ports.control}}
- name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
  value: 0.0.0.0:{{.Values.proxy.ports.admin}}
- name: LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR
  value: 127.0.0.1:{{.Values.proxy.ports.outbound}}
- name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
  value: 0.0.0.0:{{.Values.proxy.ports.inbound}}
- name: LINKERD2_PROXY_INBOUND_IPS
  valueFrom:
    fieldRef:
      fieldPath: status.podIPs
{{ if .Values.proxy.podInboundPorts -}}
- name: LINKERD2_PROXY_INBOUND_PORTS
  value: {{ .Values.proxy.podInboundPorts | quote }}
{{ end -}}
{{ if .Values.proxy.isGateway -}}
- name: LINKERD2_PROXY_INBOUND_GATEWAY_SUFFIXES
  value: {{printf "svc.%s." .Values.clusterDomain}}
{{ end -}}
{{ if .Values.proxy.isIngress -}}
- name: LINKERD2_PROXY_INGRESS_MODE
  value: "true"
{{ end -}}
- name: LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES
  {{- $internalDomain := printf "svc.%s." .Values.clusterDomain }}
  value: {{ternary "." $internalDomain .Values.proxy.enableExternalProfiles}}
- name: LINKERD2_PROXY_INBOUND_ACCEPT_KEEPALIVE
  value: 10000ms
- name: LINKERD2_PROXY_OUTBOUND_CONNECT_KEEPALIVE
  value: 10000ms
{{ if .Values.proxy.opaquePorts -}}
- name: LINKERD2_PROXY_INBOUND_PORTS_DISABLE_PROTOCOL_DETECTION
  value: {{.Values.proxy.opaquePorts | quote}}
{{ end -}}
- name: LINKERD2_PROXY_DESTINATION_CONTEXT
  value: |
    {"ns":"$(_pod_ns)", "nodeName":"$(_pod_nodeName)"}
{{ if .Values.proxy.disableIdentity -}}
- name: LINKERD2_PROXY_IDENTITY_DISABLED
  value: disabled
{{ else -}}
- name: _pod_sa
  valueFrom:
    fieldRef:
      fieldPath: spec.serviceAccountName
- name: _l5d_ns
  value: {{.Values.namespace}}
- name: _l5d_trustdomain
  value: {{$trustDomain}}
- name: LINKERD2_PROXY_IDENTITY_DIR
  value: /var/run/linkerd/identity/end-entity
- name: LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS
{{- /*
Pods in the `linkerd` namespace are not injected by the proxy injector and instead obtain
the trust anchor bundle from the `linkerd-identity-trust-roots` configmap. This should not
be used in other contexts.
*/}}
{{- if .Values.proxy.loadTrustBundleFromConfigMap }}
  valueFrom:
    configMapKeyRef:
      name: linkerd-identity-trust-roots
      key: ca-bundle.crt
{{ else }}
  value: |
    {{- required "Please provide the identity trust anchors" .Values.identityTrustAnchorsPEM | trim | nindent 4 }}
{{ end -}}
- name: LINKERD2_PROXY_IDENTITY_TOKEN_FILE
  value: /var/run/secrets/kubernetes.io/serviceaccount/token
- name: LINKERD2_PROXY_IDENTITY_SVC_ADDR
  value: {{ternary "localhost.:8080" (printf "linkerd-identity-headless.%s.svc.%s.:8080" .Values.namespace .Values.clusterDomain) (eq (toString .Values.proxy.component) "linkerd-identity")}}
- name: LINKERD2_PROXY_IDENTITY_LOCAL_NAME
  value: $(_pod_sa).$(_pod_ns).serviceaccount.identity.{{.Values.namespace}}.{{$trustDomain}}
- name: LINKERD2_PROXY_IDENTITY_SVC_NAME
  value: linkerd-identity.{{.Values.namespace}}.serviceaccount.identity.{{.Values.namespace}}.{{$trustDomain}}
- name: LINKERD2_PROXY_DESTINATION_SVC_NAME
  value: linkerd-destination.{{.Values.namespace}}.serviceaccount.identity.{{.Values.namespace}}.{{$trustDomain}}
- name: LINKERD2_PROXY_POLICY_SVC_NAME
  value: linkerd-destination.{{.Values.namespace}}.serviceaccount.identity.{{.Values.namespace}}.{{$trustDomain}}
{{ end -}}
image: {{.Values.proxy.image.name}}:{{.Values.proxy.image.version | default .Values.linkerdVersion}}
imagePullPolicy: {{.Values.proxy.image.pullPolicy | default .Values.imagePullPolicy}}
livenessProbe:
  httpGet:
    path: /live
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
{{- if or (.Values.proxy.await) (.Values.proxy.waitBeforeExitSeconds) }}
lifecycle:
{{- if .Values.proxy.await }}
  postStart:
    exec:
      command:
      - /usr/lib/linkerd/linkerd-await
{{- end }}
{{- if .Values.proxy.waitBeforeExitSeconds }}
  preStop:
    exec:
      command:
        - /bin/sleep
        - {{.Values.proxy.waitBeforeExitSeconds | quote}}
{{- end }}
{{- end }}
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
