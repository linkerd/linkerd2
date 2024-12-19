{{ define "partials.proxy" -}}
{{ if and .Values.proxy.nativeSidecar .Values.proxy.waitBeforeExitSeconds }}
{{ fail "proxy.nativeSidecar and waitBeforeExitSeconds cannot be used simultaneously" }}
{{- end }}
{{- if not (has .Values.proxy.logHTTPHeaders (list "insecure" "off" "")) }}
{{- fail "logHTTPHeaders must be one of: insecure | off" }}
{{- end }}
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
- name: _pod_uid
  valueFrom:
    fieldRef:
      fieldPath: metadata.uid
- name: _pod_nodeName
  valueFrom:
    fieldRef:
      fieldPath: spec.nodeName
- name: _pod_containerName
  value: &containerName linkerd-proxy
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
- name: LINKERD2_PROXY_SHUTDOWN_ENDPOINT_ENABLED
  value: {{.Values.proxy.enableShutdownEndpoint | quote}}
- name: LINKERD2_PROXY_LOG
  value: {{ printf "%s%s" .Values.proxy.logLevel (.Values.proxy.logHTTPHeaders | eq "insecure" | ternary "" ",[{headers}]=off,[{request}]=off") | quote }}
- name: LINKERD2_PROXY_LOG_FORMAT
  value: {{.Values.proxy.logFormat | quote}}
- name: LINKERD2_PROXY_DESTINATION_SVC_ADDR
  value: {{ternary "localhost.:8086" (printf "linkerd-dst-headless.%s.svc.%s.:8086" .Release.Namespace .Values.clusterDomain) (eq (toString .Values.proxy.component) "linkerd-destination")}}
- name: LINKERD2_PROXY_DESTINATION_PROFILE_NETWORKS
  value: {{.Values.clusterNetworks | quote}}
- name: LINKERD2_PROXY_POLICY_SVC_ADDR
  value: {{ternary "localhost.:8090" (printf "linkerd-policy.%s.svc.%s.:8090" .Release.Namespace .Values.clusterDomain) (eq (toString .Values.proxy.component) "linkerd-destination")}}
- name: LINKERD2_PROXY_POLICY_WORKLOAD
  value: |
    {"ns":"$(_pod_ns)", "pod":"$(_pod_name)"}
- name: LINKERD2_PROXY_INBOUND_DEFAULT_POLICY
  value: {{.Values.proxy.defaultInboundPolicy}}
- name: LINKERD2_PROXY_POLICY_CLUSTER_NETWORKS
  value: {{.Values.clusterNetworks | quote}}
- name: LINKERD2_PROXY_CONTROL_STREAM_INITIAL_TIMEOUT
  value: {{((.Values.proxy.control).streams).initialTimeout | default "" | quote}}
- name: LINKERD2_PROXY_CONTROL_STREAM_IDLE_TIMEOUT
  value: {{((.Values.proxy.control).streams).idleTimeout | default "" | quote}}
- name: LINKERD2_PROXY_CONTROL_STREAM_LIFETIME
  value: {{((.Values.proxy.control).streams).lifetime | default "" | quote}}
{{ if .Values.proxy.inboundConnectTimeout -}}
- name: LINKERD2_PROXY_INBOUND_CONNECT_TIMEOUT
  value: {{.Values.proxy.inboundConnectTimeout | quote}}
{{ end -}}
{{ if .Values.proxy.outboundConnectTimeout -}}
- name: LINKERD2_PROXY_OUTBOUND_CONNECT_TIMEOUT
  value: {{.Values.proxy.outboundConnectTimeout | quote}}
{{ end -}}
{{ if .Values.proxy.outboundDiscoveryCacheUnusedTimeout -}}
- name: LINKERD2_PROXY_OUTBOUND_DISCOVERY_IDLE_TIMEOUT
  value: {{.Values.proxy.outboundDiscoveryCacheUnusedTimeout | quote}}
{{ end -}}
{{ if .Values.proxy.inboundDiscoveryCacheUnusedTimeout -}}
- name: LINKERD2_PROXY_INBOUND_DISCOVERY_IDLE_TIMEOUT
  value: {{.Values.proxy.inboundDiscoveryCacheUnusedTimeout | quote}}
{{ end -}}
{{ if .Values.proxy.disableOutboundProtocolDetectTimeout -}}
- name: LINKERD2_PROXY_OUTBOUND_DETECT_TIMEOUT
  value: "365d"
{{ end -}}
{{ if .Values.proxy.disableInboundProtocolDetectTimeout -}}
- name: LINKERD2_PROXY_INBOUND_DETECT_TIMEOUT
  value: "365d"
{{ end -}}
- name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
  value: "{{ if .Values.disableIPv6 }}0.0.0.0{{ else }}[::]{{ end }}:{{.Values.proxy.ports.control}}"
- name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
  value: "{{ if .Values.disableIPv6 }}0.0.0.0{{ else }}[::]{{ end }}:{{.Values.proxy.ports.admin}}"
{{- /* Deprecated, superseded by LINKERD2_PROXY_OUTBOUND_LISTEN_ADDRS since proxy's v2.228.0 (deployed since edge-24.4.5) */}}
- name: LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR
  value: "127.0.0.1:{{.Values.proxy.ports.outbound}}"
- name: LINKERD2_PROXY_OUTBOUND_LISTEN_ADDRS
  value: "127.0.0.1:{{.Values.proxy.ports.outbound}}{{ if not .Values.disableIPv6}},[::1]:{{.Values.proxy.ports.outbound}}{{ end }}"
- name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
  value: "{{ if .Values.disableIPv6 }}0.0.0.0{{ else }}[::]{{ end }}:{{.Values.proxy.ports.inbound}}"
- name: LINKERD2_PROXY_INBOUND_IPS
  valueFrom:
    fieldRef:
      fieldPath: status.podIPs
- name: LINKERD2_PROXY_INBOUND_PORTS
  value: {{ .Values.proxy.podInboundPorts | quote }}
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
- name: LINKERD2_PROXY_INBOUND_ACCEPT_USER_TIMEOUT
  value: 30s
- name: LINKERD2_PROXY_OUTBOUND_CONNECT_USER_TIMEOUT
  value: 30s
{{- /* Configure inbound and outbound parameters, e.g. for HTTP/2 servers. */}}
{{ range $proxyK, $proxyV := (dict "inbound" .Values.proxy.inbound "outbound" .Values.proxy.outbound) -}}
{{   range $scopeK, $scopeV := $proxyV -}}
{{     range $protoK, $protoV := $scopeV -}}
{{       range $paramK, $paramV := $protoV -}}
- name: LINKERD2_PROXY_{{snakecase $proxyK | upper}}_{{snakecase $scopeK | upper}}_{{snakecase $protoK | upper}}_{{snakecase $paramK | upper}}
  value: {{ quote $paramV }}
{{       end -}}
{{     end -}}
{{   end -}}
{{ end -}}
{{ if .Values.proxy.opaquePorts -}}
- name: LINKERD2_PROXY_INBOUND_PORTS_DISABLE_PROTOCOL_DETECTION
  value: {{.Values.proxy.opaquePorts | quote}}
{{ end -}}
- name: LINKERD2_PROXY_DESTINATION_CONTEXT
  value: |
    {"ns":"$(_pod_ns)", "nodeName":"$(_pod_nodeName)", "pod":"$(_pod_name)"}
- name: _pod_sa
  valueFrom:
    fieldRef:
      fieldPath: spec.serviceAccountName
- name: _l5d_ns
  value: {{.Release.Namespace}}
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
{{- if .Values.identity.serviceAccountTokenProjection }}
  value: /var/run/secrets/tokens/linkerd-identity-token
{{ else }}
  value: /var/run/secrets/kubernetes.io/serviceaccount/token
{{ end -}}
- name: LINKERD2_PROXY_IDENTITY_SVC_ADDR
  value: {{ternary "localhost.:8080" (printf "linkerd-identity-headless.%s.svc.%s.:8080" .Release.Namespace .Values.clusterDomain) (eq (toString .Values.proxy.component) "linkerd-identity")}}
- name: LINKERD2_PROXY_IDENTITY_LOCAL_NAME
  value: $(_pod_sa).$(_pod_ns).serviceaccount.identity.{{.Release.Namespace}}.{{$trustDomain}}
- name: LINKERD2_PROXY_IDENTITY_SVC_NAME
  value: linkerd-identity.{{.Release.Namespace}}.serviceaccount.identity.{{.Release.Namespace}}.{{$trustDomain}}
- name: LINKERD2_PROXY_DESTINATION_SVC_NAME
  value: linkerd-destination.{{.Release.Namespace}}.serviceaccount.identity.{{.Release.Namespace}}.{{$trustDomain}}
- name: LINKERD2_PROXY_POLICY_SVC_NAME
  value: linkerd-destination.{{.Release.Namespace}}.serviceaccount.identity.{{.Release.Namespace}}.{{$trustDomain}}
{{ if .Values.proxy.accessLog -}}
- name: LINKERD2_PROXY_ACCESS_LOG
  value: {{.Values.proxy.accessLog | quote}}
{{ end -}}
{{ if .Values.proxy.shutdownGracePeriod -}}
- name: LINKERD2_PROXY_SHUTDOWN_GRACE_PERIOD
  value: {{.Values.proxy.shutdownGracePeriod | quote}}
{{ end -}}
{{ if .Values.proxy.additionalEnv -}}
{{ toYaml .Values.proxy.additionalEnv }}
{{ end -}}
{{ if .Values.proxy.experimentalEnv -}}
{{ toYaml .Values.proxy.experimentalEnv }}
{{ end -}}
image: {{.Values.proxy.image.name}}:{{.Values.proxy.image.version | default .Values.linkerdVersion}}
imagePullPolicy: {{.Values.proxy.image.pullPolicy | default .Values.imagePullPolicy}}
livenessProbe:
  httpGet:
    path: /live
    port: {{.Values.proxy.ports.admin}}
  initialDelaySeconds: {{.Values.proxy.livenessProbe.initialDelaySeconds }}
  timeoutSeconds: {{.Values.proxy.livenessProbe.timeoutSeconds }}
name: *containerName
ports:
- containerPort: {{.Values.proxy.ports.inbound}}
  name: linkerd-proxy
- containerPort: {{.Values.proxy.ports.admin}}
  name: linkerd-admin
readinessProbe:
  httpGet:
    path: /ready
    port: {{.Values.proxy.ports.admin}}
  initialDelaySeconds: {{.Values.proxy.readinessProbe.initialDelaySeconds }}
  timeoutSeconds: {{.Values.proxy.readinessProbe.timeoutSeconds }}
{{- if and .Values.proxy.nativeSidecar .Values.proxy.await }}
startupProbe:
  httpGet:
    path: /ready
    port: {{.Values.proxy.ports.admin}}
  initialDelaySeconds: {{.Values.proxy.startupProbe.initialDelaySeconds}}
  periodSeconds: {{.Values.proxy.startupProbe.periodSeconds}}
  failureThreshold: {{.Values.proxy.startupProbe.failureThreshold}}
{{- end }}
{{- if .Values.proxy.resources }}
{{ include "partials.resources" .Values.proxy.resources }}
{{- end }}
securityContext:
  allowPrivilegeEscalation: false
  {{- if .Values.proxy.capabilities -}}
  {{- include "partials.proxy.capabilities" . | nindent 2 -}}
  {{- end }}
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  runAsUser: {{.Values.proxy.uid}}
{{- if ge (int .Values.proxy.gid) 0 }}
  runAsGroup: {{.Values.proxy.gid}}
{{- end }}
  seccompProfile:
    type: RuntimeDefault
terminationMessagePolicy: FallbackToLogsOnError
{{- if and (not .Values.proxy.nativeSidecar) (or .Values.proxy.await .Values.proxy.waitBeforeExitSeconds) }}
lifecycle:
{{- if .Values.proxy.await }}
  postStart:
    exec:
      command:
        - /usr/lib/linkerd/linkerd-await
        - --timeout=2m
        - --port={{.Values.proxy.ports.admin}}
{{- end }}
{{- if .Values.proxy.waitBeforeExitSeconds }}
  preStop:
    exec:
      command:
        - /bin/sleep
        - {{.Values.proxy.waitBeforeExitSeconds | quote}}
{{- end }}
{{- end }}
volumeMounts:
- mountPath: /var/run/linkerd/identity/end-entity
  name: linkerd-identity-end-entity
{{- if .Values.identity.serviceAccountTokenProjection }}
- mountPath: /var/run/secrets/tokens
  name: linkerd-identity-token
{{- end }}
{{- if .Values.proxy.saMountPath }}
- mountPath: {{.Values.proxy.saMountPath.mountPath}}
  name: {{.Values.proxy.saMountPath.name}}
  readOnly: {{.Values.proxy.saMountPath.readOnly}}
{{- end -}}
{{- if .Values.proxy.nativeSidecar }}
restartPolicy: Always
{{- end -}}
{{- end }}
