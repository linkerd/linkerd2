package k8s

import (
	"context"
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/labels"

	policyv1 "github.com/linkerd/linkerd2/controller/gen/apis/policy/v1alpha1"
	serverv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta1"
	serverauthorizationv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/serverauthorization/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Authorization holds the names of the resources involved in an authorization.
type Authorization struct {
	Route               string
	Server              string
	ServerAuthorization string
	AuthorizationPolicy string
}

// AuthorizationPolicyGVR is the GroupVersionResource for the AuthorizationPolicy resource.
var AuthorizationPolicyGVR = policyv1.SchemeGroupVersion.WithResource("authorizationpolicies")

// HTTPRouteGVR is the GroupVersionResource for the HTTPRoute resource.
var HTTPRouteGVR = policyv1.SchemeGroupVersion.WithResource("httproutes")

// SazGVR is the GroupVersionResource for the ServerAuthorization resource.
var SazGVR = serverauthorizationv1beta1.SchemeGroupVersion.WithResource("serverauthorizations")

// ServerGVR is the GroupVersionResource for the Server resource.
var ServerGVR = serverv1beta1.SchemeGroupVersion.WithResource("servers")

// AuthorizationsForResource returns a list of ServerAuthorizations and
// AuthorizationPolicies which apply to any Server or HttpRoute which select
// pods belonging to the given resource.
func AuthorizationsForResource(ctx context.Context, k8sAPI *KubernetesAPI, namespace string, resource string) ([]Authorization, error) {
	pods, err := getPodsForResourceOrKind(ctx, k8sAPI, namespace, resource, "")
	if err != nil {
		return nil, err
	}

	results := make([]Authorization, 0)

	sazs, err := k8sAPI.L5dCrdClient.ServerauthorizationV1beta1().ServerAuthorizations(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get serverauthorization resources: %s\n", err)
		os.Exit(1)
	}

	for _, saz := range sazs.Items {
		var servers []serverv1beta1.Server

		if saz.Spec.Server.Name != "" {
			server, err := k8sAPI.L5dCrdClient.ServerV1beta1().Servers(saz.GetNamespace()).Get(ctx, saz.Spec.Server.Name, metav1.GetOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "ServerAuthorization/%s targets Server/%s but we failed to get it: %s\n", saz.Name, saz.Spec.Server.Name, err)
				continue
			}
			servers = []serverv1beta1.Server{*server}
		} else if saz.Spec.Server.Selector != nil {
			selector, err := metav1.LabelSelectorAsSelector(saz.Spec.Server.Selector)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to parse Server selector for ServerAuthorization/%s: %s\n", saz.Name, err)
				continue
			}
			serverList, err := k8sAPI.L5dCrdClient.ServerV1beta1().Servers(saz.GetNamespace()).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get Servers for ServerAuthorization/%s: %s\n", saz.Name, err)
				continue
			}
			servers = serverList.Items
		}

		for _, server := range servers {
			if serverIncludesPod(server, pods) {
				results = append(results, Authorization{
					Route:               "",
					Server:              server.GetName(),
					ServerAuthorization: saz.GetName(),
					AuthorizationPolicy: "",
				})
			}
		}
	}

	policies, err := k8sAPI.L5dCrdClient.PolicyV1alpha1().AuthorizationPolicies(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get AuthorizationPolicy resources: %s\n", err)
		os.Exit(1)
	}

	allServersInNamespace := map[string]*serverv1beta1.ServerList{}

	for _, p := range policies.Items {
		target := p.Spec.TargetRef
		if target.Kind == NamespaceKind && target.Group == K8sCoreAPIGroup {
			serverList, ok := allServersInNamespace[p.Namespace]
			if !ok {
				serverList, err = k8sAPI.L5dCrdClient.ServerV1beta1().Servers(p.Namespace).List(ctx, metav1.ListOptions{})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to get Servers for Namespace/%s: %s\n", p.Namespace, err)
					continue
				}

				allServersInNamespace[p.Namespace] = serverList
			}

			for _, server := range serverList.Items {
				if serverIncludesPod(server, pods) {
					results = append(results, Authorization{
						Route:               "",
						Server:              server.GetName(),
						ServerAuthorization: "",
						AuthorizationPolicy: p.GetName(),
					})
				}
			}
		} else if target.Kind == ServerKind && target.Group == PolicyAPIGroup {
			server, err := k8sAPI.L5dCrdClient.ServerV1beta1().Servers(p.Namespace).Get(ctx, string(target.Name), metav1.GetOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "AuthorizationPolicy/%s targets Server/%s but we failed to get it: %s\n", p.Name, target.Name, err)
				continue
			}
			if serverIncludesPod(*server, pods) {
				results = append(results, Authorization{
					Route:               "",
					Server:              server.GetName(),
					ServerAuthorization: "",
					AuthorizationPolicy: p.GetName(),
				})
			}
		} else if target.Kind == HTTPRouteKind && target.Group == PolicyAPIGroup {
			route, err := k8sAPI.L5dCrdClient.PolicyV1alpha1().HTTPRoutes(p.Namespace).Get(ctx, string(target.Name), metav1.GetOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "AuthorizationPolicy/%s targets HTTPRoute/%s but we failed to get it: %s\n", p.Name, target.Name, err)
				continue
			}
			for _, parent := range route.Spec.ParentRefs {
				if parent.Kind != nil && *parent.Kind == ServerKind &&
					parent.Group != nil && *parent.Group == PolicyAPIGroup {
					server, err := k8sAPI.L5dCrdClient.ServerV1beta1().Servers(p.Namespace).Get(ctx, string(parent.Name), metav1.GetOptions{})
					if err != nil {
						fmt.Fprintf(os.Stderr, "HTTPRoute/%s belongs to Server/%s but we failed to get it: %s\n", target.Name, parent.Name, err)
						continue
					}
					if serverIncludesPod(*server, pods) {
						results = append(results, Authorization{
							Route:               route.GetName(),
							Server:              server.GetName(),
							ServerAuthorization: "",
							AuthorizationPolicy: p.GetName(),
						})
					}
				}
			}
		}
	}

	return results, nil
}

// ServersForResource returns a list of Server names of Servers which select pods
// belonging to the given resource.
func ServersForResource(ctx context.Context, k8sAPI *KubernetesAPI, namespace string, resource string, labelSelector string) ([]string, error) {
	pods, err := getPodsForResourceOrKind(ctx, k8sAPI, namespace, resource, labelSelector)
	if err != nil {
		return nil, err
	}

	results := make([]string, 0)

	servers, err := k8sAPI.L5dCrdClient.ServerV1beta1().Servers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get serverauthorization resources: %s\n", err)
		os.Exit(1)
	}

	for _, server := range servers.Items {
		if serverIncludesPod(server, pods) {
			results = append(results, server.GetName())
		}
	}
	return results, nil
}

// ServerAuthorizationsForServer returns a list of ServerAuthorization names of
// ServerAuthorizations which select the given Server.
func ServerAuthorizationsForServer(ctx context.Context, k8sAPI *KubernetesAPI, namespace string, server string) ([]string, error) {
	results := make([]string, 0)

	sazs, err := k8sAPI.L5dCrdClient.ServerauthorizationV1beta1().ServerAuthorizations(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get serverauthorization resources: %s\n", err)
		os.Exit(1)
	}

	for _, saz := range sazs.Items {
		if saz.Spec.Server.Name != "" {
			s, err := k8sAPI.DynamicClient.Resource(ServerGVR).Namespace(saz.GetNamespace()).Get(ctx, saz.Spec.Server.Name, metav1.GetOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get server %s: %s\n", saz.Spec.Server.Name, err)
				os.Exit(1)
			}
			if s.GetName() == server {
				results = append(results, saz.GetName())
			}
		} else if saz.Spec.Server.Selector != nil {
			selector, err := metav1.LabelSelectorAsSelector(saz.Spec.Server.Selector)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get servers: %s\n", err)
				os.Exit(1)
			}
			serverList, err := k8sAPI.L5dCrdClient.ServerV1beta1().Servers(saz.GetNamespace()).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
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

// serverIncludesPod returns true the given server selects any of the given pods
// and that pod uses the server's port.
func serverIncludesPod(server serverv1beta1.Server, pods []corev1.Pod) bool {
	if server.Spec.PodSelector == nil {
		return false
	}

	selector, err := metav1.LabelSelectorAsSelector(server.Spec.PodSelector)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse PodSelector of Server/%s: %s\n", server.Name, err)
		return false
	}

	for _, pod := range pods {
		if selector.Matches(labels.Set(pod.Labels)) {
			for _, container := range pod.Spec.Containers {
				for _, p := range container.Ports {
					if server.Spec.Port.IntVal == p.ContainerPort || server.Spec.Port.StrVal == p.Name {
						return true
					}
				}
			}
		}
	}
	return false
}

// getPodsForResourceOrKind is similar to getPodsForResource, but also supports
// querying for all resources of a given kind (i.e. when resource name is unspecified).
func getPodsForResourceOrKind(ctx context.Context, k8sAPI kubernetes.Interface, namespace string, resource string, labelSelector string) ([]corev1.Pod, error) {

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

	selector := metav1.ListOptions{
		LabelSelector: labelSelector,
	}

	switch typ {
	case Pod:
		ps, err := k8sAPI.CoreV1().Pods(namespace).List(ctx, selector)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get pods: %s", err)
			os.Exit(1)
		}
		pods = append(pods, ps.Items...)

	case CronJob:
		jobs, err := k8sAPI.BatchV1().CronJobs(namespace).List(ctx, selector)
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
		dss, err := k8sAPI.AppsV1().DaemonSets(namespace).List(ctx, selector)
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
		deploys, err := k8sAPI.AppsV1().Deployments(namespace).List(ctx, selector)
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
		jobs, err := k8sAPI.BatchV1().Jobs(namespace).List(ctx, selector)
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
		rss, err := k8sAPI.AppsV1().ReplicaSets(namespace).List(ctx, selector)
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
		rcs, err := k8sAPI.CoreV1().ReplicationControllers(namespace).List(ctx, selector)
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
		sss, err := k8sAPI.AppsV1().StatefulSets(namespace).List(ctx, selector)
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
