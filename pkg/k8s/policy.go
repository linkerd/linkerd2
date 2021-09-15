package k8s

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

// ServerAndAuthorization holds a server name and serverauthorization name.
type ServerAndAuthorization struct {
	Server              string
	ServerAuthorization string
}

type id struct{ name, namespace string }

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

// ServerAuthorizationsForResource returns a list of Server-ServerAuthorization
// pairs which select pods belonging to the given resource.
func ServerAuthorizationsForResource(ctx context.Context, k8sAPI *KubernetesAPI, namespace string, resource string) ([]ServerAndAuthorization, error) {
	pods, err := getPodsForResourceOrKind(ctx, k8sAPI, namespace, resource)
	if err != nil {
		return nil, err
	}
	podSet := make(map[id]struct{})
	for _, pod := range pods {
		podSet[id{pod.Name, pod.Namespace}] = struct{}{}
	}

	results := make([]ServerAndAuthorization, 0)

	sazs, err := k8sAPI.DynamicClient.Resource(sazGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get serverauthorization resources: %s\n", err)
		os.Exit(1)
	}

	for _, saz := range sazs.Items {
		var servers []unstructured.Unstructured

		if name, found, _ := unstructured.NestedString(saz.UnstructuredContent(), "spec", "server", "name"); found {
			server, err := k8sAPI.DynamicClient.Resource(serverGVR).Namespace(saz.GetNamespace()).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get server %s: %s\n", name, err)
				os.Exit(1)
			}
			servers = []unstructured.Unstructured{*server}
		} else if sel, found, _ := unstructured.NestedMap(saz.UnstructuredContent(), "spec", "server", "selector"); found {
			selector := selector(sel)
			serverList, err := k8sAPI.DynamicClient.Resource(serverGVR).Namespace(saz.GetNamespace()).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(&selector)})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get servers: %s\n", err)
				os.Exit(1)
			}
			servers = serverList.Items
		}

		for _, server := range servers {
			if sel, found, _ := unstructured.NestedMap(server.UnstructuredContent(), "spec", "podSelector"); found {
				selector := selector(sel)
				selectedPods, err := k8sAPI.CoreV1().Pods(server.GetNamespace()).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(&selector)})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to get pods: %s\n", err)
					os.Exit(1)
				}
				if serverIncludesPod(server, selectedPods.Items, podSet) {
					results = append(results, ServerAndAuthorization{server.GetName(), saz.GetName()})
				}
			}

		}
	}
	return results, nil
}

// ServersForResource returns a list of Server names of Servers which select pods
// belonging to the given resource.
func ServersForResource(ctx context.Context, k8sAPI *KubernetesAPI, namespace string, resource string) ([]string, error) {
	pods, err := getPodsForResourceOrKind(ctx, k8sAPI, namespace, resource)
	if err != nil {
		return nil, err
	}
	podSet := make(map[id]struct{})
	for _, pod := range pods {
		podSet[id{pod.Name, pod.Namespace}] = struct{}{}
	}

	results := make([]string, 0)

	servers, err := k8sAPI.DynamicClient.Resource(serverGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get serverauthorization resources: %s\n", err)
		os.Exit(1)
	}

	for _, server := range servers.Items {
		if sel, found, _ := unstructured.NestedMap(server.UnstructuredContent(), "spec", "podSelector"); found {
			selector := selector(sel)
			selectedPods, err := k8sAPI.CoreV1().Pods(server.GetNamespace()).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(&selector)})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get pods: %s\n", err)
				os.Exit(1)
			}
			if serverIncludesPod(server, selectedPods.Items, podSet) {
				results = append(results, server.GetName())
			}
		}

	}
	return results, nil
}

// ServerAuthorizationsForServer returns a list of ServerAuthorization names of
// ServerAuthorizations which select the given Server.
func ServerAuthorizationsForServer(ctx context.Context, k8sAPI *KubernetesAPI, namespace string, server string) ([]string, error) {
	results := make([]string, 0)

	sazs, err := k8sAPI.DynamicClient.Resource(sazGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get serverauthorization resources: %s\n", err)
		os.Exit(1)
	}

	for _, saz := range sazs.Items {
		if name, found, _ := unstructured.NestedString(saz.UnstructuredContent(), "spec", "server", "name"); found {
			s, err := k8sAPI.DynamicClient.Resource(serverGVR).Namespace(saz.GetNamespace()).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get server %s: %s\n", name, err)
				os.Exit(1)
			}
			if s.GetName() == server {
				results = append(results, saz.GetName())
			}
		} else if sel, found, _ := unstructured.NestedMap(saz.UnstructuredContent(), "spec", "server", "selector"); found {
			selector := selector(sel)
			serverList, err := k8sAPI.DynamicClient.Resource(serverGVR).Namespace(saz.GetNamespace()).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(&selector)})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get servers: %s\n", err)
				os.Exit(1)
			}
			for _, s := range serverList.Items {
				if s.GetName() == server {
					results = append(results, saz.GetName())
					break
				}
			}
		}
	}
	return results, nil
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
		pods, err := GetPodsFor(ctx, k8sAPI, namespace, resource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
			os.Exit(1)
		}
		return pods, nil
	}
	pods := []corev1.Pod{}

	typ, err := CanonicalResourceNameFromFriendlyName(elems[0])
	if err != nil {
		return nil, fmt.Errorf("invalid resource: %s", resource)
	}
	switch typ {
	case Pod:
		ps, err := k8sAPI.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
			os.Exit(1)
		}
		pods = append(pods, ps.Items...)

	case CronJob:
		jobs, err := k8sAPI.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get cronjobs: %s", err)
			os.Exit(1)
		}
		for _, job := range jobs.Items {
			ps, err := GetPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", CronJob, job.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case DaemonSet:
		dss, err := k8sAPI.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get demonsets: %s", err)
			os.Exit(1)
		}
		for _, ds := range dss.Items {
			ps, err := GetPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", DaemonSet, ds.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case Deployment:
		deploys, err := k8sAPI.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get deployments: %s", err)
			os.Exit(1)
		}
		for _, deploy := range deploys.Items {
			ps, err := GetPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", Deployment, deploy.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case Job:
		jobs, err := k8sAPI.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get jobs: %s", err)
			os.Exit(1)
		}
		for _, job := range jobs.Items {
			ps, err := GetPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", Job, job.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case ReplicaSet:
		rss, err := k8sAPI.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get replicasets: %s", err)
			os.Exit(1)
		}
		for _, rs := range rss.Items {
			ps, err := GetPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", ReplicaSet, rs.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case ReplicationController:
		rcs, err := k8sAPI.CoreV1().ReplicationControllers(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get replicationcontrollers: %s", err)
			os.Exit(1)
		}
		for _, rc := range rcs.Items {
			ps, err := GetPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", ReplicationController, rc.Name))
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
				os.Exit(1)
			}
			pods = append(pods, ps...)
		}

	case StatefulSet:
		sss, err := k8sAPI.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get statefulsets: %s", err)
			os.Exit(1)
		}
		for _, ss := range sss.Items {
			ps, err := GetPodsFor(ctx, k8sAPI, namespace, fmt.Sprintf("%s/%s", StatefulSet, ss.Name))
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
