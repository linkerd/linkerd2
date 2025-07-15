package cmd

import (
	"bytes"
	"text/template"

	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
)

const (
	allowScrapePolicy = `---
apiVersion: policy.linkerd.io/v1beta3
kind: Server
metadata:
  name: proxy-admin
  namespace: {{ .TargetNs }}
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
  namespace: {{ .TargetNs }}
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
  namespace: {{ .TargetNs }}
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
  namespace: {{ .TargetNs }}
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
  namespace: {{ .TargetNs }}
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

type templateOptions struct {
	ChartName     string
	Version       string
	ExtensionName string
	VizNs         string
	TargetNs      string
}

// newCmdAllowScrapes creates a new cobra command `allow-scrapes`
func newCmdAllowScrapes() *cobra.Command {
	output := "yaml"
	options := templateOptions{
		ExtensionName: ExtensionName,
		ChartName:     vizChartName,
		Version:       version.Version,
		VizNs:         defaultNamespace,
	}
	cmd := &cobra.Command{
		Use:   "allow-scrapes {-n | --namespace } namespace",
		Short: "Output Kubernetes resources to authorize Prometheus scrapes",
		Long:  `Output Kubernetes resources to authorize Prometheus scrapes in a namespace or cluster with config.linkerd.io/default-inbound-policy: deny.`,
		Example: `# Allow scrapes in the 'emojivoto' namespace
linkerd viz allow-scrapes --namespace emojivoto | kubectl apply -f -`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return cmd.MarkFlagRequired("namespace")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			t := template.Must(template.New("allow-scrapes").Parse(allowScrapePolicy))
			var buf bytes.Buffer
			err := t.Execute(&buf, options)
			if err != nil {
				return err
			}

			return pkgcmd.RenderYAMLAs(&buf, stdout, output)
		},
	}
	cmd.Flags().StringVarP(&options.TargetNs, "namespace", "n", options.TargetNs, "The namespace in which to authorize Prometheus scrapes.")
	cmd.Flags().StringVarP(&output, "output", "o", output, "Output format. One of: json|yaml")

	pkgcmd.ConfigureNamespaceFlagCompletion(
		cmd, []string{"n", "namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)
	return cmd
}
