package destination

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgk8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type opaquePortsAdaptor struct {
	listener    watcher.ProfileUpdateListener
	k8sAPI      *k8s.API
	log         *logging.Entry
	profile     *sp.ServiceProfile
	opaquePorts map[uint32]bool
}

func newOpaquePortsAdaptor(listener watcher.ProfileUpdateListener, k8sAPI *k8s.API, log *logging.Entry, opaquePorts map[uint32]bool) *opaquePortsAdaptor {
	if opaquePorts == nil {
		opaquePorts = make(map[uint32]bool)
	}
	return &opaquePortsAdaptor{
		listener:    listener,
		k8sAPI:      k8sAPI,
		log:         log,
		opaquePorts: opaquePorts,
	}
}

func (opa *opaquePortsAdaptor) Add(set watcher.AddressSet) {
	diff := opa.diff(set)
	for port := range diff {
		opa.opaquePorts[port] = true
	}
	opa.publish()
}

func (opa *opaquePortsAdaptor) Remove(set watcher.AddressSet) {
	diff := opa.diff(set)
	for port := range diff {
		delete(opa.opaquePorts, port)
	}
	opa.publish()
}

func (opa *opaquePortsAdaptor) NoEndpoints(exists bool) {
	for port := range opa.opaquePorts {
		delete(opa.opaquePorts, port)
	}
	opa.publish()
}

func (opa *opaquePortsAdaptor) Update(profile *sp.ServiceProfile) {
	opa.profile = profile
	opa.publish()
}

func (opa *opaquePortsAdaptor) diff(set watcher.AddressSet) map[uint32]bool {
	diff := make(map[uint32]bool)
	for _, address := range set.Addresses {
		pod := address.Pod
		if pod != nil {
			override, err := getOpaquePortsAnnotations(opa.k8sAPI, pod)
			if err != nil {
				opa.log.Errorf("Failed to get opaque ports annotation for pod %s: %s", pod, err)
			}
			for port := range override {
				diff[port] = true
			}
		}
	}
	return diff
}

func (opa *opaquePortsAdaptor) publish() {
	merged := sp.ServiceProfile{}
	if opa.profile != nil {
		merged = *opa.profile
	}
	merged.Spec.OpaquePorts = opa.opaquePorts
	fmt.Printf("publishing SP: %v", merged)
	opa.listener.Update(&merged)
}

func getOpaquePortsAnnotations(k8sAPI *k8s.API, pod *corev1.Pod) (map[uint32]bool, error) {
	opaquePorts := make(map[uint32]bool)
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

	// TODO: Should pod annotations override the namespace annotations?
	if podOverride, ok := pod.Annotations[pkgk8s.ProxyOpaquePortsAnnotation]; ok {
		override = podOverride
	}

	opaquePortsStr := util.ParseOpaquePorts(override, pod.Spec.Containers)
	for _, portStr := range strings.Split(opaquePortsStr, ",") {
		port, err := strconv.ParseUint(portStr, 10, 32)
		if err != nil {
			return nil, err
		}
		opaquePorts[uint32(port)] = true
	}

	return opaquePorts, nil
}
