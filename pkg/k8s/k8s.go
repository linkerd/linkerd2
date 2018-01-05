package k8s

import (
	"fmt"
	"net/url"

	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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
