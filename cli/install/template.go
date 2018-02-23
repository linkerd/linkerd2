package install

// Template provides the base template for the `conduit install` command.
const Template = `### Namespace ###
kind: Namespace
apiVersion: v1
metadata:
  name: {{.Namespace}}

### Service Account Controller ###
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: conduit-controller
  namespace: {{.Namespace}}

### RBAC ###
---
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: conduit-controller
rules:
- apiGroups: ["extensions"]
  resources: ["deployments", "replicasets"]
  verbs: ["list", "get", "watch"]
- apiGroups: [""]
  resources: ["pods", "endpoints", "services"]
  verbs: ["list", "get", "watch"]

---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: conduit-controller
  namespace: {{.Namespace}}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: conduit-controller
subjects:
- kind: ServiceAccount
  name: conduit-controller
  namespace: {{.Namespace}}

### Service Account Prometheus ###
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: conduit-prometheus
  namespace: {{.Namespace}}

### RBAC ###
---
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: conduit-prometheus
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["list", "watch"]

---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: conduit-prometheus
  namespace: {{.Namespace}}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: conduit-prometheus
subjects:
- kind: ServiceAccount
  name: conduit-prometheus
  namespace: {{.Namespace}}

### Controller ###
---
kind: Service
apiVersion: v1
metadata:
  name: api
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
  name: proxy-api
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
    port: 8086
    targetPort: 8086

---
kind: Deployment
apiVersion: extensions/v1beta1
metadata:
  name: controller
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
      serviceAccount: conduit-controller
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
        - "-addr=:8085"
        - "-metrics-addr=:9995"
        - "-telemetry-addr=127.0.0.1:8087"
        - "-tap-addr=127.0.0.1:8088"
        - "-log-level={{.ControllerLogLevel}}"
      - name: destination
        ports:
        - name: grpc
          containerPort: 8089
        - name: admin-http
          containerPort: 9999
        image: {{.ControllerImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        args:
        - "destination"
        - "-addr=:8089"
        - "-metrics-addr=:9999"
        - "-log-level={{.ControllerLogLevel}}"
      - name: proxy-api
        ports:
        - name: grpc
          containerPort: 8086
        - name: admin-http
          containerPort: 9996
        image: {{.ControllerImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        args:
        - "proxy-api"
        - "-addr=:8086"
        - "-metrics-addr=:9996"
        - "-destination-addr=:8089"
        - "-telemetry-addr=:8087"
        - "-log-level={{.ControllerLogLevel}}"
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
        - "-addr=:8088"
        - "-metrics-addr=:9998"
        - "-log-level={{.ControllerLogLevel}}"
      - name: telemetry
        ports:
        - name: grpc
          containerPort: 8087
        - name: admin-http
          containerPort: 9997
        image: {{.ControllerImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        args:
        - "telemetry"
        - "-addr=:8087"
        - "-metrics-addr=:9997"
        - "-ignore-namespaces=kube-system"
        - "-prometheus-url=http://prometheus.{{.Namespace}}.svc.cluster.local:9090"
        - "-log-level={{.ControllerLogLevel}}"

### Web ###
---
kind: Service
apiVersion: v1
metadata:
  name: web
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
  name: web
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: web
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  replicas: {{.WebReplicas}}
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
        - "-addr=:8084"
        - "-metrics-addr=:9994"
        - "-api-addr=api:8085"
        - "-static-dir=/dist"
        - "-template-dir=/templates"
        - "-uuid={{.UUID}}"
        - "-controller-namespace={{.Namespace}}"
        - "-log-level={{.ControllerLogLevel}}"

### Prometheus ###
---
kind: Service
apiVersion: v1
metadata:
  name: prometheus
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
  - name: http
    port: 9090
    targetPort: 9090

---
kind: Deployment
apiVersion: extensions/v1beta1
metadata:
  name: prometheus
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: prometheus
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  replicas: {{.PrometheusReplicas}}
  template:
    metadata:
      labels:
        {{.ControllerComponentLabel}}: prometheus
      annotations:
        {{.CreatedByAnnotation}}: {{.CliVersion}}
    spec:
      serviceAccount: conduit-prometheus
      volumes:
      - name: prometheus-config
        configMap:
          name: prometheus-config
      containers:
      - name: prometheus
        ports:
        - name: http
          containerPort: 9090
        volumeMounts:
        - name: prometheus-config
          mountPath: /etc/prometheus
          readOnly: true
        image: {{.PrometheusImage}}
        imagePullPolicy: {{.ImagePullPolicy}}
        args:
        - "--storage.tsdb.retention=6h"
        - "--config.file=/etc/prometheus/prometheus.yml"

      # TODO remove/replace?
      - name: kubectl
        image: buoyantio/kubectl:v1.6.2
        args: ["proxy", "-p", "8001"]

---
kind: ConfigMap
apiVersion: v1
metadata:
  name: prometheus-config
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: prometheus
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
data:
  prometheus.yml: |-
    global:
      scrape_interval: 10s
      evaluation_interval: 10s

    scrape_configs:
    - job_name: 'prometheus'
      static_configs:
      - targets: ['localhost:9090']

    - job_name: 'controller'
      kubernetes_sd_configs:
      - role: pod
        namespaces:
          names: ['{{.Namespace}}']
      relabel_configs:
      - source_labels: [__meta_kubernetes_pod_container_port_name]
        action: keep
        regex: ^admin-http$
      - source_labels: [__meta_kubernetes_pod_container_name]
        action: replace
        target_label: job

### Grafana ###
---
kind: Service
apiVersion: v1
metadata:
  name: grafana
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
  name: grafana
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
      - name: grafana-config
        configMap:
          name: grafana-config
          items:
          - key: grafana.ini
            path: grafana.ini
          - key: datasources.yaml
            path: provisioning/datasources/datasources.yaml
          - key: dashboards.yaml
            path: provisioning/dashboards/dashboards.yaml
      - name: grafana-dashboards
        configMap:
          name: grafana-dashboards
      - name: grafana-dashboard-home
        configMap:
          name: grafana-dashboards
          items:
          - key: conduit-viz.json
            path: home.json
      containers:
      - name: grafana
        ports:
        - name: http
          containerPort: 3000
        volumeMounts:
        - name: grafana-config
          mountPath: /etc/grafana
          readOnly: true
        - name: grafana-dashboards
          mountPath: /var/lib/grafana/dashboards
          readOnly: false
        - name: grafana-dashboard-home
          mountPath: /usr/share/grafana/public/dashboards
          readOnly: true
        image: {{.GrafanaImage}}
        imagePullPolicy: {{.ImagePullPolicy}}

---
kind: ConfigMap
apiVersion: v1
metadata:
  name: grafana-config
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: grafana
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
data:
  grafana.ini: |-
    instance_name = conduit-grafana

    [auth]
    disable_login_form = true

    [auth.anonymous]
    enabled = true
    org_role = Editor

    [auth.basic]
    enabled = false

  datasources.yaml: |-
    apiVersion: 1
    datasources:
    - name: prometheus
      type: prometheus
      access: proxy
      orgId: 1
      url: http://prometheus.{{.Namespace}}.svc.cluster.local:9090
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
        homeDashboardId: conduit-viz

---
kind: ConfigMap
apiVersion: v1
metadata:
  name: grafana-dashboards
  namespace: {{.Namespace}}
  labels:
    {{.ControllerComponentLabel}}: grafana
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
data:
  conduit-viz.json: |-
    {{.VizDashboard}}

  conduit-health.json: |-
    {{.HealthDashboard}}
`
