package destination

import (
	"strconv"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgk8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

// opaquePortsAdaptor implements EndpointUpdateListener so that it can watch
// endpoints for services. When endpoints change, it can check for the opaque
// ports annotation on the pods and update the list of opaque ports.
//
// opaquePortsAdaptor also implements ProfileUpdateListener so that it can
// receive updates from trafficSplitAdaptor and merge the service profile by
// adding the list of opaque ports.
//
// When either of these implemented interfaces has an update,
// opaquePortsAdaptor publishes the new service profile to the
// profileTranslator.
type opaquePortsAdaptor struct {
	listener    watcher.ProfileUpdateListener
	k8sAPI      *k8s.API
	log         *logging.Entry
	profile     *sp.ServiceProfile
	opaquePorts map[uint32]struct{}
}

func newOpaquePortsAdaptor(listener watcher.ProfileUpdateListener, k8sAPI *k8s.API, log *logging.Entry) *opaquePortsAdaptor {
	return &opaquePortsAdaptor{
		listener:    listener,
		k8sAPI:      k8sAPI,
		log:         log,
		opaquePorts: make(map[uint32]struct{}),
	}
}

func (opa *opaquePortsAdaptor) Add(set watcher.AddressSet) {
	ports := opa.getOpaquePorts(set)
	for port := range ports {
		opa.opaquePorts[port] = struct{}{}
	}
	opa.publish()
}

func (opa *opaquePortsAdaptor) Remove(set watcher.AddressSet) {
	ports := opa.getOpaquePorts(set)
	for port := range ports {
		delete(opa.opaquePorts, port)
	}
	opa.publish()
}

func (opa *opaquePortsAdaptor) NoEndpoints(exists bool) {
	opa.opaquePorts = make(map[uint32]struct{})
	opa.publish()
}

func (opa *opaquePortsAdaptor) Update(profile *sp.ServiceProfile) {
	opa.profile = profile
	opa.publish()
}

func (opa *opaquePortsAdaptor) getOpaquePorts(set watcher.AddressSet) map[uint32]struct{} {
	ports := make(map[uint32]struct{})
	for _, address := range set.Addresses {
		pod := address.Pod
		if pod != nil {
			override, err := getOpaquePortsAnnotations(pod)
			if err != nil {
				opa.log.Errorf("Failed to get opaque ports annotation for pod %s: %s", pod, err)
			}
			for port := range override {
				ports[port] = struct{}{}
			}
		}
	}
	return ports
}

func (opa *opaquePortsAdaptor) publish() {
	merged := sp.ServiceProfile{}
	if opa.profile != nil {
		merged = *opa.profile
	}
	merged.Spec.OpaquePorts = opa.opaquePorts
	opa.listener.Update(&merged)
}

func getOpaquePortsAnnotations(pod *corev1.Pod) (map[uint32]struct{}, error) {
	opaquePorts := make(map[uint32]struct{})
	annotation := pod.Annotations[pkgk8s.ProxyOpaquePortsAnnotation]
	if annotation != "" {
		for _, portStr := range util.ParseOpaquePorts(annotation, pod.Spec.Containers) {
			port, err := strconv.ParseUint(portStr, 10, 32)
			if err != nil {
				return nil, err
			}
			opaquePorts[uint32(port)] = struct{}{}
		}
	}
	return opaquePorts, nil
}
