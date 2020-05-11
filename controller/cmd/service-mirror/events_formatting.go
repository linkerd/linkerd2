package servicemirror

import (
	"fmt"
	"strings"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

func formatAddresses(addresses []corev1.EndpointAddress) string {
	var adrs []string

	for _, a := range addresses {
		adrs = append(adrs, a.IP)
	}
	return fmt.Sprintf("[%s]", strings.Join(adrs, ","))
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

func formatEndpoints(endp *corev1.Endpoints) string {
	var subsets []string

	for _, ss := range endp.Subsets {
		subsets = append(subsets, fmt.Sprintf("%s:%s", formatAddresses(ss.Addresses), formatPorts(ss.Ports)))
	}

	return fmt.Sprintf("Endpoints: {name: %s, namespace: %s, annotations: [%s], labels: [%s], subsets: [%s]}", endp.Name, endp.Namespace, formatMetadata(endp.Annotations), formatMetadata(endp.Labels), strings.Join(subsets, ","))
}

func (b ProbeConfig) String() string {
	return fmt.Sprintf("ProbeConfig: {path: %s, port: %d, periodInSeconds: %d}", b.path, b.port, b.periodInSeconds)
}

func (b GatewaySpec) String() string {
	return fmt.Sprintf("GatewaySpec: {gatewayName: %s, gatewayNamespace: %s, clusterName: %s, addresses: [%s], incomingPort: %d, resourceVersion: %s, identity: %s, probeConfig: %s}", b.gatewayName, b.gatewayNamespace, b.clusterName, formatAddresses(b.addresses), b.incomingPort, b.resourceVersion, b.identity, b.ProbeConfig)
}

func (gtm gatewayMetadata) String() string {
	return fmt.Sprintf("gatewayMetadata: {name: %s, namespace: %s}", gtm.Name, gtm.Namespace)
}

// Events for cluster watcher
func (rsc RemoteServiceCreated) String() string {
	return fmt.Sprintf("RemoteServiceCreated: {service: %s, gatewayData: %s}", formatService(rsc.service), rsc.gatewayData)
}

func (rsu RemoteServiceUpdated) String() string {
	return fmt.Sprintf("RemoteServiceUpdated: {localService: %s, localEndpoints: %s, remoteUpdate: %s, gatewayData: %s}", formatService(rsu.localService), formatEndpoints(rsu.localEndpoints), formatService(rsu.remoteUpdate), rsu.gatewayData)
}

func (rsd RemoteServiceDeleted) String() string {
	return fmt.Sprintf("RemoteServiceDeleted: {name: %s, namespace: %s, gatewayData: %s}", rsd.Name, rsd.Namespace, rsd.GatewayData)
}

func (rgd *RemoteGatewayDeleted) String() string {
	return fmt.Sprintf("RemoteGatewayDeleted: {gatewayData: %s}", rgd.gatewayData)
}

func (rgu RemoteGatewayUpdated) String() string {
	var services []string

	for _, s := range rgu.affectedServices {
		services = append(services, fmt.Sprint(s))
	}
	return fmt.Sprintf("RemoteGatewayUpdated: {gatewaySpec: %s, affectedServices: [%s]}", rgu.gatewaySpec, strings.Join(services, ","))
}

func (cgu ConsiderGatewayUpdateDispatch) String() string {
	return fmt.Sprintf("ConsiderGatewayUpdateDispatch: {maybeGateway: %s}", formatService(cgu.maybeGateway))
}

func (cgu ClusterUnregistered) String() string {
	return "ClusterUnregistered: {}"
}

func (cgu OprhanedServicesGcTriggered) String() string {
	return "OprhanedServicesGcTriggered: {}"
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

//Events for probe manager

func (msu MirroredServiceUnpaired) String() string {
	return fmt.Sprintf("MirroredServiceUnpaired: {serviceName: %s, serviceNamespace: %s, gatewayName: %s, gatewayNs: %s, clusterName: %s}", msu.serviceName, msu.serviceNamespace, msu.gatewayName, msu.gatewayNs, msu.clusterName)
}

func (msp MirroredServicePaired) String() string {
	return fmt.Sprintf("MirroredServicePaired: {serviceName: %s, serviceNamespace: %s, gatewaySpec: %s}", msp.serviceName, msp.serviceNamespace, msp.GatewaySpec)
}

func (gtwd GatewayDeleted) String() string {
	return fmt.Sprintf("GatewayDeleted: {gatewayName: %s, gatewayNs: %s, clusterName: %s}", gtwd.gatewayName, gtwd.gatewayNs, gtwd.clusterName)
}

func (cnr ClusterNotRegistered) String() string {
	return fmt.Sprintf("ClusterNotRegistered: {clusterName: %s}", cnr.clusterName)
}

func (gtwu GatewayUpdated) String() string {
	return fmt.Sprintf("GatewayUpdated: {gatewaySpec: %s}", gtwu.GatewaySpec)
}
