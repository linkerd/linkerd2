package webhook

import (
	labels "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

// GetProxyContainerIndex gets the proxy container index of a pod; the index
// is required in webhooks because of how patches are created.
func GetProxyContainerIndex(containers []corev1.Container) int {
	for i, c := range containers {
		if c.Name == labels.ProxyContainerName {
			return i
		}
	}
	return -1
}
