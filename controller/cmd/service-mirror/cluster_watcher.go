package servicemirror

import (
	"fmt"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"strings"
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
		remoteApiClient *k8s.API
		localApiClient  *k8s.API
		stopper         chan struct{}
		log             *logging.Entry
		eventsQueue     workqueue.RateLimitingInterface
	}

	// Generated whenever a remote service is created Observing this event means
	// that the service in question is not mirrored
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
		gatewayData        *gatewayMetadata
	}

	// RemoteServiceDeleted when a remote service is going away
	RemoteServiceDeleted struct {
		Name      string
		Namespace string
	}

	// When a service that is a gateway to at least one already mirrored service
	// is deleted
	RemoteGatewayDeleted struct {
		Name      string
		Namespace string
	}

	// When a service that is a gateway to at least one already mirrored service
	// is updated. This might mean an IP change, incoming port change, etc...
	RemoteGatewayUpdated struct {
		old *corev1.Service
		new *corev1.Service
	}

	// Issued when the secret containing the remote cluster access information is deleted
	ClusterUnregistered struct{}

	// A self-triggered event which aims to delete any orphaned services that are no
	// longer on the remote cluster. It is emitted every time a new remote cluster is
	// registered for monitoring. The need for this arises because the following
	// might happen. A cluster is registered for monitoring service A,B,C are created.
	// Then this component crashes, leaving the mirrors around. In the meantime services
	// B and C are deleted. When the controller starts up again and registers to listen for
	// these services, we need to delete them as deletion events will not be received.
	OprhanedServicesGcTriggered struct{}

gatewayMetadata struct {
Name string
Namespace string
}
)

// When the gateway is resolved we need to produce a set of endpoint addresses that that
// contains the external IPs that this gateway exposes. Therefore we return the IP addresses
// as well as a single port on which the gateway is accessible.
func (gw *RemoteClusterServiceWatcher) resolveGateway(metadata *gatewayMetadata) ([]corev1.EndpointAddress, int32, error) {
	gateway, err := gw.remoteApiClient.Svc().Lister().Services(metadata.Namespace).Get(metadata.Name)
	if err != nil {
		return nil, 0, err
	}
	if len(gateway.Status.LoadBalancer.Ingress) < 1 {
		return nil, 0, fmt.Errorf("expected gateway to have at lest 1 external Ip address but it has %d", len(gateway.Spec.ExternalIPs))
	}

	var gatewayEndpoints []corev1.EndpointAddress
	for _, ingress := range gateway.Status.LoadBalancer.Ingress {
		gatewayEndpoints = append(gatewayEndpoints, corev1.EndpointAddress{
			IP:       ingress.IP,
			Hostname: ingress.Hostname,
		})
	}

	//TODO: We take the first defined port here. We need to think about that...
	// The problem stems from the fact that if we have more than two ports,
	// there is no real way to create a service that can route to the correct one.
	// For example, say we have ServiceA on the remote cluster exposing port 8080
	// port 8081 and port 8082 and associated with GatewayB that exposes ports 80 and port 443.
	// When we create a mirrored service locally for every port that it exposes we need to
	// associate one target port for the remote gateway. So [8080, 8081, 8082] -> 80 or
	// [8080, 8081, 8082] -> 443. Cannot do both ports I think...
	return gatewayEndpoints, gateway.Spec.Ports[0].Port, nil
}

// NewRemoteClusterServiceWatcher constructs a new cluster watcher
func NewRemoteClusterServiceWatcher(localApi *k8s.API, cfg *rest.Config, clusterName string) (*RemoteClusterServiceWatcher, error) {
	remoteApi, err := k8s.InitializeAPIForConfig(cfg, k8s.Svc)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize remote api for cluster %s: %s", clusterName, err)
	}
	stopper := make(chan struct{})
	return &RemoteClusterServiceWatcher{
		clusterName:     clusterName,
		remoteApiClient: remoteApi,
		localApiClient:  localApi,
		stopper:         stopper,
		log: logging.WithFields(logging.Fields{
			"cluster":    clusterName,
			"apiAddress": cfg.Host,
		}),
		eventsQueue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}, nil
}

func (sw *RemoteClusterServiceWatcher) mirroredResourceName(remoteName string) string {
	return fmt.Sprintf("%s-%s", remoteName, sw.clusterName)
}

func (sw *RemoteClusterServiceWatcher) originalResourceName(mirroredName string) string {
	return strings.TrimSuffix(mirroredName, fmt.Sprintf("-%s", sw.clusterName))
}

func (sw *RemoteClusterServiceWatcher) getMirroredServiceLabels(service *corev1.Service) map[string]string {
	newLabels := map[string]string{
		MirroredResourceLabel:      "true",
		RemoteClusterNameLabel:     sw.clusterName,
		RemoteResourceVersionLabel: service.ResourceVersion, // needed to detect real changes
	}
	for k, v := range service.Labels {
		// rewrite the mirroring ones
		if k == GatewayNameAnnotation {
			k = RemoteGatewayNameLabel
		}
		if k == GatewayNsAnnotation {
			k = RemoteGatewayNsLabel
		}
		newLabels[k] = v
	}
	return newLabels
}

func (sw *RemoteClusterServiceWatcher) mirrorNamespaceIfNecessary(namespace string) error {
	// if the namespace is already present we do not need to change it.
	// if we are creating it we want to put a label indicating this is a
	// mirrored resource so we delete it when the times comes...
	if _, err := sw.localApiClient.NS().Lister().Get(namespace); err != nil {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					MirroredResourceLabel:  "true",
					RemoteClusterNameLabel: sw.clusterName,
				},
				Name: namespace,
			},
		}
		_, err := sw.localApiClient.Client.CoreV1().Namespaces().Create(ns)
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
func (sw *RemoteClusterServiceWatcher) getEndpointsPorts(service *corev1.Service, gatewayPort int32) []corev1.EndpointPort {
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

func (sw *RemoteClusterServiceWatcher) cleanupOrphanedServices() error {
	matchLabels := map[string]string{
		MirroredResourceLabel:  "true",
		RemoteClusterNameLabel: sw.clusterName,
	}

	servicesOnLocalCluster, err := sw.localApiClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		return fmt.Errorf("failed obtaining local services while GC-ing: %s",err)
	}

	for _, srv := range servicesOnLocalCluster {
		_, err := sw.remoteApiClient.Svc().Lister().Services(srv.Namespace).Get(sw.originalResourceName(srv.Name))
		if err != nil {
			// service does not exist anymore. Need to delete
			if err := sw.localApiClient.Client.CoreV1().Services(srv.Namespace).Delete(srv.Name, &metav1.DeleteOptions{}); err != nil {
				sw.log.Errorf("Failed to GC local service %", srv.Name)
			} else {
				sw.log.Debug("Deleted service s/%s as part of GC process", srv.Namespace,srv.Name)
			}
		}
	}
	return nil
}

// Whenever we stop watching a cluster, we need to cleanup everything that we have
// created. This piece of code is responsible for doing just that. It takes care of
// services, endpoints and namespaces (if needed)
func (sw *RemoteClusterServiceWatcher) cleanupMirroredResources() error {
	matchLabels := map[string]string{
		MirroredResourceLabel:  "true",
		RemoteClusterNameLabel: sw.clusterName,
	}
	services, err := sw.localApiClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		sw.log.Errorf("Could not retrieve mirrored services that need cleaning up: %s", err)
	}

	for _, svc := range services {
		if err := sw.localApiClient.Client.CoreV1().Services(svc.Namespace).Delete(svc.Name, &metav1.DeleteOptions{}); err != nil {
			sw.log.Errorf("Could not delete  service %s/%s: ", svc.Namespace, svc.Name, err)

		} else {
			sw.log.Debugf("Deleted service %s/%s", svc.Namespace, svc.Name)

		}
	}
	edpoints, err := sw.localApiClient.Endpoint().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		sw.log.Errorf("Could not retrieve Endpoints that need cleaning up: %s", err)
	}

	for _, endpt := range edpoints {
		if err := sw.localApiClient.Client.CoreV1().Endpoints(endpt.Namespace).Delete(endpt.Name, &metav1.DeleteOptions{}); err != nil {
			sw.log.Errorf("Could not delete  Endpoints %s/%s: ", endpt.Namespace, endpt.Name, err)
		} else {
			sw.log.Debugf("Deleted Endpoints %s/%s", endpt.Namespace, endpt.Name)
		}
	}

	namespaces, err := sw.localApiClient.NS().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		sw.log.Errorf("Could not retrieve Namespaces that need cleaning up: %s", err)
	}

	for _, ns := range namespaces {
		if err := sw.localApiClient.Client.CoreV1().Namespaces().Delete(ns.Name, &metav1.DeleteOptions{}); err != nil {
			sw.log.Errorf("Could not delete  Namespace %s: ", ns.Name, err)
		} else {
			sw.log.Debugf("Deleted Namespace %s", ns.Name)

		}
	}
	return nil
}

// Deletes a locally mirrored service as it is not present on the remote cluster anymore
func (sw *RemoteClusterServiceWatcher) handleRemoteServiceDeleted(ev *RemoteServiceDeleted) error {
	localServiceName := sw.mirroredResourceName(ev.Name)
	sw.log.Debugf("Deleting mirrored service %s/%s and its corresponding Endpoints", ev.Namespace, localServiceName)
	if err := sw.localApiClient.Client.CoreV1().Services(ev.Namespace).Delete(localServiceName, &metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("could not delete Service: %s/%s: %s", ev.Namespace, localServiceName, err)
	}
	sw.log.Debugf("Successfully deleted Service: %s/%s", ev.Namespace, localServiceName)
	return nil
}

// Updates a locally mirrored service. There might have been some pretty fundamental changes such as
// new gateway being assigned or additional ports exposed. This method takes care of that.
func (sw *RemoteClusterServiceWatcher) handleRemoteServiceUpdated(ev *RemoteServiceUpdated) error {
	//TODO: We have all the data to figure out whether ports or gateways gave been updated.
	// If we can do that we can short circuit here and avoid calling the k8s api just to try and
	// update things that have not changed.
	gatewayEndpoints, gatewayPort, err := sw.resolveGateway(ev.gatewayData)
	if err != nil {
		return err
	}
	ev.localEndpoints.Subsets = []corev1.EndpointSubset{
		{
			Addresses: gatewayEndpoints,
			Ports:     sw.getEndpointsPorts(ev.remoteUpdate, gatewayPort),
		},
	}

	if _, err := sw.localApiClient.Client.CoreV1().Endpoints(ev.localEndpoints.Namespace).Update(ev.localEndpoints); err != nil {
		return err
	}

	ev.localService.Labels = sw.getMirroredServiceLabels(ev.remoteUpdate)
	ev.localService.Spec.Ports = ev.remoteUpdate.Spec.Ports

	if _, err := sw.localApiClient.Client.CoreV1().Services(ev.localService.Namespace).Update(ev.localService); err != nil {
		return err
	}
	return nil
}

func (sw *RemoteClusterServiceWatcher) handleRemoteServiceCreated(ev *RemoteServiceCreated) error {
	serviceInfo := fmt.Sprintf("%s/%s", ev.service.Namespace, ev.service.Name)
	sw.log.Debugf("Creating new service mirror for: %s", serviceInfo)
	localServiceName := sw.mirroredResourceName(ev.service.Name)

	gatewayEndpoints, gatewayPort, err := sw.resolveGateway(ev.gatewayData)
	if err != nil {
		return err
	}
	sw.log.Debugf("Resolved remote gateway [%v:%d] for %s", gatewayEndpoints, gatewayPort, serviceInfo)
	if err := sw.mirrorNamespaceIfNecessary(ev.service.Namespace); err != nil {
		return err
	}

	serviceToCreate := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        localServiceName,
			Namespace:   ev.service.Namespace,
			Annotations: map[string]string{},
			Labels:      sw.getMirroredServiceLabels(ev.service),
		},
		Spec: corev1.ServiceSpec{
			Ports: ev.service.Spec.Ports,
		},
	}

	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      localServiceName,
			Namespace: ev.service.Namespace,
			Labels: map[string]string{
				MirroredResourceLabel:  "true",
				RemoteClusterNameLabel: sw.clusterName,
			},
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: gatewayEndpoints,
				Ports:     sw.getEndpointsPorts(ev.service, gatewayPort),
			},
		},
	}
	sw.log.Debugf("Creating a new service mirror for %s", serviceInfo)
	if _, err := sw.localApiClient.Client.CoreV1().Services(ev.service.Namespace).Create(serviceToCreate); err != nil {
		return err
	}
	sw.log.Debugf("Creating a new Endpoints for %s", serviceInfo)
	if _, err := sw.localApiClient.Client.CoreV1().Endpoints(ev.service.Namespace).Create(endpoints); err != nil {
		// we clean up after ourselves
		sw.localApiClient.Client.CoreV1().Services(ev.service.Namespace).Delete(localServiceName, &metav1.DeleteOptions{})
		return err
	}
	return nil
}

// Retrieves the annotations that indicate this service can be mirrored.
// The values of these annotations help us resolve the gateway to which
// traffic should be sent.
func getGatewayMetadata(annotations map[string]string) *gatewayMetadata {
	remoteGatewayName, hasGtwName := annotations[GatewayNameAnnotation]
	remoteGatewayNs, hasGtwNs := annotations[GatewayNsAnnotation]
	if hasGtwName && hasGtwNs {
		return &gatewayMetadata{
			Name: remoteGatewayName,
			Namespace: remoteGatewayNs,
		}
	}
	return nil
}


func (sw *RemoteClusterServiceWatcher) onUpdate(old, new interface{}) {
	oldService := old.(*corev1.Service)
	newService := new.(*corev1.Service)

	if oldService.ResourceVersion != newService.ResourceVersion {
		if gtwData := getGatewayMetadata(newService.Annotations); gtwData != nil {
			localName := sw.mirroredResourceName(newService.Name)
			localService, err := sw.localApiClient.Svc().Lister().Services(newService.Namespace).Get(localName)
			if err == nil && localService != nil {
				lastMirroredRemoteVersion, ok := localService.Labels[RemoteResourceVersionLabel]
				if ok && lastMirroredRemoteVersion != newService.ResourceVersion {
					endpoints, err := sw.localApiClient.Endpoint().Lister().Endpoints(newService.Namespace).Get(localName)
					if err == nil {
						sw.eventsQueue.AddRateLimited(&RemoteServiceUpdated{
							localService:   localService,
							localEndpoints: endpoints,
							remoteUpdate:   newService,
							gatewayData: gtwData,
						})
					}
				}
			}
		}
	}
}

func (sw *RemoteClusterServiceWatcher) onAdd(svc interface{}) {
	service := svc.(*corev1.Service)

	localName := sw.mirroredResourceName(service.Name)
	if gtwMeta := getGatewayMetadata(service.Annotations); gtwMeta != nil {
		// a service that is a candidate for being mirrored as it has the needed annotations
		localService, err := sw.localApiClient.Svc().Lister().Services(service.Namespace).Get(localName)
		if err != nil {
			// in this case we need to create a new service as we do not have one present
			sw.eventsQueue.AddRateLimited(&RemoteServiceCreated{
				service:            service,
				gatewayData: gtwMeta,
				newResourceVersion: service.ResourceVersion,
			})
		} else {
			lastMirroredRemoteVersion, ok := localService.Labels[RemoteResourceVersionLabel]
			if ok && lastMirroredRemoteVersion != service.ResourceVersion {
				// Why might we see an ADD for a service we already have? Well, if our
				// controller has been restarted it will get ADDs for all services that
				// are the current snapshot of the remote cluster. In this case we need to
				// see whether anything has changed while we were doing and if it has,
				// translate that as an UPDATE. If nothing has changed, we can just skip
				// that event. This is the reason why we keep the last observed resource
				// version of the remote object locally.
				endpoints, err := sw.localApiClient.Endpoint().Lister().Endpoints(service.Namespace).Get(localName)
				if err == nil {
					sw.eventsQueue.AddRateLimited(&RemoteServiceUpdated{
						localService:   localService,
						localEndpoints: endpoints,
						remoteUpdate:   service,
						gatewayData: gtwMeta,
					})
				}
			}
		}
	} else {
		//TODO: Handle gateways!!!
	}
}

func (sw *RemoteClusterServiceWatcher) onDelete(svc interface{}) {
	var service *corev1.Service
	if ev, ok := svc.(cache.DeletedFinalStateUnknown); ok {
		service = ev.Obj.(*corev1.Service)
	} else {
		service = svc.(*corev1.Service)
	}

	_, hasGtwName := service.Annotations[GatewayNameAnnotation]
	_, hasGtwNs := service.Annotations[GatewayNsAnnotation]
	if hasGtwName && hasGtwNs {
		sw.eventsQueue.AddRateLimited(&RemoteServiceDeleted{
			Name:      service.Name,
			Namespace: service.Namespace,
		})
	} else {
		//TODO: Handle gateways!!!
	}
}

// the main processing loop in which we handle more domain specific events
// and deal with retries
func (sw *RemoteClusterServiceWatcher) processEvents() {
	maxRetries := 3 // extract magic
	for {
		event, done := sw.eventsQueue.Get()
		var err error
		switch ev := event.(type) {
		case *RemoteServiceCreated:
			err = sw.handleRemoteServiceCreated(ev)
		case *RemoteServiceUpdated:
			err = sw.handleRemoteServiceUpdated(ev)
		case *RemoteServiceDeleted:
			err = sw.handleRemoteServiceDeleted(ev)
		case *RemoteGatewayUpdated:
			//TODO: Handle it...
		case *RemoteGatewayDeleted:
			//TODO: Handle it...
		case *ClusterUnregistered:
			err = sw.cleanupMirroredResources()
		case *OprhanedServicesGcTriggered:
			err = sw.cleanupOrphanedServices()
		default:
			if ev != nil || !done { // we get a nil in case we are shutting down...
				sw.log.Warnf("Received unknown event: %v", ev)
			}
		}

		// the logic here is that there might have been an API
		// connectivity glitch or something. So its not a bad idea to requeue
		// the event and try again up to a number of limits, just to ensure
		// that we are not diverging in states due to bad luck...
		if err == nil {
			sw.eventsQueue.Forget(event)
		} else if (sw.eventsQueue.NumRequeues(event) < maxRetries) && !done {
			sw.log.Errorf("Error processing %s (will retry): %v", event, err)
			sw.eventsQueue.AddRateLimited(event)
		} else {
			sw.log.Errorf("Error processing %s (giving up): %v", event, err)
			sw.eventsQueue.Forget(event)
		}
		if done {
			sw.log.Debug("Shutting down events processor")
			return
		}
	}
}

// Start starts watching the remote cluster
func (sw *RemoteClusterServiceWatcher) Start() {
	sw.remoteApiClient.SyncWithStopCh(sw.stopper)
	sw.remoteApiClient.Svc().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    sw.onAdd,
			DeleteFunc: sw.onDelete,
			UpdateFunc: sw.onUpdate,
		})
	go sw.processEvents()
}

// Stop stops watching the cluster and cleans up all mirrored resources
func (sw *RemoteClusterServiceWatcher) Stop() {
	close(sw.stopper)
	sw.eventsQueue.AddRateLimited(&ClusterUnregistered{})
	sw.eventsQueue.ShutDown()
}
