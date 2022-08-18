package cmd

import (
	"os"
	"text/template"

	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
)

const (
	allowScrapePolicy = `---
apiVersion: policy.linkerd.io/v1beta1
kind: Server
metadata:
  name: proxy-admin
  annotations:
    linkerd-io/created-by: {{ .ChartName }} {{ .Version }}
  labels:
    linkerd.io/extension: {{ .ExtensionName }}
spec:
  podSelector:
    matchExpressions:
    - key: linkerd.io/control-plane-ns
      operator: Exists
  port: linkerd-admin
  proxyProtocol: HTTP/1
---
apiVersion: policy.linkerd.io/v1alpha1
kind: HTTPRoute
metadata:
  name: proxy-metrics
  annotations:
    linkerd-io/created-by: {{ .ChartName }} {{ .Version }}
  labels:
    linkerd.io/extension: {{ .ExtensionName }}
spec:
  parentRefs:
    - name: proxy-admin
      kind: Server
      group: policy.linkerd.io
  rules:
    - matches:
      - path:
          value: "/metrics"
---
apiVersion: policy.linkerd.io/v1alpha1
kind: HTTPRoute
metadata:
  name: proxy-probes
  annotations:
    linkerd-io/created-by: {{ .ChartName }} {{ .Version }}
  labels:
    linkerd.io/extension: {{ .ExtensionName }}
spec:
  parentRefs:
    - name: proxy-admin
      kind: Server
      group: policy.linkerd.io
  rules:
    - matches:
      - path:
          value: "/live"
      - path:
          value: "/ready"
---
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  name: prometheus-scrape
  annotations:
    linkerd-io/created-by: {{ .ChartName }} {{ .Version }}
  labels:
    linkerd.io/extension: {{ .ExtensionName }}
spec:
  targetRef:
    group: policy.linkerd.io
    kind: HTTPRoute
    name: proxy-metrics
  requiredAuthenticationRefs:
    - kind: ServiceAccount
      name: prometheus
      namespace: {{ .VizNs }}
---
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  name: proxy-probes
  annotations:
    linkerd-io/created-by: {{ .ChartName }} {{ .Version }}
  labels:
    linkerd.io/extension: {{ .ExtensionName }}
spec:
  targetRef:
    group: policy.linkerd.io
    kind: HTTPRoute
    name: proxy-probes
  requiredAuthenticationRefs:
    - kind: NetworkAuthentication
      group: policy.linkerd.io
      name: kubelet
      namespace: {{ .VizNs }}`
)

type templateArgs struct {
	ChartName     string
	Version       string
	ExtensionName string
	VizNs         string
}

// newCmdAllowScrapes creates a new cobra command `allow-scrapes`
func newCmdAllowScrapes() *cobra.Command {
	return &cobra.Command{
		Use:   "allow-scrapes",
		Short: "Output Kubernetes resources to authorize Prometheus scrapes",
		Long:  `Output Kubernetes resources to authorize Prometheus scrapes in a namespace or cluster with config.linkerd.io/default-inbound-policy: deny.`,
		Example: `# Allow scrapes in the default namespace
		linkerd viz allow-scrapes | kubectl apply -f -
	   	`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := template.Must(template.New("allow-scrapes").Parse(allowScrapePolicy))
			return t.Execute(os.Stdout, templateArgs{
				ExtensionName: ExtensionName,
				ChartName:     vizChartName,
				Version:       version.Version,
				VizNs:         defaultNamespace,
			})
		},
	}
}
