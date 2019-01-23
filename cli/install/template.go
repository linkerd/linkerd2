package install

// Chart provides the Chart.yaml file needed to validate chart metadata
var Chart = []byte(`apiVersion: "v1"
name: "linkerd"
version: 0.1.0`)

// BaseTemplate provides the base template used in `linkerd install` command
var BaseTemplate = []byte(`{{ if not .Values.SingleNamespace -}}
### Namespace ###
kind: Namespace
apiVersion: v1
metadata:
  name: {{.Values.Namespace}}
  {{- if and .Values.EnableTLS .Values.ProxyAutoInjectEnabled }}
  labels:
    {{.Values.ProxyAutoInjectLabel}}: disabled
  {{- end }}

{{ end -}}

### Service Account Controller ###
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-controller
  namespace: {{.Values.Namespace}}

### Controller RBAC ###
---
kind: {{if not .Values.SingleNamespace}}Cluster{{end}}Role
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Values.Namespace}}-controller
  {{- if .Values.SingleNamespace}}
  namespace: {{.Values.Namespace}}
  {{- end}}
rules:
- apiGroups: ["extensions", "apps"]
  resources: ["daemonsets", "deployments", "replicasets"]
  verbs: ["list", "get", "watch"]
- apiGroups: [""]
  resources: ["pods", "endpoints", "services", "replicationcontrollers"{{if not .Values.SingleNamespace}}, "namespaces"{{end}}]
  verbs: ["list", "get", "watch"]
{{- if .Values.SingleNamespace }}
- apiGroups: [""]
  resources: ["namespaces"]
  resourceNames: ["{{.Values.Namespace}}"]
  verbs: ["list", "get", "watch"]
{{- else }}
- apiGroups: ["linkerd.io"]
  resources: ["serviceprofiles"]
  verbs: ["list", "get", "watch"]
{{- end }}

---
kind: {{if not .Values.SingleNamespace}}Cluster{{end}}RoleBinding
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Values.Namespace}}-controller
  {{- if .Values.SingleNamespace}}
  namespace: {{.Values.Namespace}}
  {{- end}}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: {{if not .Values.SingleNamespace}}Cluster{{end}}Role
  name: linkerd-{{.Values.Namespace}}-controller
subjects:
- kind: ServiceAccount
  name: linkerd-controller
  namespace: {{.Values.Namespace}}

### Service Account Prometheus ###
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-prometheus
  namespace: {{.Values.Namespace}}

### Prometheus RBAC ###
---
kind: {{if not .Values.SingleNamespace}}Cluster{{end}}Role
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Values.Namespace}}-prometheus
  {{- if .Values.SingleNamespace}}
  namespace: {{.Values.Namespace}}
  {{- end}}
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]

---
kind: {{if not .Values.SingleNamespace}}Cluster{{end}}RoleBinding
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Values.Namespace}}-prometheus
  {{- if .Values.SingleNamespace}}
  namespace: {{.Values.Namespace}}
  {{- end}}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: {{if not .Values.SingleNamespace}}Cluster{{end}}Role
  name: linkerd-{{.Values.Namespace}}-prometheus
subjects:
- kind: ServiceAccount
  name: linkerd-prometheus
  namespace: {{.Values.Namespace}}

### Controller ###
---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-controller-api
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: controller
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.Values.ControllerComponentLabel}}: controller
  ports:
  - name: http
    port: 8085
    targetPort: 8085

---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-proxy-api
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: controller
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.Values.ControllerComponentLabel}}: controller
  ports:
  - name: grpc
    port: {{.Values.ProxyAPIPort}}
    targetPort: {{.Values.ProxyAPIPort}}

---
kind: Deployment
apiVersion: extensions/v1beta1
metadata:
  name: linkerd-controller
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: controller
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  replicas: {{.Values.ControllerReplicas}}
  template:
    metadata:
      labels:
        {{.Values.ControllerComponentLabel}}: controller
      annotations:
        {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
    spec:
      serviceAccountName: linkerd-controller
      containers:
      - name: public-api
        ports:
        - name: http
          containerPort: 8085
        - name: admin-http
          containerPort: 9995
        image: {{.Values.ControllerImage}}
        imagePullPolicy: {{.Values.ImagePullPolicy}}
        args:
        - "public-api"
        - "-prometheus-url=http://linkerd-prometheus.{{.Values.Namespace}}.svc.cluster.local:9090"
        - "-controller-namespace={{.Values.Namespace}}"
        - "-single-namespace={{.Values.SingleNamespace}}"
        - "-log-level={{.Values.ControllerLogLevel}}"
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
        {{- if .Values.EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.Values.ControllerUID}}
      - name: proxy-api
        ports:
        - name: grpc
          containerPort: {{.Values.ProxyAPIPort}}
        - name: admin-http
          containerPort: 9996
        image: {{.Values.ControllerImage}}
        imagePullPolicy: {{.Values.ImagePullPolicy}}
        args:
        - "proxy-api"
        - "-addr=:{{.Values.ProxyAPIPort}}"
        - "-controller-namespace={{.Values.Namespace}}"
        - "-single-namespace={{.Values.SingleNamespace}}"
        - "-enable-tls={{.Values.EnableTLS}}"
        - "-enable-h2-upgrade={{.Values.EnableH2Upgrade}}"
        - "-log-level={{.Values.ControllerLogLevel}}"
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
        {{- if .Values.EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.Values.ControllerUID}}
      - name: tap
        ports:
        - name: grpc
          containerPort: 8088
        - name: admin-http
          containerPort: 9998
        image: {{.Values.ControllerImage}}
        imagePullPolicy: {{.Values.ImagePullPolicy}}
        args:
        - "tap"
        - "-controller-namespace={{.Values.Namespace}}"
        - "-single-namespace={{.Values.SingleNamespace}}"
        - "-log-level={{.Values.ControllerLogLevel}}"
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
        {{- if .Values.EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.Values.ControllerUID}}

{{- if not .Values.SingleNamespace }}
### Service Profile CRD ###
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: serviceprofiles.linkerd.io
  namespace: {{.Values.Namespace}}
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
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
  namespace: {{.Values.Namespace}}

### Web ###
---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-web
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: web
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.Values.ControllerComponentLabel}}: web
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
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: web
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        {{.Values.ControllerComponentLabel}}: web
      annotations:
        {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
    spec:
      containers:
      - name: web
        ports:
        - name: http
          containerPort: 8084
        - name: admin-http
          containerPort: 9994
        image: {{.Values.WebImage}}
        imagePullPolicy: {{.Values.ImagePullPolicy}}
        args:
        - "-api-addr=linkerd-controller-api.{{.Values.Namespace}}.svc.cluster.local:8085"
        - "-grafana-addr=linkerd-grafana.{{.Values.Namespace}}.svc.cluster.local:3000"
        - "-uuid={{.Values.UUID}}"
        - "-controller-namespace={{.Values.Namespace}}"
        - "-single-namespace={{.Values.SingleNamespace}}"
        - "-log-level={{.Values.ControllerLogLevel}}"
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
        {{- if .Values.EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.Values.ControllerUID}}
      serviceAccountName: linkerd-web

### Prometheus ###
---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-prometheus
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: prometheus
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.Values.ControllerComponentLabel}}: prometheus
  ports:
  - name: admin-http
    port: 9090
    targetPort: 9090

---
kind: Deployment
apiVersion: extensions/v1beta1
metadata:
  name: linkerd-prometheus
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: prometheus
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        {{.Values.ControllerComponentLabel}}: prometheus
      annotations:
        {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
    spec:
      serviceAccountName: linkerd-prometheus
      volumes:
      - name: {{.Values.PrometheusVolumeName}}
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
        - name: {{.Values.PrometheusVolumeName}}
          mountPath: /{{.Values.PrometheusVolumeName}}
        - name: prometheus-config
          mountPath: /etc/prometheus
          readOnly: true
        image: {{.Values.PrometheusImage}}
        imagePullPolicy: {{.Values.ImagePullPolicy}}
        args:
        - "--storage.tsdb.path=/{{.Values.PrometheusVolumeName}}"
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
        {{- if .Values.EnableHA }}
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
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: prometheus
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
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
          names: ['{{.Values.Namespace}}']
      relabel_configs:
      - source_labels:
        - __meta_kubernetes_pod_container_name
        action: keep
        regex: ^grafana$

    - job_name: 'linkerd-controller'
      kubernetes_sd_configs:
      - role: pod
        namespaces:
          names: ['{{.Values.Namespace}}']
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
        {{- if .Values.SingleNamespace}}
        namespaces:
          names: ['{{.Values.Namespace}}']
        {{- end}}
      relabel_configs:
      - source_labels:
        - __meta_kubernetes_pod_container_name
        - __meta_kubernetes_pod_container_port_name
        - __meta_kubernetes_pod_label_linkerd_io_control_plane_ns
        action: keep
        regex: ^{{.Values.ProxyContainerName}};linkerd-metrics;{{.Values.Namespace}}$
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
  namespace: {{.Values.Namespace}}

### Grafana ###
---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-grafana
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: grafana
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.Values.ControllerComponentLabel}}: grafana
  ports:
  - name: http
    port: 3000
    targetPort: 3000

---
kind: Deployment
apiVersion: extensions/v1beta1
metadata:
  name: linkerd-grafana
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: grafana
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        {{.Values.ControllerComponentLabel}}: grafana
      annotations:
        {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
    spec:
      volumes:
      - name: {{.Values.GrafanaVolumeName}}
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
          value: /{{.Values.GrafanaVolumeName}}
        volumeMounts:
        - name: {{.Values.GrafanaVolumeName}}
          mountPath: /{{.Values.GrafanaVolumeName}}
        - name: grafana-config
          mountPath: /etc/grafana
          readOnly: true
        image: {{.Values.GrafanaImage}}
        imagePullPolicy: {{.Values.ImagePullPolicy}}
        livenessProbe:
          httpGet:
            path: /api/health
            port: 3000
          initialDelaySeconds: 30
        readinessProbe:
          httpGet:
            path: /api/health
            port: 3000
        {{- if .Values.EnableHA }}
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
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: grafana
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
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
      url: http://linkerd-prometheus.{{.Values.Namespace}}.svc.cluster.local:9090
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
        homeDashboardId: linkerd-top-line`)

// TLSTemplate provides the additional configurations when linkerd is
// installed with `--tls optional`
var TLSTemplate = []byte(`{{ if .Values.EnableTLS }}
### Service Account CA ###
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-ca
  namespace: {{.Values.Namespace}}

### CA RBAC ###
---
kind: {{if not .Values.SingleNamespace}}Cluster{{end}}Role
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Values.Namespace}}-ca
  {{- if .Values.SingleNamespace}}
  namespace: {{.Values.Namespace}}
  {{- end}}
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["create"]
- apiGroups: [""]
  resources: ["configmaps"]
  resourceNames: [{{.Values.TLSTrustAnchorConfigMapName}}]
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
{{- if and .Values.EnableTLS .Values.ProxyAutoInjectEnabled }}
- apiGroups: ["admissionregistration.k8s.io"]
  resources: ["mutatingwebhookconfigurations"]
  verbs: ["list", "get", "watch"]
{{- end }}

---
kind: {{if not .Values.SingleNamespace}}Cluster{{end}}RoleBinding
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: linkerd-{{.Values.Namespace}}-ca
  {{- if .Values.SingleNamespace}}
  namespace: {{.Values.Namespace}}
  {{- end}}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: {{if not .Values.SingleNamespace}}Cluster{{end}}Role
  name: linkerd-{{.Values.Namespace}}-ca
subjects:
- kind: ServiceAccount
  name: linkerd-ca
  namespace: {{.Values.Namespace}}

### CA ###
---
kind: Deployment
apiVersion: extensions/v1beta1
metadata:
  name: linkerd-ca
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: ca
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        {{.Values.ControllerComponentLabel}}: ca
      annotations:
        {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
    spec:
      serviceAccountName: linkerd-ca
      containers:
      - name: ca
        ports:
        - name: admin-http
          containerPort: 9997
        image: {{.Values.ControllerImage}}
        imagePullPolicy: {{.Values.ImagePullPolicy}}
        args:
        - "ca"
        - "-controller-namespace={{.Values.Namespace}}"
        - "-single-namespace={{.Values.SingleNamespace}}"
        {{- if and .Values.EnableTLS .Values.ProxyAutoInjectEnabled }}
        - "-proxy-auto-inject={{ .Values.ProxyAutoInjectEnabled }}"
        {{- end }}
        - "-log-level={{.Values.ControllerLogLevel}}"
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
        {{- if .Values.EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.Values.ControllerUID}}
{{ end }}`)

// ProxyInjectorTemplate provides the additional configurations when linkerd
// is installed with `--proxy-auto-inject`
var ProxyInjectorTemplate = []byte(`{{ if .Values.ProxyAutoInjectEnabled }}
### Proxy Injector Deployment ###
---
kind: Deployment
apiVersion: apps/v1
metadata:
  name: linkerd-proxy-injector
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: proxy-injector
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  replicas: 1
  selector:
    matchLabels:
      {{.Values.ControllerComponentLabel}}: proxy-injector
  template:
    metadata:
      labels:
        {{.Values.ControllerComponentLabel}}: proxy-injector
      annotations:
        {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
    spec:
      serviceAccountName: linkerd-proxy-injector
      containers:
      - name: proxy-injector
        image: {{.Values.ControllerImage}}
        imagePullPolicy: {{.Values.ImagePullPolicy}}
        args:
        - "proxy-injector"
        - "-controller-namespace={{.Values.Namespace}}"
        - "-log-level={{.Values.ControllerLogLevel}}"
        ports:
        - name: proxy-injector
          containerPort: 8443
        volumeMounts:
        - name: {{.Values.TLSTrustAnchorVolumeName}}
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
        {{- if .Values.EnableHA }}
        resources:
          requests:
            cpu: 20m
            memory: 50Mi
        {{- end }}
        securityContext:
          runAsUser: {{.Values.ControllerUID}}
      volumes:
      - name: webhook-secrets
        secret:
          secretName: {{.Values.ProxyInjectorTLSSecret}}
          optional: true
      - name: proxy-spec
        configMap:
          name: linkerd-proxy-injector-sidecar-config

### Proxy Injector Service Account ###
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-proxy-injector
  namespace: {{.Values.Namespace}}

### Proxy Injector RBAC ###
---
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-{{.Values.Namespace}}-proxy-injector
rules:
- apiGroups: ["admissionregistration.k8s.io"]
  resources: ["mutatingwebhookconfigurations"]
  verbs: ["create", "update", "get", "watch"]

---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-{{.Values.Namespace}}-proxy-injector
subjects:
- kind: ServiceAccount
  name: linkerd-proxy-injector
  namespace: {{.Values.Namespace}}
  apiGroup: ""
roleRef:
  kind: ClusterRole
  name: linkerd-{{.Values.Namespace}}-proxy-injector
  apiGroup: rbac.authorization.k8s.io

### Proxy Injector Service ###
---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-proxy-injector
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: proxy-injector
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
spec:
  type: ClusterIP
  selector:
    {{.Values.ControllerComponentLabel}}: proxy-injector
  ports:
  - name: proxy-injector
    port: 443
    targetPort: proxy-injector

### Proxy Sidecar Container Spec ###
---
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-proxy-injector-sidecar-config
  namespace: {{.Values.Namespace}}
  labels:
    {{.Values.ControllerComponentLabel}}: proxy-injector
  annotations:
    {{.Values.CreatedByAnnotation}}: {{.Values.CliVersion}}
data:
  {{.Values.ProxyInitSpecFileName}}: |
    args:
    - --incoming-proxy-port
    - {{.Values.InboundPort}}
    - --outgoing-proxy-port
    - {{.Values.OutboundPort}}
    - --proxy-uid
    - {{.Values.ProxyUID}}
    {{- if ne (len .Values.IgnoreInboundPorts) 0}}
    - --inbound-ports-to-ignore
    - {{.Values.IgnoreInboundPorts}}
    {{- end }}
    {{- if ne (len .Values.IgnoreOutboundPorts) 0}}
    - --outbound-ports-to-ignore
    - {{.Values.IgnoreOutboundPorts}}
    {{- end}}
    image: {{.Values.ProxyInitImage}}
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
  {{.Values.ProxySpecFileName}}: |
    env:
    - name: LINKERD2_PROXY_LOG
      value: warn,linkerd2_proxy=info
    - name: LINKERD2_PROXY_CONTROL_URL
      value: tcp://linkerd-proxy-api.{{.Values.Namespace}}.svc.cluster.local:{{.Values.ProxyAPIPort}}
    - name: LINKERD2_PROXY_CONTROL_LISTENER
      value: tcp://0.0.0.0:{{.Values.ProxyControlPort}}
    - name: LINKERD2_PROXY_METRICS_LISTENER
      value: tcp://0.0.0.0:{{.Values.ProxyMetricsPort}}
    - name: LINKERD2_PROXY_OUTBOUND_LISTENER
      value: tcp://127.0.0.1:{{.Values.OutboundPort}}
    - name: LINKERD2_PROXY_INBOUND_LISTENER
      value: tcp://0.0.0.0:{{.Values.InboundPort}}
    - name: LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES
      value: {{.Values.ProfileSuffixes}}
    - name: LINKERD2_PROXY_POD_NAMESPACE
      valueFrom:
        fieldRef:
          fieldPath: metadata.namespace
    - name: LINKERD2_PROXY_TLS_TRUST_ANCHORS
      value: /var/linkerd-io/trust-anchors/{{.Values.TLSTrustAnchorFileName}}
    - name: LINKERD2_PROXY_TLS_CERT
      value: /var/linkerd-io/identity/{{.Values.TLSCertFileName}}
    - name: LINKERD2_PROXY_TLS_PRIVATE_KEY
      value: /var/linkerd-io/identity/{{.Values.TLSPrivateKeyFileName}}
    - name: LINKERD2_PROXY_TLS_POD_IDENTITY
      value: "" # this value will be computed by the webhook
    - name: LINKERD2_PROXY_CONTROLLER_NAMESPACE
      value: {{.Values.Namespace}}
    - name: LINKERD2_PROXY_TLS_CONTROLLER_IDENTITY
      value: "" # this value will be computed by the webhook
    image: {{.Values.ProxyImage}}
    imagePullPolicy: IfNotPresent
    livenessProbe:
      httpGet:
        path: /metrics
        port: {{.Values.ProxyMetricsPort}}
      initialDelaySeconds: 10
    name: linkerd-proxy
    ports:
    - containerPort: {{.Values.InboundPort}}
      name: linkerd-proxy
    - containerPort: {{.Values.ProxyMetricsPort}}
      name: linkerd-metrics
    readinessProbe:
      httpGet:
        path: /metrics
        port: {{.Values.ProxyMetricsPort}}
      initialDelaySeconds: 10
    {{- if or .Values.ProxyResourceRequestCPU .Values.ProxyResourceRequestMemory }}
    resources:
      requests:
        {{- if .Values.ProxyResourceRequestCPU }}
        cpu: {{.Values.ProxyResourceRequestCPU}}
        {{- end }}
        {{- if .Values.ProxyResourceRequestMemory}}
        memory: {{.Values.ProxyResourceRequestMemory}}
        {{- end }}
    {{- end }}
    securityContext:
      runAsUser: {{.Values.ProxyUID}}
    terminationMessagePolicy: FallbackToLogsOnError
    volumeMounts:
    - mountPath: /var/linkerd-io/trust-anchors
      name: {{.Values.TLSTrustAnchorVolumeName}}
      readOnly: true
    - mountPath: /var/linkerd-io/identity
      name: {{.Values.TLSSecretsVolumeName}}
      readOnly: true
  {{.Values.TLSTrustAnchorVolumeSpecFileName}}: |
    name: {{.Values.TLSTrustAnchorVolumeName}}
    configMap:
      name: {{.Values.TLSTrustAnchorConfigMapName}}
      optional: true
  {{.Values.TLSIdentityVolumeSpecFileName}}: |
    name: {{.Values.TLSSecretsVolumeName}}
    secret:
      secretName: "" # this value will be computed by the webhook
      optional: true
{{ end }}`)
