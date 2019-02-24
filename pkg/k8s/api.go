package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/rest"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var minAPIVersion = [3]int{1, 10, 0}

// KubernetesAPI provides a client for accessing a Kubernetes cluster.
type KubernetesAPI struct {
	*rest.Config
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
func (kubeAPI *KubernetesAPI) GetVersionInfo(ctx context.Context, client rest.HTTPClient) (*version.Info, error) {
	rsp, err := kubeAPI.getRequest(ctx, client, "/version")
	if err != nil {
		return nil, err
	}
	defer rsp.Body.Close()

	if rsp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Unexpected Kubernetes API response: %s", rsp.Status)
	}

	bytes, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		return nil, err
	}

	var versionInfo version.Info
	err = json.Unmarshal(bytes, &versionInfo)
	return &versionInfo, err
}

// CheckVersion validates whether the configured Kubernetes cluster's version is
// running a minimum Kubernetes API version.
func (kubeAPI *KubernetesAPI) CheckVersion(versionInfo string) error {
	apiVersion, err := getK8sVersion(versionInfo)
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
func (kubeAPI *KubernetesAPI) NamespaceExists(ctx context.Context, client rest.HTTPClient, namespace string) (bool, error) {
	rsp, err := kubeAPI.getRequest(ctx, client, "/api/v1/namespaces/"+namespace)
	if err != nil {
		return false, err
	}
	defer rsp.Body.Close()

	if rsp.StatusCode != http.StatusOK && rsp.StatusCode != http.StatusNotFound {
		return false, fmt.Errorf("Unexpected Kubernetes API response: %s", rsp.Status)
	}

	return rsp.StatusCode == http.StatusOK, nil
}

// GetPodsByNamespace returns all pods in a given namespace
func (kubeAPI *KubernetesAPI) GetPodsByNamespace(ctx context.Context, client rest.HTTPClient, namespace string) ([]corev1.Pod, error) {
	return kubeAPI.getPods(ctx, client, "/api/v1/namespaces/"+namespace+"/pods")
}

func (kubeAPI *KubernetesAPI) getPods(ctx context.Context, client rest.HTTPClient, path string) ([]corev1.Pod, error) {
	rsp, err := kubeAPI.getRequest(ctx, client, path)
	if err != nil {
		return nil, err
	}
	defer rsp.Body.Close()

	if rsp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Unexpected Kubernetes API response: %s", rsp.Status)
	}

	bytes, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		return nil, err
	}

	var podList corev1.PodList
	err = json.Unmarshal(bytes, &podList)
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}

// URLFor generates a URL based on the Kubernetes config.
func (kubeAPI *KubernetesAPI) URLFor(namespace, path string) (*url.URL, error) {
	return generateKubernetesAPIURLFor(kubeAPI.Host, namespace, path)
}

func (kubeAPI *KubernetesAPI) getRequest(ctx context.Context, client rest.HTTPClient, path string) (*http.Response, error) {
	endpoint, err := generateKubernetesURL(kubeAPI.Host, path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}

	return client.Do(req.WithContext(ctx))
}

// NewAPI validates a Kubernetes config and returns a client for accessing the
// configured cluster
func NewAPI(configPath, kubeContext string) (*KubernetesAPI, error) {
	config, err := GetConfig(configPath, kubeContext)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API client: %v", err)
	}

	return &KubernetesAPI{Config: config}, nil
}
