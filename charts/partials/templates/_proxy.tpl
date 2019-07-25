{{ define "partials.proxy" -}}
- env:
  - name: LINKERD2_PROXY_LOG
    value: {{.LogLevel}}
  - name: LINKERD2_PROXY_DESTINATION_SVC_ADDR
    value: localhost.:8086
  - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
    value: 0.0.0.0:{{.Port.Control}}
  - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
    value: 0.0.0.0:{{.Port.Admin}}
  - name: LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR
    value: 127.0.0.1:{{.Port.Outbound}}
  - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
    value: 0.0.0.0:{{.Port.Inbound}}
  - name: LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES
    {{- $internalProfileSuffix := printf "svc.%s" .ClusterDomain }}
    value: {{ternary "." $internalProfileSuffix .EnableExternalProfile}}
  - name: LINKERD2_PROXY_INBOUND_ACCEPT_KEEPALIVE
    value: {{.InboundAcceptKeepAlive}}
  - name: LINKERD2_PROXY_OUTBOUND_CONNECT_KEEPALIVE
    value: {{.OutboundAcceptKeepAlive}}
  - name: _pod_ns
    valueFrom:
      fieldRef:
        apiVersion: v1
        fieldPath: metadata.namespace
  - name: LINKERD2_PROXY_DESTINATION_CONTEXT
    value: ns:$(_pod_ns)
  - name: LINKERD2_PROXY_IDENTITY_DIR
    value: /var/run/linkerd/identity/end-entity
  - name: LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS
    value: |
    {{- .IdentityTrustAnchors | nindent 6 -}}
  - name: LINKERD2_PROXY_IDENTITY_TOKEN_FILE
    value: /var/run/secrets/kubernetes.io/serviceaccount/token
  - name: LINKERD2_PROXY_IDENTITY_SVC_ADDR
    value: linkerd-identity.{{.ControlPlaneNamespace}}.svc.cluster.local:8080
  - name: _pod_sa
    valueFrom:
      fieldRef:
        apiVersion: v1
        fieldPath: spec.serviceAccountName
  - name: _l5d_ns
    value: {{.ControlPlaneNamespace}}
  - name: _l5d_trustdomain
    value: {{.ClusterDomain}}
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
  name: linkerd-proxy
  ports:
  - containerPort: {{.Port.Inbound}}
    name: linkerd-proxy
    protocol: TCP
  - containerPort: {{.Port.Admin}}
    name: linkerd-admin
    protocol: TCP
  readinessProbe:
    httpGet:
      path: /ready
      port: {{.Port.Admin}}
  {{ if eq .HighAvailability true -}}
  resources:
  {{- if .ResourceRequirements -}}
  {{- toYaml .ResourceRequirements | trim | nindent 4 -}}
  {{- else -}}
  {{- include "partials.proxy.resource" .Proxy | nindent 4 -}}
  {{- end }}
  {{- end }}
  securityContext:
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    runAsUser: {{.UID}}
  volumeMounts:
  - name: linkerd-identity-end-entity
    mountPath: /var/run/linkerd/identity/end-entity
{{ end -}}
