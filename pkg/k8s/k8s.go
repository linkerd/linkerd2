package k8s

import (
	"fmt"
	"net/url"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	Deployments            = "deployments"
	Namespaces             = "namespaces"
	Pods                   = "pods"
	ReplicationControllers = "replicationcontrollers"
	Services               = "services"
	All                    = "all"
	Authorities            = "authorities"
)

// ResourceTypesToProxyLabels maps resource type names to keys
// understood by the proxy, specifically Destination and Prometheus labels.
var ResourceTypesToProxyLabels = map[string]string{
	Deployments: "deployment",
	Namespaces:  "namespace",
	Pods:        "pod",
	ReplicationControllers: "replication_controller",
	Services:               "service",
	Authorities:            "authority", // non k8s
}

// resources to query in StatSummary when Resource.Type is "all"
var StatAllResourceTypes = []string{
	Deployments,
	ReplicationControllers,
	Pods,
	Services,
	Authorities,
}

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

func getConfig(fpath string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if fpath != "" {
		rules.ExplicitPath = fpath
	}
	overrides := &clientcmd.ConfigOverrides{}
	return clientcmd.
		NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).
		ClientConfig()
}

// CanonicalResourceNameFromFriendlyName returns a canonical name from common shorthands used in command line tools.
// This works based on https://github.com/kubernetes/kubernetes/blob/63ffb1995b292be0a1e9ebde6216b83fc79dd988/pkg/kubectl/kubectl.go#L39
// This also works for non-k8s resources, e.g. authorities
func CanonicalResourceNameFromFriendlyName(friendlyName string) (string, error) {
	switch friendlyName {
	case "deploy", "deployment", "deployments":
		return Deployments, nil
	case "ns", "namespace", "namespaces":
		return Namespaces, nil
	case "po", "pod", "pods":
		return Pods, nil
	case "rc", "replicationcontroller", "replicationcontrollers":
		return ReplicationControllers, nil
	case "svc", "service", "services":
		return Services, nil
	case "au", "authority", "authorities":
		return Authorities, nil
	case "all":
		return All, nil
	}

	return "", fmt.Errorf("cannot find Kubernetes canonical name from friendly name [%s]", friendlyName)
}

// Return a the shortest name for a k8s canonical name.
// Essentially the reverse of CanonicalResourceNameFromFriendlyName
func ShortNameFromCanonicalResourceName(canonicalName string) string {
	switch canonicalName {
	case Deployments:
		return "deploy"
	case Namespaces:
		return "ns"
	case Pods:
		return "po"
	case ReplicationControllers:
		return "rc"
	case Services:
		return "svc"
	case Authorities:
		return "au"
	default:
		return ""
	}
}
