package k8s

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	crdclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	apiregistration "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var minAPIVersion = [3]int{1, 23, 0}

// KubernetesAPI provides a client for accessing a Kubernetes cluster.
// TODO: support ServiceProfile ClientSet. A prerequisite is moving the
// ServiceProfile client code from `./controller` to `./pkg` (#2751). This will
// also allow making `NewFakeClientSets` private, as KubernetesAPI will support
// all relevant k8s resources.
type KubernetesAPI struct {
	*rest.Config
	kubernetes.Interface
	Apiextensions   apiextensionsclient.Interface // for CRDs
	Apiregistration apiregistration.Interface     // for access to APIService
	DynamicClient   dynamic.Interface
	L5dCrdClient    crdclient.Interface
}

// NewAPI validates a Kubernetes config and returns a client for accessing the
// configured cluster.
func NewAPI(configPath, kubeContext string, impersonate string, impersonateGroup []string, timeout time.Duration) (*KubernetesAPI, error) {
	config, err := GetConfig(configPath, kubeContext)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API client: %w", err)
	}
	return NewAPIForConfig(config, impersonate, impersonateGroup, timeout, 0, 0)
}

// NewAPIForConfig uses a Kubernetes config to construct a client for accessing
// the configured cluster
func NewAPIForConfig(
	config *rest.Config,
	impersonate string,
	impersonateGroup []string,
	timeout time.Duration,
	qps float32,
	burst int,
) (*KubernetesAPI, error) {

	// k8s' client-go doesn't support injecting context
	// https://github.com/kubernetes/kubernetes/issues/46503
	// but we can set the timeout manually
	config.Timeout = timeout
	if qps > 0 && burst > 0 {
		config.QPS = qps
		config.Burst = burst
		prometheus.SetClientQPS("k8s", config.QPS)
		prometheus.SetClientBurst("k8s", config.Burst)
	} else {
		prometheus.SetClientQPS("k8s", rest.DefaultQPS)
		prometheus.SetClientBurst("k8s", rest.DefaultBurst)
	}
	wt := config.WrapTransport
	config.WrapTransport = prometheus.ClientWithTelemetry("k8s", wt)

	if impersonate != "" {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: impersonate,
			Groups:   impersonateGroup,
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API clientset: %w", err)
	}
	apiextensions, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API Extensions clientset: %w", err)
	}
	aggregatorClient, err := apiregistration.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API server aggregator: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes Dynamic Client: %w", err)
	}

	l5dCrdClient, err := crdclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error configuring Linkerd CRD clientset: %w", err)
	}

	return &KubernetesAPI{
		Config:          config,
		Interface:       clientset,
		Apiextensions:   apiextensions,
		Apiregistration: aggregatorClient,
		DynamicClient:   dynamicClient,
		L5dCrdClient:    l5dCrdClient,
	}, nil
}

// NewClient returns an http.Client configured with a Transport to connect to
// the Kubernetes cluster.
func (kubeAPI *KubernetesAPI) NewClient() (*http.Client, error) {
	secureTransport, err := rest.TransportFor(kubeAPI.Config)
	if err != nil {
		return nil, fmt.Errorf("error instantiating Kubernetes API client: %w", err)
	}

	return &http.Client{
		Transport: secureTransport,
	}, nil
}

// GetVersionInfo returns version.Info for the Kubernetes cluster.
func (kubeAPI *KubernetesAPI) GetVersionInfo() (*version.Info, error) {
	return kubeAPI.Discovery().ServerVersion()
}

// CheckVersion validates whether the configured Kubernetes cluster's version is
// running a minimum Kubernetes API version.
func (kubeAPI *KubernetesAPI) CheckVersion(versionInfo *version.Info) error {
	apiVersion, err := getK8sVersion(versionInfo.String())
	if err != nil {
		return err
	}

	if !isCompatibleVersion(minAPIVersion, apiVersion) {
		return fmt.Errorf("Kubernetes is on version [%d.%d.%d], but version [%d.%d.%d] or more recent is required",
			apiVersion[0], apiVersion[1], apiVersion[2],
			minAPIVersion[0], minAPIVersion[1], minAPIVersion[2])
	}

	return nil
}

// NamespaceExists validates whether a given namespace exists.
func (kubeAPI *KubernetesAPI) NamespaceExists(ctx context.Context, namespace string) (bool, error) {
	ns, err := kubeAPI.GetNamespace(ctx, namespace)
	if kerrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return ns != nil, nil
}

// GetNamespace returns the namespace with a given name, if one exists.
func (kubeAPI *KubernetesAPI) GetNamespace(ctx context.Context, namespace string) (*corev1.Namespace, error) {
	return kubeAPI.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
}

// GetNodes returns all the nodes in a cluster.
func (kubeAPI *KubernetesAPI) GetNodes(ctx context.Context) ([]corev1.Node, error) {
	nodes, err := kubeAPI.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return nodes.Items, nil
}

// GetPodsByNamespace returns all pods in a given namespace
func (kubeAPI *KubernetesAPI) GetPodsByNamespace(ctx context.Context, namespace string) ([]corev1.Pod, error) {
	podList, err := kubeAPI.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return podList.Items, nil
}

// GetReplicaSets returns all replicasets in a given namespace
func (kubeAPI *KubernetesAPI) GetReplicaSets(ctx context.Context, namespace string) ([]appsv1.ReplicaSet, error) {
	replicaSetList, err := kubeAPI.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return replicaSetList.Items, nil
}

// GetAllNamespacesWithExtensionLabel gets all namespaces with the linkerd.io/extension label key
func (kubeAPI *KubernetesAPI) GetAllNamespacesWithExtensionLabel(ctx context.Context) ([]corev1.Namespace, error) {
	namespaces, err := kubeAPI.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: LinkerdExtensionLabel})
	if err != nil {
		return nil, err
	}

	return namespaces.Items, nil
}

// GetNamespaceWithExtensionLabel gets the namespace with the LinkerdExtensionLabel label value of `value`
func (kubeAPI *KubernetesAPI) GetNamespaceWithExtensionLabel(ctx context.Context, value string) (*corev1.Namespace, error) {
	namespaces, err := kubeAPI.GetAllNamespacesWithExtensionLabel(ctx)
	if err != nil {
		return nil, err
	}

	for _, ns := range namespaces {
		if ns.Labels[LinkerdExtensionLabel] == value {
			return &ns, err
		}
	}
	errNotFound := kerrors.NewNotFound(corev1.Resource("namespace"), value)
	errNotFound.ErrStatus.Message = fmt.Sprintf("namespace with label \"%s: %s\" not found", LinkerdExtensionLabel, value)
	return nil, errNotFound
}

// GetPodStatus receives a pod and returns the pod status, based on `kubectl` logic.
// This logic is imported and adapted from the github.com/kubernetes/kubernetes project:
// https://github.com/kubernetes/kubernetes/blob/v1.31.0-alpha.0/pkg/printers/internalversion/printers.go#L860
func GetPodStatus(pod corev1.Pod) string {
	reason := string(pod.Status.Phase)
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason
	}

	initContainers := make(map[string]*corev1.Container)
	for i := range pod.Spec.InitContainers {
		initContainers[pod.Spec.InitContainers[i].Name] = &pod.Spec.InitContainers[i]
	}

	initializing := false
	for i := range pod.Status.InitContainerStatuses {
		container := pod.Status.InitContainerStatuses[i]
		switch {
		case container.State.Terminated != nil && container.State.Terminated.ExitCode == 0 && container.State.Terminated.Signal == 0:
			continue
		case isRestartableInitContainer(initContainers[container.Name]) &&
			container.Started != nil && *container.Started && container.Ready:
			continue
		case container.State.Terminated != nil:
			// initialization is failed
			if container.State.Terminated.Reason == "" {
				if container.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Init:Signal:%d", container.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("Init:ExitCode:%d", container.State.Terminated.ExitCode)
				}
			} else {
				reason = "Init:" + container.State.Terminated.Reason
			}
			initializing = true
		case container.State.Waiting != nil && len(container.State.Waiting.Reason) > 0 && container.State.Waiting.Reason != "PodInitializing":
			reason = "Init:" + container.State.Waiting.Reason
			initializing = true
		default:
			reason = fmt.Sprintf("Init:%d/%d", i, len(pod.Spec.InitContainers))
			initializing = true
		}
		break
	}
	if !initializing {
		hasRunning := false
		for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
			container := pod.Status.ContainerStatuses[i]

			if container.State.Waiting != nil && container.State.Waiting.Reason != "" {
				reason = container.State.Waiting.Reason
			} else if container.State.Terminated != nil && container.State.Terminated.Reason != "" {
				reason = container.State.Terminated.Reason
			} else if container.State.Terminated != nil && container.State.Terminated.Reason == "" {
				if container.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Signal:%d", container.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("ExitCode:%d", container.State.Terminated.ExitCode)
				}
			} else if container.Ready && container.State.Running != nil {
				hasRunning = true
			}
		}

		// change pod status back to "Running" if there is at least one container still reporting as "Running" status
		if reason == "Completed" && hasRunning {
			reason = "Running"
		}
	}

	return reason
}

// Borrowed from
// https://github.com/kubernetes/kubernetes/blob/v1.31.0-alpha.0/pkg/printers/internalversion/printers.go#L3209
func isRestartableInitContainer(initContainer *corev1.Container) bool {
	if initContainer.RestartPolicy == nil {
		return false
	}

	return *initContainer.RestartPolicy == corev1.ContainerRestartPolicyAlways
}

// GetProxyReady returns true if the pod contains a proxy that is ready
func GetProxyReady(pod corev1.Pod) bool {
	statuses := append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...)
	for _, container := range statuses {
		if container.Name == ProxyContainerName {
			return container.Ready
		}
	}
	return false
}

// GetProxyVersion returns the container proxy's version, if any
func GetProxyVersion(pod corev1.Pod) string {
	containers := append(pod.Spec.InitContainers, pod.Spec.Containers...)
	for _, container := range containers {
		if container.Name == ProxyContainerName {
			if strings.Contains(container.Image, "@") {
				// Proxy container image is specified with digest instead of
				// tag. We are unable to determine version.
				return ""
			}
			parts := strings.Split(container.Image, ":")
			return parts[len(parts)-1]
		}
	}
	return ""
}

// GetPodsFor takes a resource string, queries the Kubernetes API, and returns a
// list of pods belonging to that resource.
func GetPodsFor(ctx context.Context, clientset kubernetes.Interface, namespace string, resource string) ([]corev1.Pod, error) {
	elems := strings.Split(resource, "/")

	if len(elems) == 1 {
		return nil, errors.New("no resource name provided")
	}

	if len(elems) != 2 {
		return nil, fmt.Errorf("invalid resource string: %s", resource)
	}

	typ, err := CanonicalResourceNameFromFriendlyName(elems[0])
	if err != nil {
		return nil, err
	}
	name := elems[1]

	// special case if a single pod was specified
	if typ == Pod {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return []corev1.Pod{*pod}, nil
	}

	var matchLabels map[string]string
	var ownerUID types.UID
	switch typ {
	case CronJob:
		jobs, err := clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}

		var pods []corev1.Pod
		for _, job := range jobs.Items {
			if isOwner(job.GetUID(), job.GetOwnerReferences()) {
				jobPods, err := GetPodsFor(ctx, clientset, namespace, fmt.Sprintf("%s/%s", Job, job.GetName()))
				if err != nil {
					return nil, err
				}
				pods = append(pods, jobPods...)
			}
		}
		return pods, nil

	case DaemonSet:
		ds, err := clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = ds.Spec.Selector.MatchLabels
		ownerUID = ds.GetUID()

	case Deployment:
		deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = deployment.Spec.Selector.MatchLabels
		ownerUID = deployment.GetUID()

		replicaSets, err := clientset.AppsV1().ReplicaSets(namespace).List(
			ctx,
			metav1.ListOptions{
				LabelSelector: labels.Set(matchLabels).AsSelector().String(),
			},
		)
		if err != nil {
			return nil, err
		}

		var pods []corev1.Pod
		for _, rs := range replicaSets.Items {
			if isOwner(ownerUID, rs.GetOwnerReferences()) {
				podsRS, err := GetPodsFor(ctx, clientset, namespace, fmt.Sprintf("%s/%s", ReplicaSet, rs.GetName()))
				if err != nil {
					return nil, err
				}
				pods = append(pods, podsRS...)
			}
		}
		return pods, nil

	case Job:
		job, err := clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = job.Spec.Selector.MatchLabels
		ownerUID = job.GetUID()

	case ReplicaSet:
		rs, err := clientset.AppsV1().ReplicaSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = rs.Spec.Selector.MatchLabels
		ownerUID = rs.GetUID()

	case ReplicationController:
		rc, err := clientset.CoreV1().ReplicationControllers(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = rc.Spec.Selector
		ownerUID = rc.GetUID()

	case StatefulSet:
		ss, err := clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = ss.Spec.Selector.MatchLabels
		ownerUID = ss.GetUID()

	default:
		return nil, fmt.Errorf("unsupported resource type: %s", name)
	}

	podList, err := clientset.
		CoreV1().
		Pods(namespace).
		List(
			ctx,
			metav1.ListOptions{
				LabelSelector: labels.Set(matchLabels).AsSelector().String(),
			},
		)
	if err != nil {
		return nil, err
	}

	if ownerUID == "" {
		return podList.Items, nil
	}

	pods := []corev1.Pod{}
	for _, pod := range podList.Items {
		if isOwner(ownerUID, pod.GetOwnerReferences()) {
			pods = append(pods, pod)
		}
	}

	return pods, nil
}

func isOwner(u types.UID, ownerRefs []metav1.OwnerReference) bool {
	for _, or := range ownerRefs {
		if u == or.UID {
			return true
		}
	}
	return false
}
