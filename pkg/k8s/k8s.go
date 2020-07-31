package k8s

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// These constants are string representations of Kubernetes resource types.
const (
	All                   = "all"
	Authority             = "authority"
	CronJob               = "cronjob"
	DaemonSet             = "daemonset"
	Deployment            = "deployment"
	Job                   = "job"
	Namespace             = "namespace"
	Pod                   = "pod"
	ReplicationController = "replicationcontroller"
	ReplicaSet            = "replicaset"
	Service               = "service"
	ServiceProfile        = "serviceprofile"
	StatefulSet           = "statefulset"
	TrafficSplit          = "trafficsplit"
	Node                  = "node"

	ServiceProfileAPIVersion = "linkerd.io/v1alpha2"
	ServiceProfileKind       = "ServiceProfile"

	LinkAPIGroup        = "multicluster.linkerd.io"
	LinkAPIVersion      = "v1alpha1"
	LinkAPIGroupVersion = "multicluster.linkerd.io/v1alpha1"
	LinkKind            = "Link"

	// special case k8s job label, to not conflict with Prometheus' job label
	l5dJob = "k8s_job"
)

type resourceName struct {
	short  string
	full   string
	plural string
}

// AllResources is a sorted list of all resources defined as constants above.
var AllResources = []string{
	Authority,
	CronJob,
	DaemonSet,
	Deployment,
	Job,
	Namespace,
	Pod,
	ReplicationController,
	ReplicaSet,
	Service,
	ServiceProfile,
	StatefulSet,
	TrafficSplit,
}

// StatAllResourceTypes represents the resources to query in StatSummary when Resource.Type is "all"
var StatAllResourceTypes = []string{
	// TODO: add Namespace here to decrease queries from the web process
	DaemonSet,
	StatefulSet,
	Job,
	Deployment,
	ReplicationController,
	Pod,
	Service,
	TrafficSplit,
	Authority,
	CronJob,
	ReplicaSet,
}

var resourceNames = []resourceName{
	{"au", "authority", "authorities"},
	{"cj", "cronjob", "cronjobs"},
	{"ds", "daemonset", "daemonsets"},
	{"deploy", "deployment", "deployments"},
	{"job", "job", "jobs"},
	{"ns", "namespace", "namespaces"},
	{"po", "pod", "pods"},
	{"rc", "replicationcontroller", "replicationcontrollers"},
	{"rs", "replicaset", "replicasets"},
	{"svc", "service", "services"},
	{"sp", "serviceprofile", "serviceprofiles"},
	{"sts", "statefulset", "statefulsets"},
	{"ts", "trafficsplit", "trafficsplits"},
	{"all", "all", "all"},
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
	for _, name := range resourceNames {
		if friendlyName == name.short || friendlyName == name.full || friendlyName == name.plural {
			return name.full, nil
		}
	}
	return "", fmt.Errorf("cannot find Kubernetes canonical name from friendly name [%s]", friendlyName)
}

// PluralResourceNameFromFriendlyName returns a pluralized canonical name from common shorthands used in command line tools.
// This works based on https://github.com/kubernetes/kubernetes/blob/63ffb1995b292be0a1e9ebde6216b83fc79dd988/pkg/kubectl/kubectl.go#L39
// This also works for non-k8s resources, e.g. authorities
func PluralResourceNameFromFriendlyName(friendlyName string) (string, error) {
	for _, name := range resourceNames {
		if friendlyName == name.short || friendlyName == name.full || friendlyName == name.plural {
			return name.plural, nil
		}
	}
	return "", fmt.Errorf("cannot find Kubernetes canonical name from friendly name [%s]", friendlyName)
}

// ShortNameFromCanonicalResourceName returns the shortest name for a k8s canonical name.
// Essentially the reverse of CanonicalResourceNameFromFriendlyName
func ShortNameFromCanonicalResourceName(canonicalName string) string {
	switch canonicalName {
	case Authority:
		return "au"
	case CronJob:
		return "cj"
	case DaemonSet:
		return "ds"
	case Deployment:
		return "deploy"
	case Job:
		return "job"
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
	case ServiceProfile:
		return "sp"
	case StatefulSet:
		return "sts"
	case TrafficSplit:
		return "ts"
	default:
		return ""
	}
}

// KindToL5DLabel converts a Kubernetes `kind` to a Linkerd label.
// For example:
//   `pod` -> `pod`
//   `job` -> `k8s_job`
func KindToL5DLabel(k8sKind string) string {
	if k8sKind == Job {
		return l5dJob
	}
	return k8sKind
}
