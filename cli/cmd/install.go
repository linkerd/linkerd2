package cmd

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"text/template"

	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/runconduit/conduit/pkg/version"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var conduitTemplate = `### Namespace ###
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
    app: controller
    {{.ControllerComponentLabel}}: controller
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  type: ClusterIP
  selector:
    app: controller
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
    app: controller
    {{.ControllerComponentLabel}}: controller
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  type: ClusterIP
  selector:
    app: controller
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
    app: controller
    {{.ControllerComponentLabel}}: controller
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  replicas: {{.ControllerReplicas}}
  template:
    metadata:
      labels:
        app: controller
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
        - "-prometheus-url=http://prometheus:9090"
        - "-log-level={{.ControllerLogLevel}}"

### Web ###
---
kind: Service
apiVersion: v1
metadata:
  name: web
  namespace: {{.Namespace}}
  labels:
    app: web
    {{.ControllerComponentLabel}}: web
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  type: ClusterIP
  selector:
    app: web
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
    app: web
    {{.ControllerComponentLabel}}: web
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  replicas: {{.WebReplicas}}
  template:
    metadata:
      labels:
        app: web
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
    app: prometheus
    {{.ControllerComponentLabel}}: prometheus
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  type: ClusterIP
  selector:
    app: prometheus
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
    app: prometheus
    {{.ControllerComponentLabel}}: prometheus
  annotations:
    {{.CreatedByAnnotation}}: {{.CliVersion}}
spec:
  replicas: {{.PrometheusReplicas}}
  template:
    metadata:
      labels:
        app: prometheus
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
    app: prometheus
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
`

type installConfig struct {
	Namespace                string
	ControllerImage          string
	WebImage                 string
	PrometheusImage          string
	ControllerReplicas       uint
	WebReplicas              uint
	PrometheusReplicas       uint
	ImagePullPolicy          string
	UUID                     string
	CliVersion               string
	ControllerLogLevel       string
	ControllerComponentLabel string
	CreatedByAnnotation      string
}

var (
	conduitVersion     string
	dockerRegistry     string
	controllerReplicas uint
	webReplicas        uint
	prometheusReplicas uint
	imagePullPolicy    string
	controllerLogLevel string
)

var installCmd = &cobra.Command{
	Use:   "install [flags]",
	Short: "Output Kubernetes configs to install Conduit",
	Long:  "Output Kubernetes configs to install Conduit.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validate(); err != nil {
			return err
		}
		config := installConfig{
			Namespace:                controlPlaneNamespace,
			ControllerImage:          fmt.Sprintf("%s/controller:%s", dockerRegistry, conduitVersion),
			WebImage:                 fmt.Sprintf("%s/web:%s", dockerRegistry, conduitVersion),
			PrometheusImage:          "prom/prometheus:v2.1.0",
			ControllerReplicas:       controllerReplicas,
			WebReplicas:              webReplicas,
			PrometheusReplicas:       prometheusReplicas,
			ImagePullPolicy:          imagePullPolicy,
			UUID:                     uuid.NewV4().String(),
			CliVersion:               k8s.CreatedByAnnotationValue(),
			ControllerLogLevel:       controllerLogLevel,
			ControllerComponentLabel: k8s.ControllerComponentLabel,
			CreatedByAnnotation:      k8s.CreatedByAnnotation,
		}
		return render(config, os.Stdout)
	},
}

func render(config installConfig, w io.Writer) error {
	template, err := template.New("conduit").Parse(conduitTemplate)
	if err != nil {
		return err
	}
	return template.Execute(w, config)
}

var alphaNumDash = regexp.MustCompile("^[a-zA-Z0-9-]+$")
var alphaNumDashDot = regexp.MustCompile("^[\\.a-zA-Z0-9-]+$")
var alphaNumDashDotSlash = regexp.MustCompile("^[\\./a-zA-Z0-9-]+$")

func validate() error {
	// These regexs are not as strict as they could be, but are a quick and dirty
	// sanity check against illegal characters.
	if !alphaNumDash.MatchString(controlPlaneNamespace) {
		return fmt.Errorf("%s is not a valid namespace", controlPlaneNamespace)
	}
	if !alphaNumDashDot.MatchString(conduitVersion) {
		return fmt.Errorf("%s is not a valid version", conduitVersion)
	}
	if !alphaNumDashDotSlash.MatchString(dockerRegistry) {
		return fmt.Errorf("%s is not a valid Docker registry", dockerRegistry)
	}
	if imagePullPolicy != "Always" && imagePullPolicy != "IfNotPresent" && imagePullPolicy != "Never" {
		return fmt.Errorf("--image-pull-policy must be one of: Always, IfNotPresent, Never")
	}
	if _, err := log.ParseLevel(controllerLogLevel); err != nil {
		return fmt.Errorf("--controller-log-level must be one of: panic, fatal, error, warn, info, debug")
	}
	return nil
}

func init() {
	RootCmd.AddCommand(installCmd)
	installCmd.PersistentFlags().StringVarP(&conduitVersion, "conduit-version", "v", version.Version, "tag to be used for Conduit images")
	installCmd.PersistentFlags().StringVarP(&dockerRegistry, "registry", "r", "gcr.io/runconduit", "Docker registry to pull images from")
	installCmd.PersistentFlags().UintVar(&controllerReplicas, "controller-replicas", 1, "replicas of the controller to deploy")
	installCmd.PersistentFlags().UintVar(&webReplicas, "web-replicas", 1, "replicas of the web server to deploy")
	installCmd.PersistentFlags().UintVar(&prometheusReplicas, "prometheus-replicas", 1, "replicas of prometheus to deploy")
	installCmd.PersistentFlags().StringVar(&imagePullPolicy, "image-pull-policy", "IfNotPresent", "Docker image pull policy")
	installCmd.PersistentFlags().StringVar(&controllerLogLevel, "controller-log-level", "info", "log level for the controller and web components")
}
