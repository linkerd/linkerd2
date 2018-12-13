package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/rest"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var minAPIVersion = [3]int{1, 8, 0}

type KubernetesAPI struct {
	*rest.Config
}

func (kubeAPI *KubernetesAPI) NewClient() (*http.Client, error) {
	secureTransport, err := rest.TransportFor(kubeAPI.Config)
	if err != nil {
		return nil, fmt.Errorf("error instantiating Kubernetes API client: %v", err)
	}

	return &http.Client{
		Transport: secureTransport,
	}, nil
}

func (kubeAPI *KubernetesAPI) GetVersionInfo(client *http.Client) (*version.Info, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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

func (kubeAPI *KubernetesAPI) NamespaceExists(client *http.Client, namespace string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
func (kubeAPI *KubernetesAPI) GetPodsByNamespace(client *http.Client, namespace string) ([]v1.Pod, error) {
	return kubeAPI.getPods(client, "/api/v1/namespaces/"+namespace+"/pods")
}

func (kubeAPI *KubernetesAPI) getPods(client *http.Client, path string) ([]v1.Pod, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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

	var podList v1.PodList
	err = json.Unmarshal(bytes, &podList)
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}

// URLFor generates a URL based on the Kubernetes config.
func (kubeAPI *KubernetesAPI) URLFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error) {
	return generateKubernetesAPIBaseURLFor(kubeAPI.Host, namespace, extraPathStartingWithSlash)
}

func (kubeAPI *KubernetesAPI) getRequest(ctx context.Context, client *http.Client, path string) (*http.Response, error) {
	endpoint, err := url.Parse(kubeAPI.Host + path)
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
