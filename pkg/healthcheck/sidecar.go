package healthcheck

import (
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

// HasExistingSidecars returns true if the pod spec already has the proxy init
// and sidecar containers injected. Otherwise, it returns false.
func HasExistingSidecars(podSpec *corev1.PodSpec) bool {
	for _, container := range podSpec.Containers {
		if strings.HasPrefix(container.Image, "cr.l5d.io/linkerd/proxy:") ||
			strings.HasPrefix(container.Image, "gcr.io/istio-release/proxyv2:") ||
			container.Name == k8s.ProxyContainerName ||
			container.Name == "istio-proxy" {
			return true
		}
	}

	for _, ic := range podSpec.InitContainers {
		if strings.HasPrefix(ic.Image, "cr.l5d.io/linkerd/proxy-init:") ||
			strings.HasPrefix(ic.Image, "gcr.io/istio-release/proxy_init:") ||
			ic.Name == "linkerd-init" ||
			ic.Name == "istio-init" {
			return true
		}
	}

	return false
}
