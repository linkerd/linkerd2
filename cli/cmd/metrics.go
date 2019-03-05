package cmd

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
)

type metricsOptions struct {
	namespace string
	pod       string
}

func newMetricsOptions() *metricsOptions {
	return &metricsOptions{
		namespace: "default",
		pod:       "",
	}
}

func newCmdMetrics() *cobra.Command {
	options := newMetricsOptions()

	cmd := &cobra.Command{
		Use:   "metrics [flags] (POD)",
		Short: "Fetch metrics from a specific Linkerd proxy",
		Long: `Fetch metrics from a specific Linkerd proxy.

  This command initiates a port-forward to a given pod, and queries the /metrics
  endpoint on the Linkerd proxy running in that pod.`,
		Example: `  # Get metrics from pod-foo-bar in the default namespace.
  linkerd metrics pod-foo-bar

  # Get metrics from the linkerd-controller pod in the linkerd namespace.
  linkerd metrics -n linkerd $(
    kubectl --namespace linkerd get pod \
      --selector linkerd.io/control-plane-component=controller \
      --output jsonpath='{.items[*].metadata.name}'
  )`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := k8s.GetConfig(kubeconfigPath, kubeContext)
			if err != nil {
				return err
			}

			clientset, err := kubernetes.NewForConfig(config)
			if err != nil {
				return err
			}

			portforward, err := k8s.NewProxyMetricsForward(
				config,
				clientset,
				options.namespace,
				args[0],
				verbose,
			)
			if err != nil {
				return err
			}

			defer portforward.Stop()

			go func() {
				err := portforward.Run()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error running port-forward: %s", err)
					os.Exit(1)
				}
			}()

			<-portforward.Ready()

			metricsURL := portforward.URLFor("/metrics")
			resp, err := http.Get(metricsURL)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			bytes, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			fmt.Printf("%s", bytes)

			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of pod")

	return cmd
}
