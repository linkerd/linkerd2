package cmd

import (
	"errors"
	"fmt"

	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/smimetrics"
	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
)

type alphaClientsOptions struct {
	namespace string
}

func newCmdAlphaClients() *cobra.Command {
	options := alphaClientsOptions{
		namespace: corev1.NamespaceDefault,
	}

	clientsCmd := &cobra.Command{
		Use:   "clients [flags] (RESOURCE)",
		Short: "Display client-side traffic stats to a resource.",
		Long: `Display client-side traffic stats to a resource.
		
(RESOURCE) is a named resource of one these kinds:
  * cronjobs
  * daemonsets
  * deployments
  * jobs
  * namespaces
  * pods
  * replicasets
  * replicationcontrollers
  * statefulsets

linkerd alpha clients will return a table of the requested resource's clients
showing traffic metrics to the requested resource such as request rate, success
rate, and latency percentiles.  These values are measured on the client side
and, for example, include network latency.

Examples:
  linkerd alpha clients -n emojivoto deploy/web`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			target, err := util.BuildResource(options.namespace, args[0])
			if err != nil {
				return err
			}
			if _, ok := allowedKinds[target.GetType()]; !ok {
				return fmt.Errorf("%s is not a supported resource type", target.GetType())
			}
			if target.GetName() == "" {
				return errors.New("You must specify a resource name")
			}
			kind, err := k8s.PluralResourceNameFromFriendlyName(target.GetType())
			if err != nil {
				return err
			}

			metrics, err := smimetrics.GetTrafficMetricsEdgesList(k8sAPI, options.namespace, kind, target.GetName(), nil)
			if err != nil {
				return err
			}
			renderTrafficMetricsEdgesList(metrics, stdout, nil, "from")

			return nil
		},
	}

	clientsCmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the specified resource")
	return clientsCmd
}
