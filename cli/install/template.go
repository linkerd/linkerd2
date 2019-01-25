package install

// Template provides the base template for the `linkerd install` command.
const Template = `{{ if not .SingleNamespace -}}
### Namespace ###
kind: Namespace
apiVersion: v1
metadata:
  name: {{.Namespace}}
  {{- if and .EnableTLS .ProxyAutoInjectEnabled }}
  labels:
    {{.ProxyAutoInjectLabel}}: disabled
  {{- end }}

{{ end -}}
### Service Account Controller ###
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-controller
  namespace: {{.Namespace}}

### Controller RBAC ###
---
kind: {{if not .SingleNamespace}}Cluster{{end}}Role
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Namespace}}-controller
  {{- if .SingleNamespace}}
  namespace: {{.Namespace}}
  {{- end}}
rules:
- apiGroups: ["extensions", "apps"]
  resources: ["daemonsets", "deployments", "replicasets"]
  verbs: ["list", "get", "watch"]
- apiGroups: [""]
  resources: ["pods", "endpoints", "services", "replicationcontrollers"{{if not .SingleNamespace}}, "namespaces"{{end}}]
  verbs: ["list", "get", "watch"]
{{- if .SingleNamespace }}
- apiGroups: [""]
  resources: ["namespaces"]
  resourceNames: ["{{.Namespace}}"]
  verbs: ["list", "get", "watch"]
{{- else }}
- apiGroups: ["linkerd.io"]
  resources: ["serviceprofiles"]
  verbs: ["list", "get", "watch"]
{{- end }}

---
kind: {{if not .SingleNamespace}}Cluster{{end}}RoleBinding
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Namespace}}-controller
  {{- if .SingleNamespace}}
  namespace: {{.Namespace}}
  {{- end}}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: {{if not .SingleNamespace}}Cluster{{end}}Role
  name: linkerd-{{.Namespace}}-controller
subjects:
- kind: ServiceAccount
  name: linkerd-controller
  namespace: {{.Namespace}}

### Service Account Prometheus ###
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-prometheus
  namespace: {{.Namespace}}

### Prometheus RBAC ###
---
kind: {{if not .SingleNamespace}}Cluster{{end}}Role
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Namespace}}-prometheus
  {{- if .SingleNamespace}}
  namespace: {{.Namespace}}
  {{- end}}
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]

---
kind: {{if not .SingleNamespace}}Cluster{{end}}RoleBinding
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Namespace}}-prometheus
  {{- if .SingleNamespace}}
  namespace: {{.Namespace}}
  {{- end}}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: {{if not .SingleNamespace}}Cluster{{end}}Role
  name: linkerd-{{.Namespace}}-prometheus
subjects:
- kind: ServiceAccount
  name: linkerd-prometheus
  namespace: {{.Namespace}}

### Controller ###
---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-controller-api
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: controller
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.ControllerComponentLabel}}: controller
  ports:
  - name: http
    port: 8085
    targetPort: 8085

---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-proxy-api
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: controller
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.ControllerComponentLabel}}: controller
  ports:
  - name: grpc
    port: {{.ProxyAPIPort}}
    targetPort: {{.ProxyAPIPort}}

---
kind: Deployment
apiVersion: extensions/v1beta1
metadata:
  name: linkerd-controller
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: controller
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  replicas: {{.ControllerReplicas}}
  template:
    metadata:
      labels:
        {{.ControllerComponentLabel}}: controller
      annotations:
        {{.CreatedByAnnotation}}: {{.CliVersion}}
    spec:
      serviceAccountName: linkerd-controller
      containers:
      - name: public-api
        ports:
        - name: http
          containerPort: 8085
        - name: admin-http
          containerPort: 9995
        image: {{.ControllerImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        args:
        - "public-api"
        - "-prometheus-url=http://linkerd-prometheus.{{.Namespace}}.svc.cluster.local:9090"
        - "-controller-namespace={{.Namespace}}"
        - "-single-namespace={{.SingleNamespace}}"
        - "-log-level={{.ControllerLogLevel}}"
        livenessProbe:
          httpGet:
            path: /ping
            port: 9995
          initialDelaySeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 9995
          failureThreshold: 7
        {{- if .EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.ControllerUID}}
      - name: proxy-api
        ports:
        - name: grpc
          containerPort: {{.ProxyAPIPort}}
        - name: admin-http
          containerPort: 9996
        image: {{.ControllerImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        args:
        - "proxy-api"
        - "-addr=:{{.ProxyAPIPort}}"
        - "-controller-namespace={{.Namespace}}"
        - "-single-namespace={{.SingleNamespace}}"
        - "-enable-tls={{.EnableTLS}}"
        - "-enable-h2-upgrade={{.EnableH2Upgrade}}"
        - "-log-level={{.ControllerLogLevel}}"
        livenessProbe:
          httpGet:
            path: /ping
            port: 9996
          initialDelaySeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 9996
          failureThreshold: 7
        {{- if .EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.ControllerUID}}
      - name: tap
        ports:
        - name: grpc
          containerPort: 8088
        - name: admin-http
          containerPort: 9998
        image: {{.ControllerImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        args:
        - "tap"
        - "-controller-namespace={{.Namespace}}"
        - "-single-namespace={{.SingleNamespace}}"
        - "-log-level={{.ControllerLogLevel}}"
        livenessProbe:
          httpGet:
            path: /ping
            port: 9998
          initialDelaySeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 9998
          failureThreshold: 7
        {{- if .EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.ControllerUID}}

{{- if not .SingleNamespace }}
### Service Profile CRD ###
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: serviceprofiles.linkerd.io
  namespace: {{.Namespace}}
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  group: linkerd.io
  version: v1alpha1
  scope: Namespaced
  names:
    plural: serviceprofiles
    singular: serviceprofile
    kind: ServiceProfile
    shortNames:
    - sp
  validation:
    openAPIV3Schema:
      properties:
        spec:
          required:
          - routes
          properties:
            routes:
              type: array
              items:
                type: object
                required:
                - name
                - condition
                properties:
                  name:
                    type: string
                  condition:
                    type: object
                    minProperties: 1
                    properties:
                      method:
                        type: string
                      pathRegex:
                        type: string
                      all:
                        type: array
                        items:
                          type: object
                      any:
                        type: array
                        items:
                          type: object
                      not:
                        type: object
                  responseClasses:
                    type: array
                    items:
                      type: object
                      required:
                      - condition
                      properties:
                        isFailure:
                          type: boolean
                        condition:
                          type: object
                          properties:
                            status:
                              type: object
                              minProperties: 1
                              properties:
                                min:
                                  type: integer
                                  minimum: 100
                                  maximum: 599
                                max:
                                  type: integer
                                  minimum: 100
                                  maximum: 599
                            all:
                              type: array
                              items:
                                type: object
                            any:
                              type: array
                              items:
                                type: object
                            not:
                              type: object
{{- end }}

### Service Account Web ###
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-web
  namespace: {{.Namespace}}

### Web ###
---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-web
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: web
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.ControllerComponentLabel}}: web
  ports:
  - name: http
    port: 8084
    targetPort: 8084
  - name: admin-http
    port: 9994
    targetPort: 9994

---
kind: Deployment
apiVersion: extensions/v1beta1
metadata:
  name: linkerd-web
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: web
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        {{.ControllerComponentLabel}}: web
      annotations:
        {{.CreatedByAnnotation}}: {{.CliVersion}}
    spec:
      containers:
      - name: web
        ports:
        - name: http
          containerPort: 8084
        - name: admin-http
          containerPort: 9994
        image: {{.WebImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        args:
        - "-api-addr=linkerd-controller-api.{{.Namespace}}.svc.cluster.local:8085"
        - "-grafana-addr=linkerd-grafana.{{.Namespace}}.svc.cluster.local:3000"
        - "-uuid={{.UUID}}"
        - "-controller-namespace={{.Namespace}}"
        - "-single-namespace={{.SingleNamespace}}"
        - "-log-level={{.ControllerLogLevel}}"
        livenessProbe:
          httpGet:
            path: /ping
            port: 9994
          initialDelaySeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 9994
          failureThreshold: 7
        {{- if .EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.ControllerUID}}
      serviceAccountName: linkerd-web

### Prometheus ###
---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-prometheus
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: prometheus
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.ControllerComponentLabel}}: prometheus
  ports:
  - name: admin-http
    port: 9090
    targetPort: 9090

---
kind: Deployment
apiVersion: extensions/v1beta1
metadata:
  name: linkerd-prometheus
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: prometheus
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        {{.ControllerComponentLabel}}: prometheus
      annotations:
        {{.CreatedByAnnotation}}: {{.CliVersion}}
    spec:
      serviceAccountName: linkerd-prometheus
      volumes:
      - name: {{.PrometheusVolumeName}}
        emptyDir: {}
      - name: prometheus-config
        configMap:
          name: linkerd-prometheus-config
      containers:
      - name: prometheus
        ports:
        - name: admin-http
          containerPort: 9090
        volumeMounts:
        - name: {{.PrometheusVolumeName}}
          mountPath: /{{.PrometheusVolumeName}}
        - name: prometheus-config
          mountPath: /etc/prometheus
          readOnly: true
        image: {{.PrometheusImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        args:
        - "--storage.tsdb.path=/{{.PrometheusVolumeName}}"
        - "--storage.tsdb.retention=6h"
        - "--config.file=/etc/prometheus/prometheus.yml"
        readinessProbe:
          httpGet:
            path: /-/ready
            port: 9090
          initialDelaySeconds: 30
          timeoutSeconds: 30
        livenessProbe:
          httpGet:
            path: /-/healthy
            port: 9090
          initialDelaySeconds: 30
          timeoutSeconds: 30
        {{- if .EnableHA }}
        resources:
          requests:
            cpu: 300m
            memory: 300Mi
        {{- end }}
        securityContext:
          runAsUser: 65534

---
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-prometheus-config
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: prometheus
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
data:
  prometheus.yml: |-
    global:
      scrape_interval: 10s
      scrape_timeout: 10s
      evaluation_interval: 10s

    rule_files:
    - /etc/prometheus/*_rules.yml

    scrape_configs:
    - job_name: 'prometheus'
      static_configs:
      - targets: ['localhost:9090']

    - job_name: 'grafana'
      kubernetes_sd_configs:
      - role: pod
        namespaces:
          names: ['{{.Namespace}}']
      relabel_configs:
      - source_labels:
        - __meta_kubernetes_pod_container_name
        action: keep
        regex: ^grafana$

    - job_name: 'linkerd-controller'
      kubernetes_sd_configs:
      - role: pod
        namespaces:
          names: ['{{.Namespace}}']
      relabel_configs:
      - source_labels:
        - __meta_kubernetes_pod_label_linkerd_io_control_plane_component
        - __meta_kubernetes_pod_container_port_name
        action: keep
        regex: (.*);admin-http$
      - source_labels: [__meta_kubernetes_pod_container_name]
        action: replace
        target_label: component

    - job_name: 'linkerd-proxy'
      kubernetes_sd_configs:
      - role: pod
        {{- if .SingleNamespace}}
        namespaces:
          names: ['{{.Namespace}}']
        {{- end}}
      relabel_configs:
      - source_labels:
        - __meta_kubernetes_pod_container_name
        - __meta_kubernetes_pod_container_port_name
        - __meta_kubernetes_pod_label_linkerd_io_control_plane_ns
        action: keep
        regex: ^{{.ProxyContainerName}};linkerd-metrics;{{.Namespace}}$
      - source_labels: [__meta_kubernetes_namespace]
        action: replace
        target_label: namespace
      - source_labels: [__meta_kubernetes_pod_name]
        action: replace
        target_label: pod
      # special case k8s' "job" label, to not interfere with prometheus' "job"
      # label
      # __meta_kubernetes_pod_label_linkerd_io_proxy_job=foo =>
      # k8s_job=foo
      - source_labels: [__meta_kubernetes_pod_label_linkerd_io_proxy_job]
        action: replace
        target_label: k8s_job
      # drop __meta_kubernetes_pod_label_linkerd_io_proxy_job
      - action: labeldrop
        regex: __meta_kubernetes_pod_label_linkerd_io_proxy_job
      # __meta_kubernetes_pod_label_linkerd_io_proxy_deployment=foo =>
      # deployment=foo
      - action: labelmap
        regex: __meta_kubernetes_pod_label_linkerd_io_proxy_(.+)
      # drop all labels that we just made copies of in the previous labelmap
      - action: labeldrop
        regex: __meta_kubernetes_pod_label_linkerd_io_proxy_(.+)
      # __meta_kubernetes_pod_label_linkerd_io_foo=bar =>
      # foo=bar
      - action: labelmap
        regex: __meta_kubernetes_pod_label_linkerd_io_(.+)

### Service Account Grafana ###
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-grafana
  namespace: {{.Namespace}}

### Grafana ###
---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-grafana
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: grafana
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.ControllerComponentLabel}}: grafana
  ports:
  - name: http
    port: 3000
    targetPort: 3000

---
kind: Deployment
apiVersion: extensions/v1beta1
metadata:
  name: linkerd-grafana
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: grafana
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        {{.ControllerComponentLabel}}: grafana
      annotations:
        {{.CreatedByAnnotation}}: {{.CliVersion}}
    spec:
      volumes:
      - name: {{.GrafanaVolumeName}}
        emptyDir: {}
      - name: grafana-config
        configMap:
          name: linkerd-grafana-config
          items:
          - key: grafana.ini
            path: grafana.ini
          - key: datasources.yaml
            path: provisioning/datasources/datasources.yaml
          - key: dashboards.yaml
            path: provisioning/dashboards/dashboards.yaml
      containers:
      - name: grafana
        ports:
        - name: http
          containerPort: 3000
        env:
        - name: GF_PATHS_DATA
          value: /{{.GrafanaVolumeName}}
        volumeMounts:
        - name: {{.GrafanaVolumeName}}
          mountPath: /{{.GrafanaVolumeName}}
        - name: grafana-config
          mountPath: /etc/grafana
          readOnly: true
        image: {{.GrafanaImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        livenessProbe:
          httpGet:
            path: /api/health
            port: 3000
          initialDelaySeconds: 30
        readinessProbe:
          httpGet:
            path: /api/health
            port: 3000
        {{- if .EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: 472
      serviceAccountName: linkerd-grafana

---
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-grafana-config
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: grafana
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
data:
  grafana.ini: |-
    instance_name = linkerd-grafana

    [server]
    root_url = %(protocol)s://%(domain)s:/grafana/

    [auth]
    disable_login_form = true

    [auth.anonymous]
    enabled = true
    org_role = Editor

    [auth.basic]
    enabled = false

    [analytics]
    check_for_updates = false

  datasources.yaml: |-
    apiVersion: 1
    datasources:
    - name: prometheus
      type: prometheus
      access: proxy
      orgId: 1
      url: http://linkerd-prometheus.{{.Namespace}}.svc.cluster.local:9090
      isDefault: true
      jsonData:
        timeInterval: "5s"
      version: 1
      editable: true

  dashboards.yaml: |-
    apiVersion: 1
    providers:
    - name: 'default'
      orgId: 1
      folder: ''
      type: file
      disableDeletion: true
      editable: true
      options:
        path: /var/lib/grafana/dashboards
        homeDashboardId: linkerd-top-line
`

// TLSTemplate provides additional configs when linkerd is installed with `--tls optional`
const TLSTemplate = `
### Service Account CA ###
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-ca
  namespace: {{.Namespace}}

### CA RBAC ###
---
kind: {{if not .SingleNamespace}}Cluster{{end}}Role
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Namespace}}-ca
  {{- if .SingleNamespace}}
  namespace: {{.Namespace}}
  {{- end}}
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["create"]
- apiGroups: [""]
  resources: ["configmaps"]
  resourceNames: [{{.TLSTrustAnchorConfigMapName}}]
  verbs: ["update"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["list", "get", "watch"]
- apiGroups: ["extensions", "apps"]
  resources: ["replicasets"]
  verbs: ["list", "get", "watch"]
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["create", "update"]
{{- if and .EnableTLS .ProxyAutoInjectEnabled }}
- apiGroups: ["admissionregistration.k8s.io"]
  resources: ["mutatingwebhookconfigurations"]
  verbs: ["list", "get", "watch"]
{{- end }}

---
kind: {{if not .SingleNamespace}}Cluster{{end}}RoleBinding
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Namespace}}-ca
  {{- if .SingleNamespace}}
  namespace: {{.Namespace}}
  {{- end}}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: {{if not .SingleNamespace}}Cluster{{end}}Role
  name: linkerd-{{.Namespace}}-ca
subjects:
- kind: ServiceAccount
  name: linkerd-ca
  namespace: {{.Namespace}}

### CA ###
---
kind: Deployment
apiVersion: extensions/v1beta1
metadata:
  name: linkerd-ca
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: ca
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        {{.ControllerComponentLabel}}: ca
      annotations:
        {{.CreatedByAnnotation}}: {{.CliVersion}}
    spec:
      serviceAccountName: linkerd-ca
      containers:
      - name: ca
        ports:
        - name: admin-http
          containerPort: 9997
        image: {{.ControllerImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        args:
        - "ca"
        - "-controller-namespace={{.Namespace}}"
        - "-single-namespace={{.SingleNamespace}}"
        {{- if and .EnableTLS .ProxyAutoInjectEnabled }}
        - "-proxy-auto-inject={{ .ProxyAutoInjectEnabled }}"
        {{- end }}
        - "-log-level={{.ControllerLogLevel}}"
        livenessProbe:
          httpGet:
            path: /ping
            port: 9997
          initialDelaySeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 9997
          failureThreshold: 7
        {{- if .EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.ControllerUID}}
`

// ProxyInjectorTemplate provides additional configs when linkerd is installed with `--proxy-auto-inject`
const ProxyInjectorTemplate = `
---
### Proxy Injector Deployment ###
kind: Deployment
apiVersion: apps/v1
metadata:
  name: linkerd-proxy-injector
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: proxy-injector
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  replicas: 1
  selector:
    matchLabels:
      {{.ControllerComponentLabel}}: proxy-injector
  template:
    metadata:
      labels:
        {{.ControllerComponentLabel}}: proxy-injector
      annotations:
        {{.CreatedByAnnotation}}: {{.CliVersion}}
    spec:
      serviceAccountName: linkerd-proxy-injector
      containers:
      - name: proxy-injector
        image: {{.ControllerImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        args:
        - "proxy-injector"
        - "-controller-namespace={{.Namespace}}"
        - "-log-level={{.ControllerLogLevel}}"
        ports:
        - name: proxy-injector
          containerPort: 8443
        volumeMounts:
        - name: {{.TLSTrustAnchorVolumeName}}
          mountPath: /var/linkerd-io/trust-anchors
          readOnly: true
        - name: webhook-secrets
          mountPath: /var/linkerd-io/identity
          readOnly: true
        - name: proxy-spec
          mountPath: /var/linkerd-io/config
        livenessProbe:
          httpGet:
            path: /ping
            port: 9995
          initialDelaySeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 9995
          failureThreshold: 7
        {{- if .EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.ControllerUID}}
      volumes:
      - name: webhook-secrets
        secret:
          secretName: {{.ProxyInjectorTLSSecret}}
          optional: true
      - name: proxy-spec
        configMap:
          name: linkerd-proxy-injector-sidecar-config

---
### Proxy Injector Service Account ###
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-proxy-injector
  namespace: {{.Namespace}}

---
### Proxy Injector RBAC ###
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-{{.Namespace}}-proxy-injector
rules:
- apiGroups: ["admissionregistration.k8s.io"]
  resources: ["mutatingwebhookconfigurations"]
  verbs: ["create", "update", "get", "watch"]

---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-{{.Namespace}}-proxy-injector
subjects:
- kind: ServiceAccount
  name: linkerd-proxy-injector
  namespace: {{.Namespace}}
  apiGroup: ""
roleRef:
  kind: ClusterRole
  name: linkerd-{{.Namespace}}-proxy-injector
  apiGroup: rbac.authorization.k8s.io

---
### Proxy Injector Service ###
kind: Service
apiVersion: v1
metadata:
  name: linkerd-proxy-injector
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: proxy-injector
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.ControllerComponentLabel}}: proxy-injector
  ports:
  - name: proxy-injector
    port: 443
    targetPort: proxy-injector

---
### Proxy Sidecar Container Spec ###
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-proxy-injector-sidecar-config
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: proxy-injector
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
data:
  {{.ProxyInitSpecFileName}}: |
    args:
    - --incoming-proxy-port
    - {{.InboundPort}}
    - --outgoing-proxy-port
    - {{.OutboundPort}}
    - --proxy-uid
    - {{.ProxyUID}}
    {{- if ne (len .IgnoreInboundPorts) 0}}
    - --inbound-ports-to-ignore
    - {{.IgnoreInboundPorts}}
    {{- end }}
    {{- if ne (len .IgnoreOutboundPorts) 0}}
    - --outbound-ports-to-ignore
    - {{.IgnoreOutboundPorts}}
    {{- end}}
    image: {{.ProxyInitImage}}
    imagePullPolicy: IfNotPresent
    name: linkerd-init
    securityContext:
      capabilities:
        add:
        - NET_ADMIN
      privileged: false
      runAsNonRoot: false
      runAsUser: 0
    terminationMessagePolicy: FallbackToLogsOnError
  {{.ProxySpecFileName}}: |
    env:
    - name: LINKERD2_PROXY_LOG
      value: warn,linkerd2_proxy=info
    - name: LINKERD2_PROXY_CONTROL_URL
      value: tcp://linkerd-proxy-api.{{.Namespace}}.svc.cluster.local:{{.ProxyAPIPort}}
    - name: LINKERD2_PROXY_CONTROL_LISTENER
      value: tcp://0.0.0.0:{{.ProxyControlPort}}
    - name: LINKERD2_PROXY_METRICS_LISTENER
      value: tcp://0.0.0.0:{{.ProxyMetricsPort}}
    - name: LINKERD2_PROXY_OUTBOUND_LISTENER
      value: tcp://127.0.0.1:{{.OutboundPort}}
    - name: LINKERD2_PROXY_INBOUND_LISTENER
      value: tcp://0.0.0.0:{{.InboundPort}}
    - name: LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES
      value: {{.ProfileSuffixes}}
    - name: LINKERD2_PROXY_POD_NAMESPACE
      valueFrom:
        fieldRef:
          fieldPath: metadata.namespace
    - name: LINKERD2_PROXY_TLS_TRUST_ANCHORS
      value: /var/linkerd-io/trust-anchors/{{.TLSTrustAnchorFileName}}
    - name: LINKERD2_PROXY_TLS_CERT
      value: /var/linkerd-io/identity/{{.TLSCertFileName}}
    - name: LINKERD2_PROXY_TLS_PRIVATE_KEY
      value: /var/linkerd-io/identity/{{.TLSPrivateKeyFileName}}
    - name: LINKERD2_PROXY_TLS_POD_IDENTITY
      value: "" # this value will be computed by the webhook
    - name: LINKERD2_PROXY_CONTROLLER_NAMESPACE
      value: {{.Namespace}}
    - name: LINKERD2_PROXY_TLS_CONTROLLER_IDENTITY
      value: "" # this value will be computed by the webhook
    image: {{.ProxyImage}}
    imagePullPolicy: IfNotPresent
    livenessProbe:
      httpGet:
        path: /metrics
        port: {{.ProxyMetricsPort}}
      initialDelaySeconds: 10
    name: linkerd-proxy
    ports:
    - containerPort: {{.InboundPort}}
      name: linkerd-proxy
    - containerPort: {{.ProxyMetricsPort}}
      name: linkerd-metrics
    readinessProbe:
      httpGet:
        path: /metrics
        port: {{.ProxyMetricsPort}}
      initialDelaySeconds: 10
    {{- if or .ProxyResourceRequestCPU .ProxyResourceRequestMemory }}
    resources:
      requests:
        {{- if .ProxyResourceRequestCPU }}
        cpu: {{.ProxyResourceRequestCPU}}
        {{- end }}
        {{- if .ProxyResourceRequestMemory}}
        memory: {{.ProxyResourceRequestMemory}}
        {{- end }}
    {{- end }}
    securityContext:
      runAsUser: {{.ProxyUID}}
    terminationMessagePolicy: FallbackToLogsOnError
    volumeMounts:
    - mountPath: /var/linkerd-io/trust-anchors
      name: {{.TLSTrustAnchorVolumeName}}
      readOnly: true
    - mountPath: /var/linkerd-io/identity
      name: {{.TLSSecretsVolumeName}}
      readOnly: true
  {{.TLSTrustAnchorVolumeSpecFileName}}: |
    name: {{.TLSTrustAnchorVolumeName}}
    configMap:
      name: {{.TLSTrustAnchorConfigMapName}}
      optional: true
  {{.TLSIdentityVolumeSpecFileName}}: |
    name: {{.TLSSecretsVolumeName}}
    secret:
      secretName: "" # this value will be computed by the webhook
      optional: true
`
