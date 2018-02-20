package k8s

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	kubernetesConfigFilePathEnvVariable = "KUBECONFIG"
	KubernetesDeployments               = "deployments"
	KubernetesPods                      = "pods"
)

func generateKubernetesApiBaseUrlFor(schemeHostAndPort string, namespace string, extraPathStartingWithSlash string) (*url.URL, error) {
	if string(extraPathStartingWithSlash[0]) != "/" {
		return nil, fmt.Errorf("Path must start with a [/], was [%s]", extraPathStartingWithSlash)
	}

	baseURL, err := generateBaseKubernetesApiUrl(schemeHostAndPort)
	if err != nil {
		return nil, err
	}

	urlString := fmt.Sprintf("%snamespaces/%s%s", baseURL.String(), namespace, extraPathStartingWithSlash)
	url, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("error generating namespace URL for Kubernetes API from [%s]", urlString)
	}

	return url, nil
}

func generateBaseKubernetesApiUrl(schemeHostAndPort string) (*url.URL, error) {
	urlString := fmt.Sprintf("%s/api/v1/", schemeHostAndPort)
	url, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("error generating base URL for Kubernetes API from [%s]", urlString)
	}
	return url, nil
}

func findK8sConfigFile(override string, contentsOfKubecongigEnvVar string, homeDir string) string {
	// See https://github.com/kubernetes/client-go/blob/master/examples/out-of-cluster-client-configuration/main.go
	if override != "" {
		return override
	}

	if contentsOfKubecongigEnvVar != "" {
		return contentsOfKubecongigEnvVar
	}

	return filepath.Join(homeDir, ".kube", "config")
}

func parseK8SConfig(pathToConfigFile string) (*rest.Config, error) {
	return clientcmd.BuildConfigFromFlags("", pathToConfigFile)
}

func buildK8sConfig(homedir string, k8sConfigFilesystemPathOverride string) (*rest.Config, error) {
	kubeconfigEnvVar := os.Getenv(kubernetesConfigFilePathEnvVariable)

	return parseK8SConfig(findK8sConfigFile(k8sConfigFilesystemPathOverride, kubeconfigEnvVar, homedir))
}

//CanonicalKubernetesNameFromFriendlyName returns a canonical name from common shorthands used in command line tools.
// This works based on https://github.com/kubernetes/kubernetes/blob/63ffb1995b292be0a1e9ebde6216b83fc79dd988/pkg/kubectl/kubectl.go#L39
func CanonicalKubernetesNameFromFriendlyName(friendlyName string) (string, error) {
	switch friendlyName {
	case "deploy", "deployment", "deployments":
		return KubernetesDeployments, nil
	case "po", "pod", "pods":
		return KubernetesPods, nil
	}

	return "", fmt.Errorf("cannot find Kubernetes canonical name from friendly name [%s]", friendlyName)
}
