package healthcheck

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// HasExistingSidecars returns true if the pod spec already has the proxy init
// and sidecar containers injected. Otherwise, it returns false.
// TODO: remove this func and use HasExisting3rdPartySidecars() instead once
// the webhook starts using the common inject logic from the CLI
func HasExistingSidecars(podSpec *corev1.PodSpec) bool {
	for _, container := range podSpec.Containers {
		if strings.HasPrefix(container.Image, "gcr.io/linkerd-io/proxy:") ||
			strings.HasPrefix(container.Image, "gcr.io/istio-release/proxyv2:") ||
			strings.HasPrefix(container.Image, "gcr.io/heptio-images/contour:") ||
			strings.HasPrefix(container.Image, "docker.io/envoyproxy/envoy-alpine:") ||
			container.Name == "linkerd-proxy" ||
			container.Name == "istio-proxy" ||
			container.Name == "contour" ||
			container.Name == "envoy" {
			return true
		}
	}

	for _, ic := range podSpec.InitContainers {
		if strings.HasPrefix(ic.Image, "gcr.io/linkerd-io/proxy-init:") ||
			strings.HasPrefix(ic.Image, "gcr.io/istio-release/proxy_init:") ||
			strings.HasPrefix(ic.Image, "gcr.io/heptio-images/contour:") ||
			ic.Name == "linkerd-init" ||
			ic.Name == "istio-init" ||
			ic.Name == "envoy-initconfig" {
			return true
		}
	}

	return false
}

// HasExisting3rdPartySidecars returns true if the pod spec already has a 3rd party proxy init
// and sidecar containers injected. Otherwise, it returns false.
func HasExisting3rdPartySidecars(podSpec *corev1.PodSpec) bool {
	for _, container := range podSpec.Containers {
		if strings.HasPrefix(container.Image, "gcr.io/istio-release/proxyv2:") ||
			strings.HasPrefix(container.Image, "gcr.io/heptio-images/contour:") ||
			strings.HasPrefix(container.Image, "docker.io/envoyproxy/envoy-alpine:") ||
			container.Name == "istio-proxy" ||
			container.Name == "contour" ||
			container.Name == "envoy" {
			return true
		}
	}

	for _, ic := range podSpec.InitContainers {
		if strings.HasPrefix(ic.Image, "gcr.io/istio-release/proxy_init:") ||
			strings.HasPrefix(ic.Image, "gcr.io/heptio-images/contour:") ||
			ic.Name == "istio-init" ||
			ic.Name == "envoy-initconfig" {
			return true
		}
	}

	return false
}
