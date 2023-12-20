package webhook

import (
	"fmt"

	labels "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

// GetProxyContainerPath gets the proxy container jsonpath of a pod relative to spec;
// this path is required in webhooks because of how patches are created.
func GetProxyContainerPath(spec corev1.PodSpec) string {
	for i, c := range spec.Containers {
		if c.Name == labels.ProxyContainerName {
			return fmt.Sprintf("containers/%d", i)
		}
	}
	for i, c := range spec.InitContainers {
		if c.Name == labels.ProxyContainerName {
			return fmt.Sprintf("initContainers/%d", i)
		}
	}
	return ""
}
