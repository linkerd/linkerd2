package healthcheck

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// Has3rdPartySidecars returns true if the pod spec already has a third party
// init or sidecar containers already injected
func Has3rdPartySidecars(podSpec *corev1.PodSpec) bool {
	for _, container := range podSpec.Containers {
		if strings.HasPrefix(container.Image, "gcr.io/istio-release/proxyv2:") ||
			container.Name == "istio-proxy" {
			return true
		}
	}

	for _, ic := range podSpec.InitContainers {
		if strings.HasPrefix(ic.Image, "gcr.io/istio-release/proxy_init:") ||
			ic.Name == "istio-init" {
			return true
		}
	}

	return false
}
