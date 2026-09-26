package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
)

type metricsOptions struct {
	namespace     string
	pod           string
	obfuscate     bool
	labelSelector string
}

func newMetricsOptions() *metricsOptions {
	return &metricsOptions{
		pod:           "",
		obfuscate:     false,
		labelSelector: "",
	}
}

func newCmdMetrics() *cobra.Command {
	options := newMetricsOptions()

	cmd := &cobra.Command{
		Use:   "proxy-metrics [flags] [(RESOURCE)]",
		Short: "Fetch metrics directly from Linkerd proxies",
		Long: `Fetch metrics directly from Linkerd proxies.

  This command initiates a port-forward to a given pod or set of pods, and
  queries the /metrics endpoint on the Linkerd proxies.

  The RESOURCE argument specifies the target resource to query metrics for:
  (TYPE/NAME). Alternatively, use --selector (-l) to select pods by label
  without specifying a resource name.

  Examples:
  * cronjob/my-cronjob
  * deploy/my-deploy
  * ds/my-daemonset
  * job/my-job
  * po/mypod1
  * rc/my-replication-controller
  * sts/my-statefulset

  Valid resource types include:
  * cronjobs
  * daemonsets
  * deployments
  * jobs
  * pods
  * replicasets
  * replicationcontrollers
  * statefulsets`,
		Example: `  # Get metrics from pod-foo-bar in the default namespace.
  linkerd diagnostics proxy-metrics po/pod-foo-bar

  # Get metrics from the web deployment in the emojivoto namespace.
  linkerd diagnostics proxy-metrics -n emojivoto deploy/web

  # Get metrics from all pods with a given label in the emojivoto namespace.
  linkerd diagnostics proxy-metrics -n emojivoto -l app=web

  # Get metrics from the linkerd-destination pod in the linkerd namespace.
  linkerd diagnostics proxy-metrics -n linkerd $(
    kubectl --namespace linkerd get pod \
      --selector linkerd.io/control-plane-component=destination \
      --output name
  )`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && options.labelSelector == "" {
				return errors.New("must specify a resource or --selector")
			}

			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			var pods []corev1.Pod
			if len(args) == 0 {
				// selector-only mode: list pods matching the label selector
				pods, err = k8s.GetPodsBySelector(cmd.Context(), k8sAPI, options.namespace, options.labelSelector)
			} else {
				pods, err = k8s.GetPodsFor(cmd.Context(), k8sAPI, options.namespace, args[0])
			}
			if err != nil {
				return err
			}

			results := getMetrics(k8sAPI, pods, k8s.ProxyAdminPortName, 30*time.Second, verbose)

			var buf bytes.Buffer
			for i, result := range results {
				content := fmt.Sprintf("#\n# POD %s (%d of %d)\n#\n", result.pod, i+1, len(results))
				switch {
				case result.err != nil:
					content += fmt.Sprintf("# ERROR: %s\n", result.err)
				case options.obfuscate:
					obfuscatedMetrics, err := obfuscateMetrics(result.metrics)
					if err != nil {
						content += fmt.Sprintf("# ERROR %s\n", err)
					} else {
						content += string(obfuscatedMetrics)
					}
				default:
					content += string(result.metrics)
				}

				buf.WriteString(content)
			}
			fmt.Printf("%s", buf.String())

			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of resource")
	cmd.PersistentFlags().BoolVar(&options.obfuscate, "obfuscate", options.obfuscate, "Obfuscate sensitive information")
	cmd.PersistentFlags().StringVarP(&options.labelSelector, "selector", "l", options.labelSelector, "Selector (label query) to filter on, supports '=', '==', and '!=")

	pkgcmd.ConfigureNamespaceFlagCompletion(cmd, []string{"namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)

	return cmd
}
