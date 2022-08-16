package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

const (
	allowScrapePolicy = `---
apiVersion: policy.linkerd.io/v1beta1
kind: Server
metadata:
  name: proxy-admin
  labels:
    linkerd.io/extension: viz
spec:
  podSelector:
    matchExpressions:
    - key: linkerd.io/proxy-deployment
      operator: Exists
  port: linkerd-admin
  proxyProtocol: HTTP/1
---
apiVersion: policy.linkerd.io/v1alpha1
kind: HTTPRoute
metadata:
  name: proxy-metrics
  labels:
    linkerd.io/extension: viz
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
  labels:
    linkerd.io/extension: viz
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
  labels:
    linkerd.io/extension: viz
spec:
  targetRef:
    group: policy.linkerd.io
    kind: HTTPRoute
    name: proxy-metrics
  requiredAuthenticationRefs:
    - kind: ServiceAccount
      name: prometheus
      namespace: linkerd-viz
---
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  name: proxy-probes
  labels:
    linkerd.io/extension: viz
spec:
  targetRef:
    group: policy.linkerd.io
    kind: HTTPRoute
    name: proxy-probes
  requiredAuthenticationRefs:
    - kind: NetworkAuthentication
      group: policy.linkerd.io
      name: kubelet
      namespace: linkerd-viz`
)

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
			_, err := os.Stdout.WriteString(allowScrapePolicy)
			return err
		},
	}
}
