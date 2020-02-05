package servicemirror

import (
	"fmt"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/workqueue"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type RemoteClusterServiceWatcher struct {
	clusterName     string
	remoteApiClient *k8s.API
	localApiClient  *k8s.API
	stopper         chan struct{}
	log             *logging.Entry
	eventsQueue     workqueue.RateLimitingInterface
}

func (gw *RemoteClusterServiceWatcher) resolveGateway(namespace string, gatewayName string) (string, int32, error) {
	gateway, err := gw.remoteApiClient.Svc().Lister().Services(namespace).Get(gatewayName)
	if err != nil {
		return "", 0, err
	}

	if len(gateway.Spec.ExternalIPs) != 1 {
		return "", 0, fmt.Errorf("expected gateway to have 1 external Ip address but it has %d", len(gateway.Spec.ExternalIPs))
	}
	if len(gateway.Spec.Ports) != 1 {
		return "", 0, fmt.Errorf("expected gateway to have 1 port but it has %d", len(gateway.Spec.Ports))
	}

	return gateway.Spec.ExternalIPs[0], gateway.Spec.Ports[0].Port, nil
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

func (sw *RemoteClusterServiceWatcher) namespaceExists(ns string) bool {
	_, err := sw.localApiClient.NS().Lister().Get(ns)
	return err == nil
}

func (sw *RemoteClusterServiceWatcher) getServiceTemplate(nameSpace string, name string) *corev1.Service {
	localServiceName := sw.mirroredResourceName(name)
	service, err := sw.localApiClient.Svc().Lister().Services(nameSpace).Get(localServiceName)
	if err != nil {
		// in this case we do not have a service present so we need to create a new one
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        localServiceName,
				Namespace:   nameSpace,
				Annotations: map[string]string{},
			},
		}
	} else {
		return service
	}
}

func (sw *RemoteClusterServiceWatcher) getEndpointsTemplate(nameSpace string, name string) *corev1.Endpoints {
	localEndpointsName := sw.mirroredResourceName(name)
	endpoints, err := sw.localApiClient.Endpoint().Lister().Endpoints(nameSpace).Get(localEndpointsName)
	if err != nil {
		// we do not have Endpoints present so we need to create new ones
		return &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      localEndpointsName,
				Namespace: nameSpace,
				Labels: map[string]string{
					MirroredResourceLabel:  "true",
					RemoteClusterNameLabel: sw.clusterName,
				},
			},
		}
	} else {
		return endpoints
	}
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
	if !sw.namespaceExists(namespace) {
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

func (sw *RemoteClusterServiceWatcher) getRemappedPorts(service *corev1.Service, gatewayPort int32) []corev1.ServicePort {
	var remappedPorts []corev1.ServicePort
	for _, remotePort := range service.Spec.Ports {
		remappedPorts = append(remappedPorts, corev1.ServicePort{
			Name:       remotePort.Name,
			Protocol:   remotePort.Protocol,
			Port:       remotePort.Port,
			TargetPort: intstr.IntOrString{IntVal: gatewayPort},
			NodePort:   remotePort.NodePort,
		})
	}
	return remappedPorts
}

type RemoteServiceCreated struct {
	service            *corev1.Service
	gatewayNs          string
	gatewayName        string
	newResourceVersion string
}

type RemoteServiceUpdated struct {
	localService   *corev1.Service
	localEndpoints *corev1.Endpoints
	remoteUpdate   *corev1.Service
	gatewayNs      string
	gatewayName    string
}

type RemoteServiceDeleted struct {
	Name      string
	Namespace string
}

type RemoteGatewayDeleted struct {
	Name      string
	Namespace string
}

type RemoteGatewayUpdated struct {
	old *corev1.Service
	new *corev1.Service
}

type ClusterUnregistered struct{}

type OprhanedServicesGcTriggered struct{}


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

func (sw *RemoteClusterServiceWatcher) handleNewRemoteServiceUpdated(ev *RemoteServiceUpdated) error {
	gatewayOnLocalService := ev.localService.Labels[RemoteGatewayNameAnnotation]
	gatewayNsOnLocalService := ev.localService.Labels[RemoteGatewayNsAnnottion]

	gatewayIp, gatewayPort, err := sw.resolveGateway(ev.gatewayNs, ev.gatewayName)
	if err != nil {
		return err
	}
	if gatewayNsOnLocalService != ev.gatewayNs || gatewayOnLocalService != ev.gatewayName {
		// means we need to update the endpoints object
		ev.localEndpoints.Subsets = []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: gatewayIp,
					},
				},
				Ports: []corev1.EndpointPort{{
					Port: gatewayPort,
				}},
			},
		}
		if _, err := sw.localApiClient.Client.CoreV1().Endpoints(ev.localEndpoints.Namespace).Update(ev.localEndpoints); err != nil {
			return err
		}
	}

	ev.localService.Labels = sw.getMirroredServiceLabels(ev.remoteUpdate)
	ev.localService.Spec.Ports = sw.getRemappedPorts(ev.remoteUpdate, gatewayPort)

	if _, err := sw.localApiClient.Client.CoreV1().Services(ev.localService.Namespace).Update(ev.localService); err != nil {
		return err
	}
	return nil
}

func (sw *RemoteClusterServiceWatcher) handleNewRemoteServiceCreated(ev *RemoteServiceCreated) error {
	serviceInfo := fmt.Sprintf("%s/%s", ev.service.Namespace, ev.service.Name)
	sw.log.Debugf("Creating new service mirror for: %s", serviceInfo)
	localServiceName := sw.mirroredResourceName(ev.service.Name)

	gatewayIp, gatewayPort, err := sw.resolveGateway(ev.gatewayNs, ev.gatewayName)
	if err != nil {
		return err
	}
	sw.log.Debugf("Resolved remote gateway [%s:%d] for %s", gatewayIp, gatewayPort, serviceInfo)

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
			Ports: sw.getRemappedPorts(ev.service, gatewayPort),
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
				Addresses: []corev1.EndpointAddress{
					{
						IP: gatewayIp,
					},
				},
				Ports: []corev1.EndpointPort{{
					Port: gatewayPort,
				}},
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

func (sw *RemoteClusterServiceWatcher) handleUpdate(old, new interface{}) {
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

func (sw *RemoteClusterServiceWatcher) handleAdd(svc interface{}) {
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

func (sw *RemoteClusterServiceWatcher) handleDelete(svc interface{}) {
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
			err = sw.handleNewRemoteServiceCreated(ev)
		case *RemoteServiceUpdated:
			err = sw.handleNewRemoteServiceUpdated(ev)
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
			AddFunc:    sw.handleAdd,
			DeleteFunc: sw.handleDelete,
			UpdateFunc: sw.handleUpdate,
		})

	go sw.processEvents()
}

func (sw *RemoteClusterServiceWatcher) Stop() {
	close(sw.stopper)
	sw.eventsQueue.Add(&ClusterUnregistered{})
	sw.eventsQueue.ShutDown()
}
