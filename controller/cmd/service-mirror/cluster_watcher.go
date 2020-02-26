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
)

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
		clusterDomain   string
		remoteAPIClient *k8s.API
		localAPIClient  *k8s.API
		stopper         chan struct{}
		log             *logging.Entry
	}

	gatewayMetadata struct {
		Name      string
		Namespace string
	}
)

func (rcsw *RemoteClusterServiceWatcher) extractGatewayInfo(gateway *corev1.Service) ([]corev1.EndpointAddress, int32, string, error) {
	if len(gateway.Status.LoadBalancer.Ingress) == 0 {
		return nil, 0, "", errors.New("expected gateway to have at lest 1 external Ip address but it has none")
	}

	var foundPort = false
	var port int32
	for _, p := range gateway.Spec.Ports {
		if p.Name == consts.GatewayPortName {
			foundPort = true
			port = p.Port
			break
		}
	}

	if !foundPort {
		return nil, 0, "", fmt.Errorf("cannot find  port named %s on gateway", consts.GatewayPortName)
	}

	var gatewayEndpoints []corev1.EndpointAddress
	for _, ingress := range gateway.Status.LoadBalancer.Ingress {
		gatewayEndpoints = append(gatewayEndpoints, corev1.EndpointAddress{
			IP:       ingress.IP,
			Hostname: ingress.Hostname,
		})
	}
	return gatewayEndpoints, port, gateway.ResourceVersion, nil
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
func NewRemoteClusterServiceWatcher(localAPI *k8s.API, cfg *rest.Config, clusterName string, clusterDomain string) (*RemoteClusterServiceWatcher, error) {
	remoteAPI, err := k8s.InitializeAPIForConfig(cfg, false, k8s.Svc)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize remote api for cluster %s: %s", clusterName, err)
	}
	stopper := make(chan struct{})
	return &RemoteClusterServiceWatcher{
		clusterName:     clusterName,
		clusterDomain:   clusterDomain,
		remoteAPIClient: remoteAPI,
		localAPIClient:  localAPI,
		stopper:         stopper,
		log: logging.WithFields(logging.Fields{
			"cluster":    clusterName,
			"apiAddress": cfg.Host,
		}),
	}, nil
}

func (rcsw *RemoteClusterServiceWatcher) mirroredResourceName(remoteName string) string {
	return fmt.Sprintf("%s-%s", remoteName, rcsw.clusterName)
}

func (rcsw *RemoteClusterServiceWatcher) originalResourceName(mirroredName string) string {
	return strings.TrimSuffix(mirroredName, fmt.Sprintf("-%s", rcsw.clusterName))
}

func (rcsw *RemoteClusterServiceWatcher) getMirroredServiceLabels(gatewayData *gatewayMetadata) map[string]string {
	return map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteClusterNameLabel: rcsw.clusterName,
		consts.RemoteGatewayNameLabel: gatewayData.Name,
		consts.RemoteGatewayNsLabel:   gatewayData.Namespace,
	}
}

func (rcsw *RemoteClusterServiceWatcher) getMirroredServiceAnnotations(remoteService *corev1.Service) map[string]string {
	return map[string]string{
		consts.RemoteResourceVersionAnnotation: remoteService.ResourceVersion, // needed to detect real changes
		consts.RemoteServiceFqName:             fmt.Sprintf("%s.%s.svc.%s", remoteService.Name, remoteService.Namespace, rcsw.clusterDomain),
	}
}

func (rcsw *RemoteClusterServiceWatcher) mirrorNamespaceIfNecessary(namespace string) error {
	// if the namespace is already present we do not need to change it.
	// if we are creating it we want to put a label indicating this is a
	// mirrored resource
	if _, err := rcsw.localAPIClient.NS().Lister().Get(namespace); err != nil {
		if kerrors.IsNotFound(err) {
			// if the namespace is not found, we can just create it
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
				// something went wrong with the create, we can just retry as well
				return err
			}
		} else {
			// something else went wrong, so we can just retry
			return err
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

	servicesOnLocalCluster, err := rcsw.localAPIClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		return fmt.Errorf("failed obtaining local services while GC-ing: %s", err)
	}
	for _, srv := range servicesOnLocalCluster {
		_, err := rcsw.remoteAPIClient.Svc().Lister().Services(srv.Namespace).Get(rcsw.originalResourceName(srv.Name))
		if err != nil {
			if kerrors.IsNotFound(err) {
				// service does not exist anymore. Need to delete
				if err := rcsw.localAPIClient.Client.CoreV1().Services(srv.Namespace).Delete(srv.Name, &metav1.DeleteOptions{}); err != nil {
					// something went wrong with deletion, we need to retry
					return err
				}
				rcsw.log.Debugf("Deleted service %s/%s as part of GC process", srv.Namespace, srv.Name)

			} else {
				// something went wrong getting the service, we can retry
				return err
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

	services, err := rcsw.localAPIClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		return fmt.Errorf("could not retrieve mirrored services that need cleaning up: %s", err)
	}

	for _, svc := range services {
		if err := rcsw.localAPIClient.Client.CoreV1().Services(svc.Namespace).Delete(svc.Name, &metav1.DeleteOptions{}); err != nil {
			if kerrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("Could not delete  service %s/%s: %s", svc.Namespace, svc.Name, err)
		}
		rcsw.log.Debugf("Deleted service %s/%s", svc.Namespace, svc.Name)

	}

	endpoints, err := rcsw.localAPIClient.Endpoint().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		return fmt.Errorf("could not retrieve Endpoints that need cleaning up: %s", err)
	}

	for _, endpt := range endpoints {
		if err := rcsw.localAPIClient.Client.CoreV1().Endpoints(endpt.Namespace).Delete(endpt.Name, &metav1.DeleteOptions{}); err != nil {
			if kerrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("Could not delete  Endpoints %s/%s: %s", endpt.Namespace, endpt.Name, err)
		}
		rcsw.log.Debugf("Deleted Endpoints %s/%s", endpt.Namespace, endpt.Name)

	}

	return nil
}

// Deletes a locally mirrored service as it is not present on the remote cluster anymore
func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceDeleted(name string,
	namespace string) error {
	localServiceName := rcsw.mirroredResourceName(name)
	rcsw.log.Debugf("Deleting mirrored service %s/%s and its corresponding Endpoints", namespace, localServiceName)
	if err := rcsw.localAPIClient.Client.CoreV1().Services(namespace).Delete(localServiceName, &metav1.DeleteOptions{}); err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("could not delete Service: %s/%s: %s", namespace, localServiceName, err)

	}
	rcsw.log.Debugf("Successfully deleted Service: %s/%s", namespace, localServiceName)
	return nil
}

// Updates a locally mirrored service. There might have been some pretty fundamental changes such as
// new gateway being assigned or additional ports exposed. This method takes care of that.
func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceUpdated(localService *corev1.Service,
	localEndpoints *corev1.Endpoints,
	remoteUpdate *corev1.Service,
	gatewayData *gatewayMetadata) error {
	serviceInfo := fmt.Sprintf("%s/%s", remoteUpdate.Namespace, remoteUpdate.Name)
	rcsw.log.Debugf("Updating remote mirrored service %s/%s", localService.Namespace, localService.Name)

	gatewayEndpoints, gatewayPort, resVersion, err := rcsw.resolveGateway(gatewayData)
	if err == nil {
		localEndpoints.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayEndpoints,
				Ports:     rcsw.getEndpointsPorts(remoteUpdate, gatewayPort),
			},
		}
	} else {
		rcsw.log.Warnf("Could not resolve gateway for %s: %s, nulling endpoints", serviceInfo, err)
		localEndpoints.Subsets = nil
	}

	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(localEndpoints.Namespace).Update(localEndpoints); err != nil {
		return err
	}

	localService.Labels = rcsw.getMirroredServiceLabels(gatewayData)
	localService.Annotations = rcsw.getMirroredServiceAnnotations(remoteUpdate)
	localService.Annotations[consts.RemoteGatewayResourceVersionAnnotation] = resVersion
	localService.Spec.Ports = remapRemoteServicePorts(remoteUpdate.Spec.Ports)

	if _, err := rcsw.localAPIClient.Client.CoreV1().Services(localService.Namespace).Update(localService); err != nil {
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

func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceCreated(service *corev1.Service, gatewayData *gatewayMetadata) error {
	remoteService := service.DeepCopy()
	serviceInfo := fmt.Sprintf("%s/%s", remoteService.Namespace, remoteService.Name)
	localServiceName := rcsw.mirroredResourceName(remoteService.Name)

	if err := rcsw.mirrorNamespaceIfNecessary(remoteService.Namespace); err != nil {
		return err
	}
	// here we always create both a service and endpoints, even if we cannot resolve the gateway
	serviceToCreate := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        localServiceName,
			Namespace:   remoteService.Namespace,
			Annotations: rcsw.getMirroredServiceAnnotations(remoteService),
			Labels:      rcsw.getMirroredServiceLabels(gatewayData),
		},
		Spec: corev1.ServiceSpec{
			Ports: remapRemoteServicePorts(remoteService.Spec.Ports),
		},
	}

	endpointsToCreate := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      localServiceName,
			Namespace: service.Namespace,
			Labels: map[string]string{
				consts.MirroredResourceLabel:  "true",
				consts.RemoteClusterNameLabel: rcsw.clusterName,
				consts.RemoteGatewayNameLabel: gatewayData.Name,
				consts.RemoteGatewayNsLabel:   gatewayData.Namespace,
			},
		},
	}

	// Now we try to resolve the remote gateway
	gatewayEndpoints, gatewayPort, resVersion, err := rcsw.resolveGateway(gatewayData)
	if err == nil {
		// only if we resolve it, we are updating the endpoints addresses and ports
		rcsw.log.Debugf("Resolved remote gateway [%v:%d] for %s", gatewayEndpoints, gatewayPort, serviceInfo)
		endpointsToCreate.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayEndpoints,
				Ports:     rcsw.getEndpointsPorts(service, gatewayPort),
			},
		}

		serviceToCreate.Annotations[consts.RemoteGatewayResourceVersionAnnotation] = resVersion

	} else {
		rcsw.log.Warnf("Could not resolve gateway for %s: %s, skipping subsets", serviceInfo, err)
		endpointsToCreate.Subsets = nil
	}

	rcsw.log.Debugf("Creating a new service mirror for %s", serviceInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Services(remoteService.Namespace).Create(serviceToCreate); err != nil {
		if !kerrors.IsAlreadyExists(err) {
			// we might have created it during earlier attempt, if that is not the case, we retry
			return err
		}
	}

	rcsw.log.Debugf("Creating a new Endpoints for %s", serviceInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(service.Namespace).Create(endpointsToCreate); err != nil {
		rcsw.localAPIClient.Client.CoreV1().Services(service.Namespace).Delete(localServiceName, &metav1.DeleteOptions{})
		return err
	}
	return nil
}

func (rcsw *RemoteClusterServiceWatcher) handleRemoteGatewayDeleted(gatewayData *gatewayMetadata, affectedEndpoints []*corev1.Endpoints) error {
	if len(affectedEndpoints) > 0 {
		rcsw.log.Debugf("Nulling %d endpoints due to remote gateway [%s/%s] deletion", len(affectedEndpoints), gatewayData.Namespace, gatewayData.Name)
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

func (rcsw *RemoteClusterServiceWatcher) handleRemoteGatewayUpdated(
	newPort int32,
	newEndpointAddresses []corev1.EndpointAddress,
	gatewayData *gatewayMetadata,
	newResourceVersion string,
	affectedServices []*corev1.Service,
) error {
	rcsw.log.Debugf("Updating %d services due to remote gateway [%s/%s] update", len(affectedServices), gatewayData.Namespace, gatewayData.Name)

	for _, svc := range affectedServices {
		updatedService := svc.DeepCopy()
		if updatedService.Labels != nil {
			updatedService.Annotations[consts.RemoteGatewayResourceVersionAnnotation] = newResourceVersion
		}
		endpoints, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(svc.Namespace).Get(svc.Name)
		if err != nil {
			return fmt.Errorf("Could not get endpoints: %s", err)
		}

		updatedEndpoints := endpoints.DeepCopy()
		updatedEndpoints.Subsets = []corev1.EndpointSubset{
			{
				Addresses: newEndpointAddresses,
				Ports:     rcsw.getEndpointsPorts(updatedService, newPort),
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
func (rcsw *RemoteClusterServiceWatcher) handleConsiderGatewayUpdateDispatch(maybeGateway *corev1.Service) error {
	gtwMetadata := &gatewayMetadata{
		Name:      maybeGateway.Name,
		Namespace: maybeGateway.Namespace,
	}

	services, err := rcsw.mirroredServicesForGateway(gtwMetadata)
	if err != nil {
		return err
	}

	if len(services) > 0 {
		gatewayMeta := &gatewayMetadata{
			Name:      maybeGateway.Name,
			Namespace: maybeGateway.Namespace,
		}

		endpoints, port, resVersion, err := rcsw.extractGatewayInfo(maybeGateway)

		if err != nil {
			rcsw.log.Warnf("Gateway [%s/%s] is not a compliant gateway anymore, dispatching GatewayDeleted event: %s", maybeGateway.Namespace, maybeGateway.Name, err)
			// in case something changed about this gateway and it is not really a gateway anymore,
			// simply dispatch deletion event so all endpoints are nulled
			endpoints, err := rcsw.endpointsForGateway(gatewayMeta)
			if err != nil {
				return err
			}
			return rcsw.handleRemoteGatewayDeleted(gatewayMeta, endpoints)
		}
		affectedServices, err := rcsw.affectedMirroredServicesForGatewayUpdate(gtwMetadata, maybeGateway.ResourceVersion)
		if err != nil {
			return err
		}

		if len(affectedServices) > 0 {
			return rcsw.handleRemoteGatewayUpdated(port, endpoints, gatewayMeta, resVersion, affectedServices)
		}
	}
	return nil
}

// this method is common to both CREATE and UPDATE because if we have been
// offline for some time due to a crash a CREATE for a service that we have
// observed before is simply a case of UPDATE
func (rcsw *RemoteClusterServiceWatcher) createOrUpdateService(service *corev1.Service) error {
	localName := rcsw.mirroredResourceName(service.Name)
	localService, err := rcsw.localAPIClient.Svc().Lister().Services(service.Namespace).Get(localName)
	gtwData := getGatewayMetadata(service.Annotations)

	if err != nil {
		if kerrors.IsNotFound(err) {
			if gtwData != nil {
				// at this point we know that this is a service that
				// we are not mirroring but has gateway data, so we need
				// to create it
				return rcsw.handleRemoteServiceCreated(service, gtwData)
			}
			// at this point we know that we do not have such a service
			// and the remote service does not have metadata. So we try to
			// dispatch a gateway update as the remote service might be a
			/// gateway for some of our already mirrored services
			return rcsw.handleConsiderGatewayUpdateDispatch(service)
		}
		return err

	}
	if gtwData != nil {
		// at this point we know this is an update to a service that we already
		// have locally, so we try and see whether the res version has changed
		// and if so, dispatch an RemoteServiceUpdated event
		lastMirroredRemoteVersion, ok := localService.Annotations[consts.RemoteResourceVersionAnnotation]
		if ok && lastMirroredRemoteVersion != service.ResourceVersion {
			endpoints, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(service.Namespace).Get(localName)
			if err == nil {
				return rcsw.handleRemoteServiceUpdated(localService, endpoints, service, gtwData)
			}
			return err

		}
	} else {
		// if this is missing gateway metadata, but we have the
		// service we can dispatch a RemoteServiceDeleted event
		// because at some point in time we mirrored this service,
		// however it is not mirrorable anymore
		return rcsw.handleRemoteServiceDeleted(service.Name, service.Namespace)
	}

	return nil
}

func (rcsw *RemoteClusterServiceWatcher) affectedMirroredServicesForGatewayUpdate(gatewayData *gatewayMetadata, latestResourceVersion string) ([]*corev1.Service, error) {
	services, err := rcsw.mirroredServicesForGateway(gatewayData)
	if err != nil {
		return nil, err
	}

	affectedServices := []*corev1.Service{}
	for _, srv := range services {
		ver, ok := srv.Annotations[consts.RemoteGatewayResourceVersionAnnotation]
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

	services, err := rcsw.localAPIClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
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

	endpoints, err := rcsw.localAPIClient.Endpoint().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		return nil, err
	}
	return endpoints, nil
}

func (rcsw *RemoteClusterServiceWatcher) handleOnDelete(service *corev1.Service) error {
	if gtwData := getGatewayMetadata(service.Annotations); gtwData != nil {
		return rcsw.handleRemoteServiceDeleted(service.Name, service.Namespace)
	}
	meta := &gatewayMetadata{
		Name:      service.Name,
		Namespace: service.Namespace,
	}

	endpoints, err := rcsw.endpointsForGateway(meta)
	if err != nil {
		return err
	}
	return rcsw.handleRemoteGatewayDeleted(meta, endpoints)

}

// Start starts watching the remote cluster
func (rcsw *RemoteClusterServiceWatcher) Start() {
	rcsw.remoteAPIClient.Sync(rcsw.stopper)
	if err := rcsw.cleanupOrphanedServices(); err != nil {
		rcsw.log.Error(err)
	}

	rcsw.remoteAPIClient.Svc().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(svc interface{}) {
				if err := rcsw.createOrUpdateService(svc.(*corev1.Service)); err != nil {
					rcsw.log.Error(err)
				}
			},
			DeleteFunc: func(obj interface{}) {
				service, ok := obj.(*corev1.Service)
				if !ok {
					tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
					if !ok {
						rcsw.log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
						return
					}
					service, ok = tombstone.Obj.(*corev1.Service)
					if !ok {
						rcsw.log.Errorf("DeletedFinalStateUnknown contained object that is not a Service %#v", obj)
						return
					}
				}
				if err := rcsw.handleOnDelete(service); err != nil {
					rcsw.log.Error(err)
				}
			},
			UpdateFunc: func(old, new interface{}) {
				if err := rcsw.createOrUpdateService(new.(*corev1.Service)); err != nil {
					rcsw.log.Error(err)
				}
			},
		},
	)
}

// Stop stops watching the cluster and cleans up all mirrored resources
func (rcsw *RemoteClusterServiceWatcher) Stop(cleanupState bool) {
	close(rcsw.stopper)
	if cleanupState {
		if err := rcsw.cleanupMirroredResources(); err != nil {
			rcsw.log.Error(err)
		}
	}
}
