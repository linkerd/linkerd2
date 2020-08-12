package servicemirror

import (
	"fmt"
	"strings"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

func formatAddresses(addresses []corev1.EndpointAddress) string {
	var addrs []string

	for _, a := range addresses {
		addrs = append(addrs, a.IP)
	}
	return fmt.Sprintf("[%s]", strings.Join(addrs, ","))
}

func formatMetadata(meta map[string]string) string {
	var metadata []string

	for k, v := range meta {
		if strings.Contains(k, consts.Prefix) || strings.Contains(k, consts.ProxyConfigAnnotationsPrefix) {
			metadata = append(metadata, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return fmt.Sprintf("[%s]", strings.Join(metadata, ","))
}

func formatPorts(ports []corev1.EndpointPort) string {
	var formattedPorts []string

	for _, p := range ports {
		formattedPorts = append(formattedPorts, fmt.Sprintf("Port: {name: %s, port: %d}", p.Name, p.Port))
	}
	return fmt.Sprintf("[%s]", strings.Join(formattedPorts, ","))
}

func formatService(svc *corev1.Service) string {
	return fmt.Sprintf("Service: {name: %s, namespace: %s, annotations: [%s], labels [%s]}", svc.Name, svc.Namespace, formatMetadata(svc.Annotations), formatMetadata(svc.Labels))
}

func formatEndpoints(endpoints *corev1.Endpoints) string {
	var subsets []string

	for _, ss := range endpoints.Subsets {
		subsets = append(subsets, fmt.Sprintf("%s:%s", formatAddresses(ss.Addresses), formatPorts(ss.Ports)))
	}

	return fmt.Sprintf("Endpoints: {name: %s, namespace: %s, annotations: [%s], labels: [%s], subsets: [%s]}", endpoints.Name, endpoints.Namespace, formatMetadata(endpoints.Annotations), formatMetadata(endpoints.Labels), strings.Join(subsets, ","))
}

// Events for cluster watcher
func (rsc RemoteServiceCreated) String() string {
	return fmt.Sprintf("RemoteServiceCreated: {service: %s}", formatService(rsc.service))
}

func (rsu RemoteServiceUpdated) String() string {
	return fmt.Sprintf("RemoteServiceUpdated: {localService: %s, localEndpoints: %s, remoteUpdate: %s}", formatService(rsu.localService), formatEndpoints(rsu.localEndpoints), formatService(rsu.remoteUpdate))
}

func (rsd RemoteServiceDeleted) String() string {
	return fmt.Sprintf("RemoteServiceDeleted: {name: %s, namespace: %s }", rsd.Name, rsd.Namespace)
}

func (cgu ClusterUnregistered) String() string {
	return "ClusterUnregistered: {}"
}

func (cgu OrphanedServicesGcTriggered) String() string {
	return "OrphanedServicesGcTriggered: {}"
}

func (oa OnAddCalled) String() string {
	return fmt.Sprintf("OnAddCalled: {svc: %s}", formatService(oa.svc))
}

func (ou OnUpdateCalled) String() string {
	return fmt.Sprintf("OnUpdateCalled: {svc: %s}", formatService(ou.svc))
}

func (od OnDeleteCalled) String() string {
	return fmt.Sprintf("OnDeleteCalled: {svc: %s}", formatService(od.svc))
}

func (re RepairEndpoints) String() string {
	return "RepairEndpoints"
}
