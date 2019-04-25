package k8s

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/linkerd/linkerd2/pkg/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var minAPIVersion = [3]int{1, 10, 0}

// KubernetesAPI provides a client for accessing a Kubernetes cluster.
type KubernetesAPI struct {
	*rest.Config
	kubernetes.Interface
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

// URLFor generates a URL based on the Kubernetes config.
func (kubeAPI *KubernetesAPI) URLFor(namespace, path string) (*url.URL, error) {
	return generateKubernetesAPIURLFor(kubeAPI.Host, namespace, path)
}

// NewAPI validates a Kubernetes config and returns a client for accessing the
// configured cluster.
func NewAPI(configPath, kubeContext string, timeout time.Duration) (*KubernetesAPI, error) {
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

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API clientset: %v", err)
	}

	return &KubernetesAPI{
		Config:    config,
		Interface: clientset,
	}, nil
}

// GetReplicaSets returns all replicasets in a given namespace
func (kubeAPI *KubernetesAPI) GetReplicaSets(namespace string) ([]appsv1.ReplicaSet, error) {
	replicaSetList, err := kubeAPI.AppsV1().ReplicaSets(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return replicaSetList.Items, nil
}
