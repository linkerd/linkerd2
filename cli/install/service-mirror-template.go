package install


const (
	// CNITemplate provides the base template for the `linkerd install-cni-plugin` command.
	ServiceMirrorTemplate = `### Namespace ###
kind: Namespace
apiVersion: v1
metadata:
  name: {{.Namespace}}
---
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-{{.Namespace}}-service-mirror
  labels:
    ControllerComponentLabel: service-mirror
    ControllerNamespaceLabel: {{.Namespace}}
rules:
- apiGroups: [""]
  resources: ["pods", "endpoints", "services", "secrets", "namespaces"]
  verbs: ["list", "get", "watch", "create"]
---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-{{.Namespace}}-service-mirror
  labels:
    ControllerComponentLabel: service-mirror
    ControllerNamespaceLabel: {{.Namespace}}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: linkerd-{{.Namespace}}-service-mirror
subjects:
- kind: ServiceAccount
  name: linkerd-service-mirror
  namespace: {{.Namespace}}
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-service-mirror
  namespace: {{.Namespace}}
  labels:
    ControllerComponentLabel: service-mirror
    ControllerNamespaceLabel: {{.Namespace}}
---
kind: Service
apiVersion: v1
metadata:
  name: linkerd-service-mirror
  namespace: {{.Namespace}}
spec:
  type: ClusterIP
  selector:
    ControllerComponentLabel: service-mirror
  ports:
  - name: grpc
    port: 8086
    targetPort: 8086
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    ControllerComponentLabel: service-mirror
  name: linkerd-service-mirror
  namespace: {{.Namespace}}
spec:
  replicas: 1
  selector:
    matchLabels:
      ControllerComponentLabel: service-mirror
      ControllerNamespaceLabel: {{.Namespace}}
  template:
    metadata:
      labels:
        ControllerComponentLabel: service-mirror
        ControllerNamespaceLabel: {{.Namespace}}
    spec:
      containers:
      - args:
        - service-mirror
        image: gcr.io/linkerd-io/controller:{{.LinkerdVersion}}
        name: service-mirror
        ports:
        - containerPort: 8086
          name: grpc
        - containerPort: 9996
          name: admin-http
        securityContext:
          runAsUser: 2103
      serviceAccountName: linkerd-service-mirror
`
)
