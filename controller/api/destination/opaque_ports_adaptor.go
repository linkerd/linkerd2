package destination

import (
	"fmt"
	"strconv"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgk8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
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
			override, err := getOpaquePortsAnnotations(opa.k8sAPI, pod)
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

func getOpaquePortsAnnotations(k8sAPI *k8s.API, pod *corev1.Pod) (map[uint32]struct{}, error) {
	opaquePorts := make(map[uint32]struct{})
	obj, err := k8sAPI.GetObjects("", pkgk8s.Namespace, pod.Namespace, labels.Everything())
	if err != nil {
		return nil, err
	}
	if len(obj) > 1 {
		return nil, fmt.Errorf("Namespace conflict: %v, %v", obj[0], obj[1])
	}
	if len(obj) != 1 {
		return nil, fmt.Errorf("Namespace not found: %v", pod.Namespace)
	}
	ns, ok := obj[0].(*corev1.Namespace)
	if !ok {
		// This is very unlikely due to how `GetObjects` works
		return nil, fmt.Errorf("Object with name %s was not a namespace", pod.Namespace)
	}
	override := ns.Annotations[pkgk8s.ProxyOpaquePortsAnnotation]

	// Pod annotations override namespace annotations
	if podOverride, ok := pod.Annotations[pkgk8s.ProxyOpaquePortsAnnotation]; ok {
		override = podOverride
	}

	if override != "" {
		for _, portStr := range util.ParseOpaquePorts(override, pod.Spec.Containers) {
			port, err := strconv.ParseUint(portStr, 10, 32)
			if err != nil {
				return nil, err
			}
			opaquePorts[uint32(port)] = struct{}{}
		}
	}

	return opaquePorts, nil
}
