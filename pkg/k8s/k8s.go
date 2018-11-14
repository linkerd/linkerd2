package k8s

import (
	"fmt"
	"net/url"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	All                   = "all"
	Authority             = "authority"
	DaemonSet             = "daemonset"
	Deployment            = "deployment"
	Namespace             = "namespace"
	Pod                   = "pod"
	ReplicationController = "replicationcontroller"
	ReplicaSet            = "replicaset"
	Service               = "service"
	ServiceProfile        = "serviceprofile"
	StatefulSet           = "statefulset"
)

// AllResources is a sorted list of all resources defined as constants above.
var AllResources = []string{
	Authority,
	DaemonSet,
	Deployment,
	Namespace,
	Pod,
	ReplicationController,
	ReplicaSet,
	Service,
	ServiceProfile,
	StatefulSet,
}

// resources to query in StatSummary when Resource.Type is "all"
var StatAllResourceTypes = []string{
	// TODO: add Namespace here to decrease queries from the web process
	Deployment,
	ReplicationController,
	Pod,
	Service,
	Authority,
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

// GetConfig returns kubernetes config based on the current environment.
// If fpath is provided, loads configuration from that file. Otherwise,
// GetConfig uses default strategy to load configuration from $KUBECONFIG,
// .kube/config, or just returns in-cluster config.
func GetConfig(fpath, kubeContext string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if fpath != "" {
		rules.ExplicitPath = fpath
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
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
		return Deployment, nil
	case "ds", "daemonset", "daemonsets":
		return DaemonSet, nil
	case "ns", "namespace", "namespaces":
		return Namespace, nil
	case "po", "pod", "pods":
		return Pod, nil
	case "rc", "replicationcontroller", "replicationcontrollers":
		return ReplicationController, nil
	case "svc", "service", "services":
		return Service, nil
	case "sts", "statefulset", "statefulsets":
		return StatefulSet, nil
	case "au", "authority", "authorities":
		return Authority, nil
	case "all":
		return All, nil
	}

	return "", fmt.Errorf("cannot find Kubernetes canonical name from friendly name [%s]", friendlyName)
}

// Return a the shortest name for a k8s canonical name.
// Essentially the reverse of CanonicalResourceNameFromFriendlyName
func ShortNameFromCanonicalResourceName(canonicalName string) string {
	switch canonicalName {
	case Deployment:
		return "deploy"
	case DaemonSet:
		return "ds"
	case Namespace:
		return "ns"
	case Pod:
		return "po"
	case ReplicationController:
		return "rc"
	case ReplicaSet:
		return "rs"
	case Service:
		return "svc"
	case StatefulSet:
		return "sts"
	case Authority:
		return "au"
	default:
		return ""
	}
}
