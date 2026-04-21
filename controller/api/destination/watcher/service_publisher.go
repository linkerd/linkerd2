package watcher

import (
	"strconv"
	"sync"

	"github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta3"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type (

	// servicePublisher represents a service.  It keeps a map of portPublishers
	// keyed by port and hostname.  This is because each watch on a service
	// will have a port and optionally may specify a hostname.  The port
	// and hostname will influence the endpoint set which is why a separate
	// portPublisher is required for each port and hostname combination.  The
	// service's port mapping will be applied to the requested port and the
	// mapped port will be used in the addresses set.  If a hostname is
	// requested, the address set will be filtered to only include addresses
	// with the requested hostname.
	servicePublisher struct {
		id                   ServiceID
		log                  *logging.Entry
		k8sAPI               *k8s.API
		metadataAPI          *k8s.MetadataAPI
		enableEndpointSlices bool
		enableIPv6           bool
		localTrafficPolicy   bool
		cluster              string
		ports                map[Port]*portPublisher
		// All access to the servicePublisher and its portPublishers is explicitly synchronized by
		// this mutex.
		sync.Mutex
	}
)

func (sp *servicePublisher) updateEndpoints(newEndpoints *corev1.Endpoints) {
	sp.Lock()
	defer sp.Unlock()
	sp.log.Debugf("Updating endpoints for %s", sp.id)
	for _, port := range sp.ports {
		port.updateEndpoints(newEndpoints)
	}
}

func (sp *servicePublisher) deleteEndpoints() {
	sp.Lock()
	defer sp.Unlock()
	sp.log.Debugf("Deleting endpoints for %s", sp.id)
	for _, port := range sp.ports {
		port.noEndpoints(false)
	}
}

func (sp *servicePublisher) addEndpointSlice(newSlice *discovery.EndpointSlice) {
	sp.Lock()
	defer sp.Unlock()

	sp.log.Debugf("Adding ES %s/%s", newSlice.Namespace, newSlice.Name)
	for _, port := range sp.ports {
		port.addEndpointSlice(newSlice)
	}
}

func (sp *servicePublisher) updateEndpointSlice(oldSlice *discovery.EndpointSlice, newSlice *discovery.EndpointSlice) {
	sp.Lock()
	defer sp.Unlock()

	sp.log.Debugf("Updating ES %s/%s", oldSlice.Namespace, oldSlice.Name)
	for _, port := range sp.ports {
		port.updateEndpointSlice(oldSlice, newSlice)
	}
}

func (sp *servicePublisher) deleteEndpointSlice(es *discovery.EndpointSlice) {
	sp.Lock()
	defer sp.Unlock()

	sp.log.Debugf("Deleting ES %s/%s", es.Namespace, es.Name)
	for _, port := range sp.ports {
		port.deleteEndpointSlice(es)
	}
}

func (sp *servicePublisher) updateService(newService *corev1.Service) {
	sp.Lock()
	defer sp.Unlock()
	sp.log.Debugf("Updating service for %s", sp.id)

	// set localTrafficPolicy to true if InternalTrafficPolicy is set to local
	if newService.Spec.InternalTrafficPolicy != nil {
		sp.localTrafficPolicy = *newService.Spec.InternalTrafficPolicy == corev1.ServiceInternalTrafficPolicyLocal
	} else {
		sp.localTrafficPolicy = false
	}

	for port, publisher := range sp.ports {
		newTargetPort := getTargetPort(newService, port)
		if newTargetPort != publisher.targetPort {
			publisher.updatePort(newTargetPort)
		}
		// update service endpoints with new localTrafficPolicy
		if publisher.localTrafficPolicy != sp.localTrafficPolicy {
			publisher.updateLocalTrafficPolicy(sp.localTrafficPolicy)
		}
	}

}

func (sp *servicePublisher) subscribe(srcPort Port, listener EndpointUpdateListener, filterKey FilterKey) error {
	sp.Lock()
	defer sp.Unlock()

	publisher, ok := sp.ports[srcPort]
	if !ok {
		var err error
		publisher, err = sp.newPortPublisher(srcPort)
		if err != nil {
			return err
		}
		sp.ports[srcPort] = publisher
	}
	publisher.subscribe(listener, filterKey)
	return nil
}

func (sp *servicePublisher) unsubscribe(srcPort Port, listener EndpointUpdateListener, filterKey FilterKey, withRemove bool) {
	sp.Lock()
	defer sp.Unlock()

	publisher, ok := sp.ports[srcPort]
	if ok {
		publisher.unsubscribe(listener, filterKey, withRemove)
		if publisher.totalListeners() == 0 {
			endpointsVecs.unregister(sp.metricsLabels(srcPort))
			delete(sp.ports, srcPort)
		}
	}
}

func (sp *servicePublisher) newPortPublisher(srcPort Port) (*portPublisher, error) {
	targetPort := intstr.FromInt(int(srcPort))
	svc, err := sp.k8sAPI.Svc().Lister().Services(sp.id.Namespace).Get(sp.id.Name)
	if err != nil && !apierrors.IsNotFound(err) {
		sp.log.Errorf("error getting service: %s", err)
	}
	exists := false
	if err == nil {
		targetPort = getTargetPort(svc, srcPort)
		exists = true
	}

	log := sp.log.WithField("port", srcPort)

	metrics, err := endpointsVecs.newEndpointsMetrics(sp.metricsLabels(srcPort))
	if err != nil {
		return nil, err
	}
	port := &portPublisher{
		filteredListeners:    map[FilterKey]*filteredListenerGroup{},
		targetPort:           targetPort,
		srcPort:              srcPort,
		exists:               exists,
		k8sAPI:               sp.k8sAPI,
		metadataAPI:          sp.metadataAPI,
		log:                  log,
		metrics:              metrics,
		enableEndpointSlices: sp.enableEndpointSlices,
		enableIPv6:           sp.enableIPv6,
		localTrafficPolicy:   sp.localTrafficPolicy,
	}

	if port.enableEndpointSlices {
		matchLabels := map[string]string{discovery.LabelServiceName: sp.id.Name}
		selector := labels.Set(matchLabels).AsSelector()

		sliceList, err := sp.k8sAPI.ES().Lister().EndpointSlices(sp.id.Namespace).List(selector)
		if err != nil && !apierrors.IsNotFound(err) {
			sp.log.Errorf("error getting endpointSlice list: %s", err)
		}
		if err == nil {
			for _, slice := range sliceList {
				port.addEndpointSlice(slice)
			}
		}
	} else {
		endpoints, err := sp.k8sAPI.Endpoint().Lister().Endpoints(sp.id.Namespace).Get(sp.id.Name)
		if err != nil && !apierrors.IsNotFound(err) {
			sp.log.Errorf("error getting endpoints: %s", err)
		}
		if err == nil {
			port.updateEndpoints(endpoints)
		}
	}

	return port, nil
}

func (sp *servicePublisher) metricsLabels(port Port) prometheus.Labels {
	return endpointsLabels(sp.cluster, sp.id.Namespace, sp.id.Name, strconv.Itoa(int(port)))
}

func (sp *servicePublisher) updateServer(oldServer, newServer *v1beta3.Server) {
	sp.Lock()
	defer sp.Unlock()

	for _, pp := range sp.ports {
		pp.updateServer(oldServer, newServer)
	}
}
