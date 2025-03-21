package servicemirror

import (
	"fmt"
	"strings"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

func formatMetadata(meta map[string]string) string {
	var metadata []string

	for k, v := range meta {
		if strings.HasPrefix(k, consts.Prefix) || strings.HasPrefix(k, consts.ProxyConfigAnnotationsPrefix) {
			metadata = append(metadata, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return fmt.Sprintf("[%s]", strings.Join(metadata, ","))
}

func formatService(svc *corev1.Service) string {
	if svc == nil {
		return "Service: nil"
	}
	return fmt.Sprintf("Service: {name: %s, namespace: %s, annotations: [%s], labels [%s]}", svc.Name, svc.Namespace, formatMetadata(svc.Annotations), formatMetadata(svc.Labels))
}

// Events for cluster watcher
func (rsc RemoteServiceExported) String() string {
	return fmt.Sprintf("RemoteServiceExported: {service: %s}", formatService(rsc.service))
}

func (rsu RemoteExportedServiceUpdated) String() string {
	return fmt.Sprintf("RemoteExportedServiceUpdated: {remoteUpdate: %s}", formatService(rsu.remoteUpdate))
}

func (rsd RemoteServiceUnexported) String() string {
	return fmt.Sprintf("RemoteServiceUnexported: {name: %s, namespace: %s }", rsd.Name, rsd.Namespace)
}

func (cfs CreateFederatedService) String() string {
	return fmt.Sprintf("CreateFederatedService: {service: %s}", formatService(cfs.service))
}

func (jfs RemoteServiceJoinsFederatedService) String() string {
	return fmt.Sprintf("RemoteServiceJoinsFederatedService: {remoteUpdate: %s}", formatService(jfs.remoteUpdate))
}

func (lfs RemoteServiceLeavesFederatedService) String() string {
	return fmt.Sprintf("RemoteServiceLeavesFederatedService: {name: %s, namespace: %s }", lfs.Name, lfs.Namespace)
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

func (ol OnLocalNamespaceAdded) String() string {
	return fmt.Sprintf("OnLocalNamespaceAdded: {namespace: %s}", ol.ns)
}
