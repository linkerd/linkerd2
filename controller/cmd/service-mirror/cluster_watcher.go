package servicemirror

import (
	"errors"
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

//TODO: Handle temporary network partitions

type (
	// RemoteClusterServiceWatcher is a watcher instantiated for every cluster that is being watched
	// Its main job is to listen to events coming from the remote cluster and react accordingly, keeping
	// the state of the mirrored services in sync. This is achieved by maintaining a SharedInformer
	// on the remote cluster. The basic add/update/delete operations are mapped to a more domain specific
	// events, put onto a work queue and handled by the processing loop. In case processing an event fails
	// it can be requeued up to N times, to ensure that the failure is not due to some temporary network
	// problems or general glitch in the Matrix.
	RemoteClusterServiceWatcher struct {
		clusterName     string
		remoteAPIClient *k8s.API
		localAPIClient  *k8s.API
		stopper         chan struct{}
		log             *logging.Entry
		eventsQueue     workqueue.RateLimitingInterface
		requeueLimit    int
	}

	// RemoteServiceCreated is generated whenever a remote service is created Observing
	// this event means that the service in question is not mirrored atm
	RemoteServiceCreated struct {
		service            *corev1.Service
		gatewayData        *gatewayMetadata
		newResourceVersion string
	}

	// RemoteServiceUpdated is generated when we see something about an already
	// mirrored service change on the remote cluster. In that case we need to
	// reconcile. Most importantly we need to keep track of exposed ports
	// and gateway association changes.
	RemoteServiceUpdated struct {
		localService   *corev1.Service
		localEndpoints *corev1.Endpoints
		remoteUpdate   *corev1.Service
		gatewayData    *gatewayMetadata
	}

	// RemoteServiceDeleted when a remote service is going away
	RemoteServiceDeleted struct {
		Name      string
		Namespace string
	}

	// RemoteGatewayDeleted is observed when a service that is a gateway to at least
	// one already mirrored service is deleted
	RemoteGatewayDeleted struct {
		gatewayData       *gatewayMetadata
		affectedEndpoints []*corev1.Endpoints
	}

	// RemoteGatewayUpdated happens when a service that is a gateway to at least
	// one already mirrored service is updated. This might mean an IP change,
	// incoming port change, etc...
	RemoteGatewayUpdated struct {
		newPort              int32
		newEndpointAddresses []corev1.EndpointAddress
		gatewayData          *gatewayMetadata
		newResourceVersion   string
		affectedServices     []*corev1.Service
	}

	// ConsiderGatewayUpdateDispatch is issued when we are receiving an update for a
	// service but we are not sure that this is a gateway. We need to hit the local
	// API and make sure there are services that have this service as a gateway. Since
	// this is an operation that might fail (a glitch in the local api connectivity)
	// we want to represent that as a separate event that can be requeued
	ConsiderGatewayUpdateDispatch struct {
		maybeGateway *corev1.Service
	}

	// ClusterUnregistered is issued when the secret containing the remote cluster
	// access information is deleted
	ClusterUnregistered struct{}

	// OprhanedServicesGcTriggered is a self-triggered event which aims to delete any
	// orphaned services that are no longer on the remote cluster. It is emitted every
	// time a new remote cluster is registered for monitoring. The need for this arises
	// because the following might happen.
	//
	// 1. A cluster is registered for monitoring
	// 2. Services A,B,C are created and mirrored
	// 3. Then this component crashes, leaving the mirrors around
	// 4. In the meantime services B and C are deleted on the remote cluster
	// 5. When the controller starts up again it registers to listen for mirrored services
	// 6. It receives an ADD for A but not a DELETE for B and C
	//
	// This event indicates that we need to make a diff with all services on the remote
	// cluster, ensuring that we do not keep any mirrors that are not relevant anymore
	OprhanedServicesGcTriggered struct{}

	gatewayMetadata struct {
		Name      string
		Namespace string
	}
)

func (rcsw *RemoteClusterServiceWatcher) getLocalService(ns, name string) (*corev1.Service, error) {
	svc, err := rcsw.localAPIClient.Svc().Lister().Services(ns).Get(name)
	if err != nil {
		return nil, err
	}
	return svc.DeepCopy(), nil
}

func (rcsw *RemoteClusterServiceWatcher) getLocalEndpoints(ns, name string) (*corev1.Endpoints, error) {
	endpoints, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(ns).Get(name)
	if err != nil {
		return nil, err
	}
	return endpoints.DeepCopy(), nil
}

func (rcsw *RemoteClusterServiceWatcher) listLocalServices(matchLabels map[string]string) ([]*corev1.Service, error) {
	services, err := rcsw.localAPIClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		return nil, err
	}

	var ret []*corev1.Service
	for _, s := range services {
		ret = append(ret, s.DeepCopy())
	}
	return ret, nil
}

func (rcsw *RemoteClusterServiceWatcher) listLocalEndpoints(matchLabels map[string]string) ([]*corev1.Endpoints, error) {
	endpoints, err := rcsw.localAPIClient.Endpoint().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		return nil, err
	}

	var ret []*corev1.Endpoints
	for _, e := range endpoints {
		ret = append(ret, e.DeepCopy())
	}
	return ret, nil
}

func (rcsw *RemoteClusterServiceWatcher) extractGatewayInfo(gateway *corev1.Service) ([]corev1.EndpointAddress, int32, string, error) {
	if len(gateway.Status.LoadBalancer.Ingress) == 0 {
		return nil, 0, "", errors.New("expected gateway to have at lest 1 external Ip address but it has none")
	}

	var gatewayEndpoints []corev1.EndpointAddress
	for _, ingress := range gateway.Status.LoadBalancer.Ingress {
		gatewayEndpoints = append(gatewayEndpoints, corev1.EndpointAddress{
			IP:       ingress.IP,
			Hostname: ingress.Hostname,
		})
	}
	return gatewayEndpoints, gateway.Spec.Ports[0].Port, gateway.ResourceVersion, nil
}

// When the gateway is resolved we need to produce a set of endpoint addresses that that
// contain the external IPs that this gateway exposes. Therefore we return the IP addresses
// as well as a single port on which the gateway is accessible.
func (rcsw *RemoteClusterServiceWatcher) resolveGateway(metadata *gatewayMetadata) ([]corev1.EndpointAddress, int32, string, error) {
	gateway, err := rcsw.remoteAPIClient.Svc().Lister().Services(metadata.Namespace).Get(metadata.Name)
	if err != nil {
		return nil, 0, "", err
	}
	return rcsw.extractGatewayInfo(gateway)
}

// NewRemoteClusterServiceWatcher constructs a new cluster watcher
func NewRemoteClusterServiceWatcher(localAPI *k8s.API, cfg *rest.Config, clusterName string, requeueLimit int) (*RemoteClusterServiceWatcher, error) {
	remoteAPI, err := k8s.InitializeAPIForConfig(cfg, k8s.Svc)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize remote api for cluster %s: %s", clusterName, err)
	}
	stopper := make(chan struct{})
	return &RemoteClusterServiceWatcher{
		clusterName:     clusterName,
		remoteAPIClient: remoteAPI,
		localAPIClient:  localAPI,
		stopper:         stopper,
		log: logging.WithFields(logging.Fields{
			"cluster":    clusterName,
			"apiAddress": cfg.Host,
		}),
		eventsQueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		requeueLimit: requeueLimit,
	}, nil
}

func (rcsw *RemoteClusterServiceWatcher) mirroredResourceName(remoteName, namespace string) string {
	return fmt.Sprintf("%s-%s-%s", remoteName, namespace, rcsw.clusterName)
}

func (rcsw *RemoteClusterServiceWatcher) originalResourceName(mirroredName string) string {
	return strings.TrimSuffix(mirroredName, fmt.Sprintf("-%s", rcsw.clusterName))
}

func (rcsw *RemoteClusterServiceWatcher) getMirroredServiceLabels(service *corev1.Service, gatewayData *gatewayMetadata) map[string]string {
	return map[string]string{
		consts.MirroredResourceLabel:      "true",
		consts.RemoteClusterNameLabel:     rcsw.clusterName,
		consts.RemoteResourceVersionLabel: service.ResourceVersion, // needed to detect real changes
		consts.RemoteGatewayNameLabel:     gatewayData.Name,
		consts.RemoteGatewayNsLabel:       gatewayData.Namespace,
	}
}

func (rcsw *RemoteClusterServiceWatcher) mirrorNamespaceIfNecessary(namespace string) error {
	// if the namespace is already present we do not need to change it.
	// if we are creating it we want to put a label indicating this is a
	// mirrored resource so we delete it when the times comes...
	if _, err := rcsw.localAPIClient.NS().Lister().Get(namespace); err != nil {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					consts.MirroredResourceLabel:  "true",
					consts.RemoteClusterNameLabel: rcsw.clusterName,
				},
				Name: namespace,
			},
		}
		_, err := rcsw.localAPIClient.Client.CoreV1().Namespaces().Create(ns)
		if err != nil {
			return fmt.Errorf("could not create mirror namespace: %s", err)
		}
	}
	return nil
}

// This method takes care of port remapping. What it does essentially is get the one gateway port
// that we should send traffic to and create endpoint ports that bind to the mirrored service ports
// (same name, etc) but send traffic to the gateway port. This way we do not need to do any remapping
// on the service side of things. It all happens in the endpoints.
func (rcsw *RemoteClusterServiceWatcher) getEndpointsPorts(service *corev1.Service, gatewayPort int32) []corev1.EndpointPort {
	var endpointsPorts []corev1.EndpointPort
	for _, remotePort := range service.Spec.Ports {
		endpointsPorts = append(endpointsPorts, corev1.EndpointPort{
			Name:     remotePort.Name,
			Protocol: remotePort.Protocol,
			Port:     gatewayPort,
		})
	}
	return endpointsPorts
}

func (rcsw *RemoteClusterServiceWatcher) cleanupOrphanedServices() error {
	matchLabels := map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteClusterNameLabel: rcsw.clusterName,
	}

	servicesOnLocalCluster, err := rcsw.listLocalServices(matchLabels)
	if err != nil {
		return fmt.Errorf("failed obtaining local services while GC-ing: %s", err)
	}

	for _, srv := range servicesOnLocalCluster {
		_, err := rcsw.remoteAPIClient.Svc().Lister().Services(srv.Namespace).Get(rcsw.originalResourceName(srv.Name))
		if err != nil {
			// service does not exist anymore. Need to delete
			if err := rcsw.localAPIClient.Client.CoreV1().Services(srv.Namespace).Delete(srv.Name, &metav1.DeleteOptions{}); err != nil {
				rcsw.log.Errorf("Failed to GC local service %s", srv.Name)
			} else {
				rcsw.log.Debugf("Deleted service %s/%s as part of GC process", srv.Namespace, srv.Name)
			}
		}
	}
	return nil
}

// Whenever we stop watching a cluster, we need to cleanup everything that we have
// created. This piece of code is responsible for doing just that. It takes care of
// services, endpoints and namespaces (if needed)
func (rcsw *RemoteClusterServiceWatcher) cleanupMirroredResources() error {
	matchLabels := map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteClusterNameLabel: rcsw.clusterName,
	}

	services, err := rcsw.listLocalServices(matchLabels)
	if err != nil {
		return fmt.Errorf("could not retrieve mirrored services that need cleaning up: %s", err)
	}

	for _, svc := range services {
		if err := rcsw.localAPIClient.Client.CoreV1().Services(svc.Namespace).Delete(svc.Name, &metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("Could not delete  service %s/%s: %s", svc.Namespace, svc.Name, err)
		}
		rcsw.log.Debugf("Deleted service %s/%s", svc.Namespace, svc.Name)

	}
	endpoints, err := rcsw.listLocalEndpoints(matchLabels)
	if err != nil {
		return fmt.Errorf("could not retrieve Endpoints that need cleaning up: %s", err)
	}

	for _, endpt := range endpoints {
		if err := rcsw.localAPIClient.Client.CoreV1().Endpoints(endpt.Namespace).Delete(endpt.Name, &metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("Could not delete  Endpoints %s/%s: %s", endpt.Namespace, endpt.Name, err)
		}
		rcsw.log.Debugf("Deleted Endpoints %s/%s", endpt.Namespace, endpt.Name)

	}

	namespaces, err := rcsw.localAPIClient.NS().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		return fmt.Errorf("could not retrieve Namespaces that need cleaning up: %s", err)
	}

	for _, ns := range namespaces {
		if err := rcsw.localAPIClient.Client.CoreV1().Namespaces().Delete(ns.Name, &metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("Could not delete  Namespace %s: %s", ns.Name, err)
		}
		rcsw.log.Debugf("Deleted Namespace %s", ns.Name)

	}
	return nil
}

// Deletes a locally mirrored service as it is not present on the remote cluster anymore
func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceDeleted(ev *RemoteServiceDeleted) error {
	localServiceName := rcsw.mirroredResourceName(ev.Name, ev.Namespace)
	rcsw.log.Debugf("Deleting mirrored service %s/%s and its corresponding Endpoints", ev.Namespace, localServiceName)
	if err := rcsw.localAPIClient.Client.CoreV1().Services(ev.Namespace).Delete(localServiceName, &metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("could not delete Service: %s/%s: %s", ev.Namespace, localServiceName, err)
	}
	rcsw.log.Debugf("Successfully deleted Service: %s/%s", ev.Namespace, localServiceName)
	return nil
}

// Updates a locally mirrored service. There might have been some pretty fundamental changes such as
// new gateway being assigned or additional ports exposed. This method takes care of that.
func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceUpdated(ev *RemoteServiceUpdated) error {
	serviceInfo := fmt.Sprintf("%s/%s", ev.remoteUpdate.Namespace, ev.remoteUpdate.Name)
	rcsw.log.Debugf("Updating remote mirrored service %s/%s", ev.localService.Namespace, ev.localService.Name)

	gatewayEndpoints, gatewayPort, resVersion, err := rcsw.resolveGateway(ev.gatewayData)
	if err == nil {
		ev.localEndpoints.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayEndpoints,
				Ports:     rcsw.getEndpointsPorts(ev.remoteUpdate, gatewayPort),
			},
		}
	} else {
		rcsw.log.Warnf("Could not resolve gateway for %s: %s, nulling endpoints", serviceInfo, err)
		ev.localEndpoints.Subsets = nil
	}

	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(ev.localEndpoints.Namespace).Update(ev.localEndpoints); err != nil {
		return err
	}

	ev.localService.Labels = rcsw.getMirroredServiceLabels(ev.remoteUpdate, ev.gatewayData)
	ev.localService.Labels[consts.RemoteGatewayResourceVersionLabel] = resVersion
	ev.localService.Spec.Ports = ev.remoteUpdate.Spec.Ports

	if _, err := rcsw.localAPIClient.Client.CoreV1().Services(ev.localService.Namespace).Update(ev.localService); err != nil {
		return err
	}
	return nil
}

func remapRemoteServicePorts(ports []corev1.ServicePort) []corev1.ServicePort {
	// We ignore the NodePort here as its not relevant
	// to the local cluster
	var newPorts []corev1.ServicePort
	for _, port := range ports {
		newPorts = append(newPorts, corev1.ServicePort{
			Name:       port.Name,
			Protocol:   port.Protocol,
			Port:       port.Port,
			TargetPort: port.TargetPort,
		})
	}
	return newPorts
}

func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceCreated(ev *RemoteServiceCreated) error {
	remoteService := ev.service.DeepCopy()
	serviceInfo := fmt.Sprintf("%s/%s", remoteService.Namespace, remoteService.Name)
	localServiceName := rcsw.mirroredResourceName(remoteService.Name, remoteService.Namespace)

	if err := rcsw.mirrorNamespaceIfNecessary(remoteService.Namespace); err != nil {
		return err
	}
	// here we always create both a service and endpoints, even if we cannot resolve the gateway
	serviceToCreate := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        localServiceName,
			Namespace:   remoteService.Namespace,
			Annotations: map[string]string{},
			Labels:      rcsw.getMirroredServiceLabels(remoteService, ev.gatewayData),
		},
		Spec: corev1.ServiceSpec{
			Ports: remapRemoteServicePorts(remoteService.Spec.Ports),
		},
	}

	endpointsToCreate := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      localServiceName,
			Namespace: ev.service.Namespace,
			Labels: map[string]string{
				consts.MirroredResourceLabel:  "true",
				consts.RemoteClusterNameLabel: rcsw.clusterName,
				consts.RemoteGatewayNameLabel: ev.gatewayData.Name,
				consts.RemoteGatewayNsLabel:   ev.gatewayData.Namespace,
			},
		},
	}

	// Now we try to resolve the remote gateway
	gatewayEndpoints, gatewayPort, resVersion, err := rcsw.resolveGateway(ev.gatewayData)
	if err == nil {
		// only if we resolve it, we are updating the endpoints addresses and ports
		rcsw.log.Debugf("Resolved remote gateway [%v:%d] for %s", gatewayEndpoints, gatewayPort, serviceInfo)
		endpointsToCreate.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayEndpoints,
				Ports:     rcsw.getEndpointsPorts(ev.service, gatewayPort),
			},
		}

		serviceToCreate.Labels[consts.RemoteGatewayResourceVersionLabel] = resVersion

	} else {
		rcsw.log.Warnf("Could not resolve gateway for %s: %s", serviceInfo, err)
	}

	rcsw.log.Debugf("Creating a new service mirror for %s", serviceInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Services(remoteService.Namespace).Create(serviceToCreate); err != nil {
		return err
	}

	rcsw.log.Debugf("Creating a new Endpoints for %s", serviceInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(ev.service.Namespace).Create(endpointsToCreate); err != nil {
		// we clean up after ourselves
		rcsw.localAPIClient.Client.CoreV1().Services(ev.service.Namespace).Delete(localServiceName, &metav1.DeleteOptions{})
		return err
	}
	return nil
}

func (rcsw *RemoteClusterServiceWatcher) handleRemoteGatewayDeleted(ev *RemoteGatewayDeleted) error {

	affectedEndpoints, err := rcsw.endpointsForGateway(ev.gatewayData)
	if err != nil {
		return err
	}
	if len(affectedEndpoints) > 0 {
		rcsw.log.Debugf("Nulling %d endpoints due to remote gateway [%s/%s] deletion", len(affectedEndpoints), ev.gatewayData.Namespace, ev.gatewayData.Name)
		for _, ep := range affectedEndpoints {
			updated := ep.DeepCopy()
			updated.Subsets = nil
			if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(ep.Namespace).Update(updated); err != nil {
				return err
			}
		}
	}
	return nil
}

func (rcsw *RemoteClusterServiceWatcher) handleRemoteGatewayUpdated(ev *RemoteGatewayUpdated) error {
	rcsw.log.Debugf("Updating %d services due to remote gateway [%s/%s] update", len(ev.affectedServices), ev.gatewayData.Namespace, ev.gatewayData.Name)
	for _, svc := range ev.affectedServices {
		updatedService := svc.DeepCopy()
		if updatedService.Labels != nil {
			updatedService.Labels[consts.RemoteGatewayResourceVersionLabel] = ev.newResourceVersion
		}
		endpoints, err := rcsw.getLocalEndpoints(svc.Namespace, svc.Name)
		if err != nil {
			return err
		}

		updatedEndpoints := endpoints.DeepCopy()
		updatedEndpoints.Subsets = []corev1.EndpointSubset{
			{
				Addresses: ev.newEndpointAddresses,
				Ports:     rcsw.getEndpointsPorts(updatedService, ev.newPort),
			},
		}
		_, err = rcsw.localAPIClient.Client.CoreV1().Services(updatedService.Namespace).Update(updatedService)
		if err != nil {
			return err
		}

		_, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(updatedService.Namespace).Update(updatedEndpoints)
		if err != nil {
			rcsw.localAPIClient.Client.CoreV1().Services(updatedService.Namespace).Delete(updatedService.Name, &metav1.DeleteOptions{})
			return err
		}
	}
	return nil
}

// Retrieves the annotations that indicate this service can be mirrored.
// The values of these annotations help us resolve the gateway to which
// traffic should be sent.
func getGatewayMetadata(annotations map[string]string) *gatewayMetadata {
	remoteGatewayName, hasGtwName := annotations[consts.GatewayNameAnnotation]
	remoteGatewayNs, hasGtwNs := annotations[consts.GatewayNsAnnotation]
	if hasGtwName && hasGtwNs {
		return &gatewayMetadata{
			Name:      remoteGatewayName,
			Namespace: remoteGatewayNs,
		}
	}
	return nil
}
func (rcsw *RemoteClusterServiceWatcher) handleConsiderGatewayUpdateDispatch(event *ConsiderGatewayUpdateDispatch) error {
	gtwMetadata := &gatewayMetadata{
		Name:      event.maybeGateway.Name,
		Namespace: event.maybeGateway.Namespace,
	}

	services, err := rcsw.mirroredServicesForGateway(gtwMetadata)
	if err != nil {
		// we can fail and requeue here in case there is a problem obtaining these...
		return err
	}

	if len(services) > 0 {
		gatewayMeta := &gatewayMetadata{
			Name:      event.maybeGateway.Name,
			Namespace: event.maybeGateway.Namespace,
		}
		if endpoints, port, resVersion, err := rcsw.extractGatewayInfo(event.maybeGateway); err != nil {
			rcsw.log.Warnf("Gateway [%s/%s] is not a compliant gateway anymore, dispatching GatewayDeleted event: %s", event.maybeGateway.Namespace, event.maybeGateway.Name, err)
			// in case something changed about this gateway and it is not really a gateway anymore,
			// simply dispatch deletion event so all endpoints are nulled
			endpoints, err := rcsw.endpointsForGateway(gatewayMeta)
			if err != nil {
				return err
			}
			rcsw.eventsQueue.AddRateLimited(&RemoteGatewayDeleted{gatewayMeta, endpoints})
		} else {

			affectedServices, err := rcsw.affectedMirroredServicesForGatewayUpdate(gtwMetadata, event.maybeGateway.ResourceVersion)
			if err != nil {
				return err
			}

			if len(affectedServices) > 0 {
				rcsw.eventsQueue.Add(&RemoteGatewayUpdated{
					newPort:              port,
					newEndpointAddresses: endpoints,
					gatewayData:          gatewayMeta,
					newResourceVersion:   resVersion,
					affectedServices:     affectedServices,
				})
			}

		}
	}
	return nil
}

func (rcsw *RemoteClusterServiceWatcher) createOrUpdateService(service *corev1.Service) {
	localName := rcsw.mirroredResourceName(service.Name, service.Namespace)
	localService, err := rcsw.getLocalService(service.Namespace, localName)
	gtwData := getGatewayMetadata(service.Annotations)

	if err != nil {
		if kerrors.IsNotFound(err) && gtwData != nil {
			// a service that has been annotated after creation
			rcsw.eventsQueue.Add(&RemoteServiceCreated{
				service:            service,
				gatewayData:        gtwData,
				newResourceVersion: service.ResourceVersion,
			})
		}
		if kerrors.IsNotFound(err) {
			rcsw.eventsQueue.Add(&ConsiderGatewayUpdateDispatch{maybeGateway: service})
		}
	} else {
		if gtwData != nil {
			lastMirroredRemoteVersion, ok := localService.Labels[consts.RemoteResourceVersionLabel]
			if ok && lastMirroredRemoteVersion != service.ResourceVersion {
				endpoints, err := rcsw.getLocalEndpoints(service.Namespace, localName)
				if err == nil {
					rcsw.eventsQueue.Add(&RemoteServiceUpdated{
						localService:   localService,
						localEndpoints: endpoints,
						remoteUpdate:   service,
						gatewayData:    gtwData,
					})
				}
			}
		} else {
			rcsw.eventsQueue.Add(&RemoteServiceDeleted{
				Name:      service.Name,
				Namespace: service.Namespace,
			})
		}
	}
}

func (rcsw *RemoteClusterServiceWatcher) onUpdate(old, new interface{}) {
	rcsw.createOrUpdateService(new.(*corev1.Service))
}

func (rcsw *RemoteClusterServiceWatcher) onAdd(svc interface{}) {
	rcsw.createOrUpdateService(svc.(*corev1.Service))
}

func (rcsw *RemoteClusterServiceWatcher) affectedMirroredServicesForGatewayUpdate(gatewayData *gatewayMetadata, latestResourceVersion string) ([]*corev1.Service, error) {
	services, err := rcsw.mirroredServicesForGateway(gatewayData)
	if err != nil {
		return nil, err
	}

	affectedServices := []*corev1.Service{}
	for _, srv := range services {
		ver, ok := srv.Labels[consts.RemoteGatewayResourceVersionLabel]
		if ok && ver != latestResourceVersion {
			affectedServices = append(affectedServices, srv)
		}
	}
	return affectedServices, nil
}

func (rcsw *RemoteClusterServiceWatcher) mirroredServicesForGateway(gatewayData *gatewayMetadata) ([]*corev1.Service, error) {
	matchLabels := map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteGatewayNameLabel: gatewayData.Name,
		consts.RemoteGatewayNsLabel:   gatewayData.Namespace,
	}

	services, err := rcsw.listLocalServices(matchLabels)
	if err != nil {
		return nil, err
	}
	return services, nil
}

func (rcsw *RemoteClusterServiceWatcher) endpointsForGateway(gatewayData *gatewayMetadata) ([]*corev1.Endpoints, error) {
	matchLabels := map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteGatewayNameLabel: gatewayData.Name,
		consts.RemoteGatewayNsLabel:   gatewayData.Namespace,
	}

	endpoints, err := rcsw.listLocalEndpoints(matchLabels)
	if err != nil {
		return nil, err
	}
	return endpoints, nil
}

func (rcsw *RemoteClusterServiceWatcher) onDelete(svc interface{}) {
	service, ok := svc.(*corev1.Service)
	if !ok {
		tombstone, ok := svc.(cache.DeletedFinalStateUnknown)
		if !ok {
			rcsw.log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", svc)
			return
		}
		service, ok = tombstone.Obj.(*corev1.Service)
		if !ok {
			rcsw.log.Errorf("DeletedFinalStateUnknown contained object that is not a Service %#v", svc)
			return
		}
	}
	if gtwData := getGatewayMetadata(service.Annotations); gtwData != nil {
		rcsw.eventsQueue.Add(&RemoteServiceDeleted{
			Name:      service.Name,
			Namespace: service.Namespace,
		})
	} else {
		rcsw.eventsQueue.Add(&RemoteGatewayDeleted{
			gatewayData: &gatewayMetadata{
				Name:      service.Name,
				Namespace: service.Namespace,
			}})
	}
}

// the main processing loop in which we handle more domain specific events
// and deal with retries
func (rcsw *RemoteClusterServiceWatcher) processEvents() {
	for {
		event, done := rcsw.eventsQueue.Get()
		var err error
		switch ev := event.(type) {
		case *RemoteServiceCreated:
			err = rcsw.handleRemoteServiceCreated(ev)
		case *RemoteServiceUpdated:
			err = rcsw.handleRemoteServiceUpdated(ev)
		case *RemoteServiceDeleted:
			err = rcsw.handleRemoteServiceDeleted(ev)
		case *RemoteGatewayUpdated:
			err = rcsw.handleRemoteGatewayUpdated(ev)
		case *RemoteGatewayDeleted:
			err = rcsw.handleRemoteGatewayDeleted(ev)
		case *ConsiderGatewayUpdateDispatch:
			err = rcsw.handleConsiderGatewayUpdateDispatch(ev)
		case *ClusterUnregistered:
			err = rcsw.cleanupMirroredResources()
		case *OprhanedServicesGcTriggered:
			err = rcsw.cleanupOrphanedServices()
		default:
			if ev != nil || !done { // we get a nil in case we are shutting down...
				rcsw.log.Warnf("Received unknown event: %v", ev)
			}
		}

		// the logic here is that there might have been an API
		// connectivity glitch or something. So its not a bad idea to requeue
		// the event and try again up to a number of limits, just to ensure
		// that we are not diverging in states due to bad luck...
		if err == nil {
			rcsw.eventsQueue.Forget(event)
		} else if (rcsw.eventsQueue.NumRequeues(event) < rcsw.requeueLimit) && !done {
			rcsw.log.Errorf("Error processing %s (will retry): %v", event, err)
			rcsw.eventsQueue.Add(event)
		} else {
			rcsw.log.Errorf("Error processing %s (giving up): %v", event, err)
			rcsw.eventsQueue.Forget(event)
		}
		if done {
			rcsw.log.Debug("Shutting down events processor")
			return
		}
	}
}

// Start starts watching the remote cluster
func (rcsw *RemoteClusterServiceWatcher) Start() {
	rcsw.remoteAPIClient.SyncWithStopCh(rcsw.stopper)
	rcsw.eventsQueue.Add(&OprhanedServicesGcTriggered{})
	rcsw.remoteAPIClient.Svc().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    rcsw.onAdd,
			DeleteFunc: rcsw.onDelete,
			UpdateFunc: rcsw.onUpdate,
		},
	)
	go rcsw.processEvents()
}

// Stop stops watching the cluster and cleans up all mirrored resources
func (rcsw *RemoteClusterServiceWatcher) Stop() {
	close(rcsw.stopper)
	rcsw.eventsQueue.Add(&ClusterUnregistered{})
	rcsw.eventsQueue.ShutDown()
}
