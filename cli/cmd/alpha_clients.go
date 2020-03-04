package cmd

import (
	"fmt"

	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/smimetrics"
	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type alphaClientsOptions struct {
	namespace     string
	allNamespaces bool
}

func newCmdAlphaClients() *cobra.Command {
	options := alphaClientsOptions{
		namespace: "default",
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
  * pods
  * replicasets
  * replicationcontrollers
  * statefulsets

linkerd alpha clients will return a table of the requested resource's clients
showing traffic metrics metrics to the requested resource such as request rate,
success rate, and latency percentiles.  These values are measured on the client
side and, for example, include network latency.

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
			kind, err := k8s.PluralResourceNameFromFriendlyName(target.GetType())
			if err != nil {
				return err
			}
			clientNs := options.namespace
			if options.allNamespaces {
				clientNs = ""
			}

			clients, err := getAllResourceNamesOfKind(k8sAPI, clientNs, target.GetType())
			if err != nil {
				return err
			}

			metricsList := smimetrics.TrafficMetricsList{
				Items: []smimetrics.TrafficMetrics{},
			}

			for _, c := range clients {
				metrics, err := smimetrics.GetTrafficMetricsEdgesList(k8sAPI, c.GetNamespace(), kind, c.GetName(), nil)
				if err != nil {
					continue
				}
				metricsList.Items = append(metricsList.Items, metrics.Items...)
			}
			renderTrafficMetricsEdgesList(&metricsList, stdout, &target)

			return nil
		},
	}

	clientsCmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the specified resource")
	clientsCmd.PersistentFlags().BoolVarP(&options.allNamespaces, "all-namespaces", "A", options.allNamespaces, "If present, returns stats from clients in all namespaces")

	return clientsCmd
}

func getAllResourceNamesOfKind(k8sAPI *k8s.KubernetesAPI, namespace, kind string) ([]metav1.ObjectMeta, error) {
	names := []metav1.ObjectMeta{}
	switch kind {
	case k8s.CronJob:
		list, err := k8sAPI.BatchV2alpha1().CronJobs(namespace).List(metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, i := range list.Items {
			names = append(names, i.ObjectMeta)
		}
	case k8s.DaemonSet:
		list, err := k8sAPI.AppsV1().DaemonSets(namespace).List(metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, i := range list.Items {
			names = append(names, i.ObjectMeta)
		}
	case k8s.Deployment:
		list, err := k8sAPI.AppsV1().Deployments(namespace).List(metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, i := range list.Items {
			names = append(names, i.ObjectMeta)
		}
	case k8s.Job:
		list, err := k8sAPI.BatchV1().Jobs(namespace).List(metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, i := range list.Items {
			names = append(names, i.ObjectMeta)
		}
	case k8s.Pod:
		list, err := k8sAPI.CoreV1().Pods(namespace).List(metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, i := range list.Items {
			names = append(names, i.ObjectMeta)
		}
	case k8s.ReplicaSet:
		list, err := k8sAPI.AppsV1().ReplicaSets(namespace).List(metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, i := range list.Items {
			names = append(names, i.ObjectMeta)
		}
	case k8s.ReplicationController:
		list, err := k8sAPI.CoreV1().ReplicationControllers(namespace).List(metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, i := range list.Items {
			names = append(names, i.ObjectMeta)
		}
	case k8s.StatefulSet:
		list, err := k8sAPI.AppsV1().StatefulSets(namespace).List(metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, i := range list.Items {
			names = append(names, i.ObjectMeta)
		}
	}
	return names, nil
}
