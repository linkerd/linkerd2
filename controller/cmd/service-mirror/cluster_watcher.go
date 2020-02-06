package servicemirror

import (
	"fmt"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/workqueue"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		gatewayNs          string
		gatewayName        string
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
		gatewayNs      string
		gatewayName    string
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
	//TODO: Maybe do that as a general step in the beginning instead of per cluster...
	OprhanedServicesGcTriggered struct{}
)

func (gw *RemoteClusterServiceWatcher) resolveGateway(namespace string, gatewayName string) ([]corev1.EndpointAddress, int32, error) {
	gateway, err := gw.remoteApiClient.Svc().Lister().Services(namespace).Get(gatewayName)
	if err != nil {
		return nil, 0, err
	}
	if len(gateway.Status.LoadBalancer.Ingress) < 1 {
		return nil, 0, fmt.Errorf("expected gateway to have at lest 1 external Ip address but it has %d", len(gateway.Spec.ExternalIPs))
	}

	var gatewayEndpoints []corev1.EndpointAddress
	for _, ingress := range gateway.Status.LoadBalancer.Ingress {
		gatewayEndpoints = append(gatewayEndpoints, corev1.EndpointAddress{IP: ingress.IP, Hostname: ingress.Hostname})
	}

	return gatewayEndpoints, gateway.Spec.Ports[0].Port, nil
}

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

func (sw *RemoteClusterServiceWatcher) getMirroredServiceLabels(service *corev1.Service) map[string]string {
	newLabels := map[string]string{
		MirroredResourceLabel:      "true",
		RemoteClusterNameLabel:     sw.clusterName,
		RemoteResourceVersionLabel: service.ResourceVersion,
	}
	for k, v := range service.Labels {
		if k == GatewayNameAnnotation {
			k = RemoteGatewayNameAnnotation
		}
		if k == GatewayNsAnnottion {
			k = RemoteGatewayNsAnnottion
		}
		newLabels[k] = v
	}
	return newLabels
}

func (sw *RemoteClusterServiceWatcher) mirrorNamespaceIfNecessary(namespace string) error {
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

func (sw *RemoteClusterServiceWatcher) cleanupOrhpanesServices() error {
	sw.log.Debug("GC-ing orphaned services")
	return nil
}

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

func (sw *RemoteClusterServiceWatcher) handleRemoteServiceDeleted(ev *RemoteServiceDeleted) error {
	localServiceName := sw.mirroredResourceName(ev.Name)
	sw.log.Debugf("Deleting mirrored service %s/%s and its corresponding Endpoints", ev.Namespace, localServiceName)
	if err := sw.localApiClient.Client.CoreV1().Services(ev.Namespace).Delete(localServiceName, &metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("could not delete Service: %s/%s: %s", ev.Namespace, localServiceName, err)
	}
	sw.log.Debugf("Successfully deleted Service: %s/%s", ev.Namespace, localServiceName)
	return nil
}

func (sw *RemoteClusterServiceWatcher) handleRemoteServiceUpdated(ev *RemoteServiceUpdated) error {
	gatewayEndpoints, gatewayPort, err := sw.resolveGateway(ev.gatewayNs, ev.gatewayName)
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

	gatewayEndpoints, gatewayPort, err := sw.resolveGateway(ev.gatewayNs, ev.gatewayName)
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
		sw.localApiClient.Client.CoreV1().Services(ev.service.Namespace).Delete(localServiceName, &metav1.DeleteOptions{})
		return err
	}
	return nil
}

func (sw *RemoteClusterServiceWatcher) onUpdate(old, new interface{}) {
	oldService := old.(*corev1.Service)
	newService := new.(*corev1.Service)

	remoteGatewayName, hasGtwName := newService.Annotations[GatewayNameAnnotation]
	remoteGatewayNs, hasGtwNs := newService.Annotations[GatewayNsAnnottion]

	if oldService.ResourceVersion != newService.ResourceVersion {
		if hasGtwName && hasGtwNs {
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
							gatewayNs:      remoteGatewayNs,
							gatewayName:    remoteGatewayName,
						})
					}
				}
			}
		}
	}
}

func (sw *RemoteClusterServiceWatcher) onAdd(svc interface{}) {
	service := svc.(*corev1.Service)
	remoteGatewayName, hasGtwName := service.Annotations[GatewayNameAnnotation]
	remoteGatewayNs, hasGtwNs := service.Annotations[GatewayNsAnnottion]
	localName := sw.mirroredResourceName(service.Name)

	if hasGtwName && hasGtwNs {
		// a service that is a candiadte for being mirrored as it has the needed annotations
		localService, err := sw.localApiClient.Svc().Lister().Services(service.Namespace).Get(localName)
		if err != nil {
			sw.eventsQueue.AddRateLimited(&RemoteServiceCreated{
				service:            service,
				gatewayNs:          remoteGatewayNs,
				gatewayName:        remoteGatewayName,
				newResourceVersion: service.ResourceVersion,
			})
		} else {
			lastMirroredRemoteVersion, ok := localService.Labels[RemoteResourceVersionLabel]
			if ok && lastMirroredRemoteVersion != service.ResourceVersion {
				endpoints, err := sw.localApiClient.Endpoint().Lister().Endpoints(service.Namespace).Get(localName)
				if err == nil {
					sw.eventsQueue.AddRateLimited(&RemoteServiceUpdated{
						localService:   localService,
						localEndpoints: endpoints,
						remoteUpdate:   service,
						gatewayNs:      remoteGatewayNs,
						gatewayName:    remoteGatewayName,
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
	_, hasGtwNs := service.Annotations[GatewayNsAnnottion]
	if hasGtwName && hasGtwNs {
		sw.eventsQueue.AddRateLimited(&RemoteServiceDeleted{
			Name:      service.Name,
			Namespace: service.Namespace,
		})
	} else {
		//TODO: Handle gateways!!!
	}
}

func (sw *RemoteClusterServiceWatcher) processEvents() {
	maxRetries := 3 // magic...
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
			err = sw.cleanupOrhpanesServices()
		default:
			if ev != nil || !done { // we get a nil in case we are shutting down...
				sw.log.Warnf("Received unknown event: %v", ev)
			}
		}

		// handle errors
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

func (sw *RemoteClusterServiceWatcher) Stop() {
	close(sw.stopper)
	sw.eventsQueue.AddRateLimited(&ClusterUnregistered{})
	sw.eventsQueue.ShutDown()
}
