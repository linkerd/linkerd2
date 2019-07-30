{{ define "partials.proxy" -}}
- env:
  - name: LINKERD2_PROXY_LOG
    value: {{.Proxy.LogLevel}}
  - name: LINKERD2_PROXY_DESTINATION_SVC_ADDR
    value: {{ternary "localhost.:8086" (printf "linkerd-destination.%s.svc.%s:8086" .Namespace .ClusterDomain) (eq .Proxy.Component "linkerd-controller")}}
  - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
    value: 0.0.0.0:{{.Proxy.Port.Control}}
  - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
    value: 0.0.0.0:{{.Proxy.Port.Admin}}
  - name: LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR
    value: 127.0.0.1:{{.Proxy.Port.Outbound}}
  - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
    value: 0.0.0.0:{{.Proxy.Port.Inbound}}
  - name: LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES
    {{- $internalProfileSuffix := printf "svc.%s." .ClusterDomain }}
    value: {{ternary "." $internalProfileSuffix .Proxy.EnableExternalProfile}}
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
  {{ if eq .Proxy.Component "linkerd-prometheus" -}}
  - name: LINKERD2_PROXY_OUTBOUND_ROUTER_CAPACITY
    value: "10000"
  {{ end -}}
  {{ if .DisableIdentity -}}
  - name: LINKERD2_PROXY_IDENTITY_DISABLED
    value: disabled
  {{ else -}}
  - name: LINKERD2_PROXY_IDENTITY_DIR
    value: /var/run/linkerd/identity/end-entity
  - name: LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS
    value: |
    {{- .Identity.Issuer.CrtPEM | trim | nindent 6 }}
  - name: LINKERD2_PROXY_IDENTITY_TOKEN_FILE
    value: /var/run/secrets/kubernetes.io/serviceaccount/token
  - name: LINKERD2_PROXY_IDENTITY_SVC_ADDR
    {{- $identitySvcAddr := printf "linkerd-identity.%s.svc.%s:8080" .Namespace .ClusterDomain }}
    value: {{ternary "localhost.:8080" $identitySvcAddr (eq .Proxy.Component "linkerd-identity")}}
  - name: _pod_sa
    valueFrom:
      fieldRef:
        fieldPath: spec.serviceAccountName
  - name: _l5d_ns
    value: {{.Namespace}}
  - name: _l5d_trustdomain
    value: {{.Identity.TrustDomain}}
  - name: LINKERD2_PROXY_IDENTITY_LOCAL_NAME
    value: $(_pod_sa).$(_pod_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
  - name: LINKERD2_PROXY_IDENTITY_SVC_NAME
    value: linkerd-identity.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
  - name: LINKERD2_PROXY_DESTINATION_SVC_NAME
    value: linkerd-controller.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
  {{ end -}}
  {{ if .Proxy.DisableTap -}}
  - name: LINKERD2_PROXY_TAP_DISABLED
    value: "true"
  {{ else -}}
  - name: LINKERD2_PROXY_TAP_SVC_NAME
    value: linkerd-tap.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
  {{ end -}}
  image: {{.Proxy.Image.Name}}:{{.Proxy.Image.Version}}
  imagePullPolicy: {{.Proxy.Image.PullPolicy}}
  livenessProbe:
    httpGet:
      path: /metrics
      port: {{.Proxy.Port.Admin}}
    initialDelaySeconds: 10
  name: linkerd-proxy
  ports:
  - containerPort: {{.Proxy.Port.Inbound}}
    name: linkerd-proxy
  - containerPort: {{.Proxy.Port.Admin}}
    name: linkerd-admin
  readinessProbe:
    httpGet:
      path: /ready
      port: {{.Proxy.Port.Admin}}
    initialDelaySeconds: 2
  {{- if eq .HighAvailability true -}}
  {{- include "partials.resources" .Proxy.ResourceRequirements | nindent 2 -}}
  {{- end }}
  securityContext:
    allowPrivilegeEscalation: false
    {{- if .Proxy.Capabilities -}}
    {{- include "partials.proxy.capabilities" .Proxy | nindent 4 -}}
    {{- end }}
    readOnlyRootFilesystem: true
    runAsUser: {{.Proxy.UID}}
  terminationMessagePolicy: FallbackToLogsOnError
  volumeMounts:
  - mountPath: /var/run/linkerd/identity/end-entity
    name: linkerd-identity-end-entity
  {{- if .Proxy.MountPaths }}
  {{- toYaml .Proxy.MountPaths | trim | nindent 2 -}}
  {{- end }}
{{ end -}}
