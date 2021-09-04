package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/cli/table"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

var sazGVR = schema.GroupVersionResource{
	Group:    "policy.linkerd.io",
	Version:  "v1alpha1",
	Resource: "serverauthorizations",
}

var serverGVR = schema.GroupVersionResource{
	Group:    "policy.linkerd.io",
	Version:  "v1alpha1",
	Resource: "servers",
}

type id struct{ name, namespace string }

func newCmdAuthz() *cobra.Command {

	var namespace string

	cmd := &cobra.Command{
		Use:   "authz [flags] resource",
		Short: "List server authorizations for a resource",
		Long:  "List server authorizations for a resource.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			if namespace == "" {
				namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			pods, err := getPodsForResourceOrKind(cmd.Context(), k8sAPI, namespace, args[0])
			if err != nil {
				return err
			}
			podSet := make(map[id]struct{})
			for _, pod := range pods {
				podSet[id{pod.Name, pod.Namespace}] = struct{}{}
			}

			rows := make([]table.Row, 0)

			sazs, err := k8sAPI.DynamicClient.Resource(sazGVR).Namespace(namespace).List(cmd.Context(), metav1.ListOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get server resources: %s\n", err)
				os.Exit(1)
			}

			for _, saz := range sazs.Items {
				var servers []unstructured.Unstructured

				if name, found, _ := unstructured.NestedString(saz.UnstructuredContent(), "spec", "server", "name"); found {
					server, err := k8sAPI.DynamicClient.Resource(serverGVR).Namespace(saz.GetNamespace()).Get(cmd.Context(), name, metav1.GetOptions{})
					if err != nil {
						fmt.Fprintf(os.Stderr, "Failed to get server %s: %s\n", name, err)
						os.Exit(1)
					}
					servers = []unstructured.Unstructured{*server}
				} else if sel, found, _ := unstructured.NestedMap(saz.UnstructuredContent(), "spec", "server", "selector"); found {
					selector := selector(sel)
					serverList, err := k8sAPI.DynamicClient.Resource(serverGVR).Namespace(saz.GetNamespace()).List(cmd.Context(), metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(&selector)})
					if err != nil {
						fmt.Fprintf(os.Stderr, "Failed to get servers: %s\n", err)
						os.Exit(1)
					}
					servers = serverList.Items
				}

				for _, server := range servers {
					if sel, found, _ := unstructured.NestedMap(server.UnstructuredContent(), "spec", "podSelector"); found {
						selector := selector(sel)
						selectedPods, err := k8sAPI.CoreV1().Pods(server.GetNamespace()).List(cmd.Context(), metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(&selector)})
						if err != nil {
							fmt.Fprintf(os.Stderr, "Failed to get pods: %s\n", err)
							os.Exit(1)
						}
						if serverIncludesPod(server, selectedPods.Items, podSet) {
							rows = append(rows, table.Row{server.GetName(), saz.GetName()})
						}
					}

				}

			}

			cols := []table.Column{
				{Header: "SERVER", Width: 6, Flexible: true},
				{Header: "AUTHORIZATION", Width: 13, Flexible: true},
			}

			table := table.NewTable(cols, rows)
			table.Render(os.Stdout)

			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Namespace of resource")

	pkgcmd.ConfigureNamespaceFlagCompletion(cmd, []string{"namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)
	return cmd
}

func selector(selector map[string]interface{}) metav1.LabelSelector {
	if labels, found, err := unstructured.NestedStringMap(selector, "matchLabels"); found && err == nil {
		return metav1.LabelSelector{MatchLabels: labels}
	}
	if expressions, found, err := unstructured.NestedSlice(selector, "matchExpressions"); found && err == nil {
		exprs := make([]metav1.LabelSelectorRequirement, len(expressions))
		for i, expr := range expressions {
			exprs[i] = matchExpression(expr)
		}
		return metav1.LabelSelector{MatchExpressions: exprs}
	}
	return metav1.LabelSelector{}
}

func matchExpression(expr interface{}) metav1.LabelSelectorRequirement {
	if exprMap, ok := expr.(map[string]interface{}); ok {
		if key, found, err := unstructured.NestedString(exprMap, "key"); found && err == nil {
			if op, found, err := unstructured.NestedString(exprMap, "operator"); found && err == nil {
				if values, found, err := unstructured.NestedStringSlice(exprMap, "values"); found && err == nil {
					return metav1.LabelSelectorRequirement{
						Key:      key,
						Operator: metav1.LabelSelectorOperator(op),
						Values:   values,
					}
				}
			}
		}
	}
	return metav1.LabelSelectorRequirement{}
}

func serverIncludesPod(server unstructured.Unstructured, serverPods []corev1.Pod, podSet map[id]struct{}) bool {
	for _, pod := range serverPods {
		if _, ok := podSet[id{pod.Name, pod.Namespace}]; ok {
			if port, found, err := unstructured.NestedInt64(server.UnstructuredContent(), "spec", "port"); found && err == nil {
				for _, container := range pod.Spec.Containers {
					for _, p := range container.Ports {
						if int32(port) == p.ContainerPort {
							return true
						}
					}
				}
			}
			if port, found, err := unstructured.NestedString(server.UnstructuredContent(), "spec", "port"); found && err == nil {
				for _, container := range pod.Spec.Containers {
					for _, p := range container.Ports {
						if port == p.Name {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// getPodsForResourceOrKind is similar to getPodsForResource, but also supports
// querying for all resources of a given kind (i.e. when resource name is unspecified).
func getPodsForResourceOrKind(ctx context.Context, k8sAPI kubernetes.Interface, namespace string, resource string) ([]corev1.Pod, error) {

	elems := strings.Split(resource, "/")
	if len(elems) > 2 {
		return nil, fmt.Errorf("invalid resource: %s", resource)
	}
	if len(elems) == 2 {
		pods, err := getPodsFor(ctx, k8sAPI, namespace, resource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
			os.Exit(1)
		}
		return pods, nil
	}
	pods := []corev1.Pod{}

	typ, err := k8s.CanonicalResourceNameFromFriendlyName(elems[0])
	if err != nil {
		return nil, fmt.Errorf("invalid resource: %s", resource)
	}
	switch typ {
	case k8s.CronJob:
		jobs, err := k8sAPI.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get cronjobs: %s", err)
			os.Exit(1)
		}
		for _, job := range jobs.Items {
			ps, err := getPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", k8s.CronJob, job.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case k8s.DaemonSet:
		dss, err := k8sAPI.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get demonsets: %s", err)
			os.Exit(1)
		}
		for _, ds := range dss.Items {
			ps, err := getPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", k8s.DaemonSet, ds.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case k8s.Deployment:
		deploys, err := k8sAPI.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get deployments: %s", err)
			os.Exit(1)
		}
		for _, deploy := range deploys.Items {
			ps, err := getPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", k8s.Deployment, deploy.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case k8s.Job:
		jobs, err := k8sAPI.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get jobs: %s", err)
			os.Exit(1)
		}
		for _, job := range jobs.Items {
			ps, err := getPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", k8s.Job, job.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case k8s.ReplicaSet:
		rss, err := k8sAPI.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get replicasets: %s", err)
			os.Exit(1)
		}
		for _, rs := range rss.Items {
			ps, err := getPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", k8s.ReplicaSet, rs.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case k8s.ReplicationController:
		rcs, err := k8sAPI.CoreV1().ReplicationControllers(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get replicationcontrollers: %s", err)
			os.Exit(1)
		}
		for _, rc := range rcs.Items {
			ps, err := getPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", k8s.ReplicationController, rc.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case k8s.StatefulSet:
		sss, err := k8sAPI.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get statefulsets: %s", err)
			os.Exit(1)
		}
		for _, ss := range sss.Items {
			ps, err := getPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", k8s.StatefulSet, ss.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	default:
		return nil, fmt.Errorf("unsupported resource type: %s", typ)
	}
	return pods, nil
}
