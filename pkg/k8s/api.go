package k8s

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/runconduit/conduit/cli/shell"
	"k8s.io/client-go/rest"
)

const kubernetesConfigFilePathEnvVariable = "KUBECONFIG"

type KubernetesApi interface {
	UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error)
	NewClient() (*http.Client, error)
}

type kubernetesApi struct {
	config               *rest.Config
	apiSchemeHostAndPort string
}

func (k8s *kubernetesApi) NewClient() (*http.Client, error) {
	secureTransport, err := rest.TransportFor(k8s.config)
	if err != nil {
		return nil, fmt.Errorf("error instantiating Kubernetes API client: %v", err)
	}

	return &http.Client{
		Transport: secureTransport,
	}, nil
}

func (k8s *kubernetesApi) UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error) {
	return generateKubernetesApiBaseUrlFor(k8s.apiSchemeHostAndPort, namespace, extraPathStartingWithSlash)
}

func NewK8sAPi(shell shell.Shell, k8sConfigFilesystemPathOverride string, apiHostAndPortOverride string) (KubernetesApi, error) {
	kubeconfigEnvVar := os.Getenv(kubernetesConfigFilePathEnvVariable)

	config, err := parseK8SConfig(findK8sConfigFile(k8sConfigFilesystemPathOverride, kubeconfigEnvVar, shell.HomeDir()))
	if err != nil {
		return nil, fmt.Errorf("error instantiating Kubernetes API client: %v", err)
	}

	if apiHostAndPortOverride == "" {
		apiHostAndPortOverride = config.Host
	}

	return &kubernetesApi{
		apiSchemeHostAndPort: apiHostAndPortOverride,
		config:               config,
	}, nil
}
