package k8s

import (
	"fmt"
	"net/http"
	"time"

	tsclient "github.com/deislabs/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var minAPIVersion = [3]int{1, 12, 0}

// KubernetesAPI provides a client for accessing a Kubernetes cluster.
// TODO: support ServiceProfile ClientSet. A prerequisite is moving the
// ServiceProfile client code from `./controller` to `./pkg` (#2751). This will
// also allow making `NewFakeClientSets` private, as KubernetesAPI will support
// all relevant k8s resources.
type KubernetesAPI struct {
	*rest.Config
	kubernetes.Interface
	Apiextensions apiextensionsclient.Interface // for CRDs
	TsClient      tsclient.Interface
}

// NewAPI validates a Kubernetes config and returns a client for accessing the
// configured cluster.
func NewAPI(configPath, kubeContext string, impersonate string, timeout time.Duration) (*KubernetesAPI, error) {
	config, err := GetConfig(configPath, kubeContext)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API client: %v", err)
	}

	// k8s' client-go doesn't support injecting context
	// https://github.com/kubernetes/kubernetes/issues/46503
	// but we can set the timeout manually
	config.Timeout = timeout
	wt := config.WrapTransport
	config.WrapTransport = prometheus.ClientWithTelemetry("k8s", wt)

	if impersonate != "" {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: impersonate,
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API clientset: %v", err)
	}
	apiextensions, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API Extensions clientset: %v", err)
	}
	tsClient, err := tsclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &KubernetesAPI{
		Config:        config,
		Interface:     clientset,
		Apiextensions: apiextensions,
		TsClient:      tsClient,
	}, nil
}

// NewClient returns an http.Client configured with a Transport to connect to
// the Kubernetes cluster.
func (kubeAPI *KubernetesAPI) NewClient() (*http.Client, error) {
	secureTransport, err := rest.TransportFor(kubeAPI.Config)
	if err != nil {
		return nil, fmt.Errorf("error instantiating Kubernetes API client: %v", err)
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
func (kubeAPI *KubernetesAPI) NamespaceExists(namespace string) (bool, error) {
	ns, err := kubeAPI.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return ns != nil, nil
}

// GetPodsByNamespace returns all pods in a given namespace
func (kubeAPI *KubernetesAPI) GetPodsByNamespace(namespace string) ([]corev1.Pod, error) {
	podList, err := kubeAPI.CoreV1().Pods(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return podList.Items, nil
}

// GetReplicaSets returns all replicasets in a given namespace
func (kubeAPI *KubernetesAPI) GetReplicaSets(namespace string) ([]appsv1.ReplicaSet, error) {
	replicaSetList, err := kubeAPI.AppsV1().ReplicaSets(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return replicaSetList.Items, nil
}

// GetPodStatus receives a pod and returns the pod status, based on `kubectl` logic.
// This logic is imported and adapted from the github.com/kubernetes/kubernetes project:
// https://github.com/kubernetes/kubernetes/blob/33a3e325f754d179b25558dee116fca1c67d353a/pkg/printers/internalversion/printers.go#L558-L640
func GetPodStatus(pod corev1.Pod) string {
	reason := string(pod.Status.Phase)
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason
	}

	initializing := false
	for i := range pod.Status.InitContainerStatuses {
		container := pod.Status.InitContainerStatuses[i]
		switch {
		case container.State.Terminated != nil && container.State.Terminated.ExitCode == 0 && container.State.Terminated.Signal == 0:
			continue
		case container.State.Terminated != nil:
			// initialization is failed
			if len(container.State.Terminated.Reason) == 0 {
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
