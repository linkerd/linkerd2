{{ define "partials.proxy" -}}
- env:
  - name: LINKERD2_PROXY_LOG
    value: {{.LogLevel}}
  - name: LINKERD2_PROXY_DESTINATION_SVC_ADDR
    value: {{ternary "localhost.:8086" (printf "linkerd-destination.%s.svc.%s:8086" .ControlPlaneNamespace .ClusterDomain) (eq .Component "linkerd-controller")}}
  - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
    value: 0.0.0.0:{{.Port.Control}}
  - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
    value: 0.0.0.0:{{.Port.Admin}}
  - name: LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR
    value: 127.0.0.1:{{.Port.Outbound}}
  - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
    value: 0.0.0.0:{{.Port.Inbound}}
  - name: LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES
    {{- $internalProfileSuffix := printf "svc.%s." .ClusterDomain }}
    value: {{ternary "." $internalProfileSuffix .EnableExternalProfile}}
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
  {{ if eq .Component "linkerd-prometheus" -}}
  - name: LINKERD2_PROXY_OUTBOUND_ROUTER_CAPACITY
    value: "10000"
  {{ end -}}
  - name: LINKERD2_PROXY_IDENTITY_DIR
    value: /var/run/linkerd/identity/end-entity
  - name: LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS
    value: |
    {{- .Identity.TrustAnchors | trim | nindent 6 }}
  - name: LINKERD2_PROXY_IDENTITY_TOKEN_FILE
    value: /var/run/secrets/kubernetes.io/serviceaccount/token
  - name: LINKERD2_PROXY_IDENTITY_SVC_ADDR
    {{- $identitySvcAddr := printf "linkerd-identity.%s.svc.%s:8080" .ControlPlaneNamespace .ClusterDomain }}
    value: {{ternary "localhost.:8080" $identitySvcAddr (eq .Component "linkerd-identity")}}
  - name: _pod_sa
    valueFrom:
      fieldRef:
        fieldPath: spec.serviceAccountName
  - name: _l5d_ns
    value: {{.ControlPlaneNamespace}}
  - name: _l5d_trustdomain
    value: {{.Identity.TrustDomain}}
  - name: LINKERD2_PROXY_IDENTITY_LOCAL_NAME
    value: $(_pod_sa).$(_pod_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
  - name: LINKERD2_PROXY_IDENTITY_SVC_NAME
    value: linkerd-identity.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
  - name: LINKERD2_PROXY_DESTINATION_SVC_NAME
    value: linkerd-controller.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
  image: {{.Image.Name}}:{{.Image.Version}}
  imagePullPolicy: {{.Image.PullPolicy}}
  livenessProbe:
    httpGet:
      path: /metrics
      port: {{.Port.Admin}}
    initialDelaySeconds: 10
  name: linkerd-proxy
  ports:
  - containerPort: {{.Port.Inbound}}
    name: linkerd-proxy
  - containerPort: {{.Port.Admin}}
    name: linkerd-admin
  readinessProbe:
    httpGet:
      path: /ready
      port: {{.Port.Admin}}
    initialDelaySeconds: 2
  {{- if eq .HighAvailability true -}}
  {{- include "partials.resources" .ResourceRequirements | nindent 2 -}}
  {{- end }}
  securityContext:
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    runAsUser: {{.UID}}
  terminationMessagePolicy: FallbackToLogsOnError
  volumeMounts:
  - mountPath: /var/run/linkerd/identity/end-entity
    name: linkerd-identity-end-entity
{{ end -}}
