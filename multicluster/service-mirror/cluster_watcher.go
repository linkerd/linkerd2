package servicemirror

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/multicluster"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
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
		serviceMirrorNamespace string
		link                   *multicluster.Link
		remoteAPIClient        *k8s.API
		localAPIClient         *k8s.API
		stopper                chan struct{}
		log                    *logging.Entry
		eventsQueue            workqueue.RateLimitingInterface
		requeueLimit           int
		repairPeriod           time.Duration
	}

	// RemoteServiceCreated is generated whenever a remote service is created Observing
	// this event means that the service in question is not mirrored atm
	RemoteServiceCreated struct {
		service *corev1.Service
	}

	// RemoteServiceUpdated is generated when we see something about an already
	// mirrored service change on the remote cluster. In that case we need to
	// reconcile. Most importantly we need to keep track of exposed ports
	// and gateway association changes.
	RemoteServiceUpdated struct {
		localService   *corev1.Service
		localEndpoints *corev1.Endpoints
		remoteUpdate   *corev1.Service
	}

	// RemoteServiceDeleted when a remote service is going away or it is not
	// considered mirrored anymore
	RemoteServiceDeleted struct {
		Name      string
		Namespace string
	}

	// ClusterUnregistered is issued when this ClusterWatcher is shut down.
	ClusterUnregistered struct{}

	// OrphanedServicesGcTriggered is a self-triggered event which aims to delete any
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
	OrphanedServicesGcTriggered struct{}

	// OnAddCalled is issued when the onAdd function of the
	// shared informer is called
	OnAddCalled struct {
		svc *corev1.Service
	}

	// OnUpdateCalled is issued when the onUpdate function of the
	// shared informer is called
	OnUpdateCalled struct {
		svc *corev1.Service
	}

	// OnDeleteCalled is issued when the onDelete function of the
	// shared informer is called
	OnDeleteCalled struct {
		svc *corev1.Service
	}

	// RepairEndpoints is issued when the service mirror and mirror gateway
	// endpoints should be resolved based on the remote gateway and updated.
	RepairEndpoints struct{}

	// RetryableError is an error that should be retried through requeuing events
	RetryableError struct{ Inner []error }
)

func (re RetryableError) Error() string {
	var errorStrings []string
	for _, err := range re.Inner {
		errorStrings = append(errorStrings, err.Error())
	}
	return fmt.Sprintf("Inner errors:\n\t%s", strings.Join(errorStrings, "\n\t"))
}

// NewRemoteClusterServiceWatcher constructs a new cluster watcher
func NewRemoteClusterServiceWatcher(
	ctx context.Context,
	serviceMirrorNamespace string,
	localAPI *k8s.API,
	cfg *rest.Config,
	link *multicluster.Link,
	requeueLimit int,
	repairPeriod time.Duration,

) (*RemoteClusterServiceWatcher, error) {
	remoteAPI, err := k8s.InitializeAPIForConfig(ctx, cfg, false, k8s.Svc)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize api for target cluster %s: %s", clusterName, err)
	}
	_, err = remoteAPI.Client.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to api for target cluster %s: %s", clusterName, err)
	}

	stopper := make(chan struct{})
	return &RemoteClusterServiceWatcher{
		serviceMirrorNamespace: serviceMirrorNamespace,
		link:                   link,
		remoteAPIClient:        remoteAPI,
		localAPIClient:         localAPI,
		stopper:                stopper,
		log: logging.WithFields(logging.Fields{
			"cluster":    clusterName,
			"apiAddress": cfg.Host,
		}),
		eventsQueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		requeueLimit: requeueLimit,
		repairPeriod: repairPeriod,
	}, nil
}

func (rcsw *RemoteClusterServiceWatcher) mirroredResourceName(remoteName string) string {
	return fmt.Sprintf("%s-%s", remoteName, rcsw.link.TargetClusterName)
}

func (rcsw *RemoteClusterServiceWatcher) originalResourceName(mirroredName string) string {
	return strings.TrimSuffix(mirroredName, fmt.Sprintf("-%s", rcsw.link.TargetClusterName))
}

func (rcsw *RemoteClusterServiceWatcher) getMirroredServiceLabels() map[string]string {
	return map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteClusterNameLabel: rcsw.link.TargetClusterName,
	}
}

func (rcsw *RemoteClusterServiceWatcher) getMirroredServiceAnnotations(remoteService *corev1.Service) map[string]string {
	annotations := map[string]string{
		consts.RemoteResourceVersionAnnotation: remoteService.ResourceVersion, // needed to detect real changes
		consts.RemoteServiceFqName:             fmt.Sprintf("%s.%s.svc.%s", remoteService.Name, remoteService.Namespace, rcsw.link.TargetClusterDomain),
	}
	value, ok := remoteService.GetAnnotations()[consts.ProxyOpaquePortsAnnotation]
	if ok {
		annotations[consts.ProxyOpaquePortsAnnotation] = value
	}
	return annotations
}

func (rcsw *RemoteClusterServiceWatcher) mirrorNamespaceIfNecessary(ctx context.Context, namespace string) error {
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
						consts.RemoteClusterNameLabel: rcsw.link.TargetClusterName,
					},
					Name: namespace,
				},
			}
			_, err := rcsw.localAPIClient.Client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
			if err != nil {
				// something went wrong with the create, we can just retry as well
				return RetryableError{[]error{err}}
			}
		} else {
			// something else went wrong, so we can just retry
			return RetryableError{[]error{err}}
		}
	}
	return nil
}

// This method takes care of port remapping. What it does essentially is get the one gateway port
// that we should send traffic to and create endpoint ports that bind to the mirrored service ports
// (same name, etc) but send traffic to the gateway port. This way we do not need to do any remapping
// on the service side of things. It all happens in the endpoints.
func (rcsw *RemoteClusterServiceWatcher) getEndpointsPorts(service *corev1.Service) []corev1.EndpointPort {
	var endpointsPorts []corev1.EndpointPort
	for _, remotePort := range service.Spec.Ports {
		endpointsPorts = append(endpointsPorts, corev1.EndpointPort{
			Name:     remotePort.Name,
			Protocol: remotePort.Protocol,
			Port:     int32(rcsw.link.GatewayPort),
		})
	}
	return endpointsPorts
}

func (rcsw *RemoteClusterServiceWatcher) cleanupOrphanedServices(ctx context.Context) error {
	matchLabels := map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteClusterNameLabel: rcsw.link.TargetClusterName,
	}

	servicesOnLocalCluster, err := rcsw.localAPIClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		innerErr := fmt.Errorf("failed to list services while cleaning up mirror services: %s", err)
		if kerrors.IsNotFound(err) {
			return innerErr
		}
		// if it is something else, we can just retry
		return RetryableError{[]error{innerErr}}
	}

	var errors []error
	for _, srv := range servicesOnLocalCluster {
		_, err := rcsw.remoteAPIClient.Svc().Lister().Services(srv.Namespace).Get(rcsw.originalResourceName(srv.Name))
		if err != nil {
			if kerrors.IsNotFound(err) {
				// service does not exist anymore. Need to delete
				if err := rcsw.localAPIClient.Client.CoreV1().Services(srv.Namespace).Delete(ctx, srv.Name, metav1.DeleteOptions{}); err != nil {
					// something went wrong with deletion, we need to retry
					errors = append(errors, err)
				} else {
					rcsw.log.Infof("Deleted service %s/%s while cleaning up mirror services", srv.Namespace, srv.Name)
				}
			} else {
				// something went wrong getting the service, we can retry
				errors = append(errors, err)
			}
		}
	}
	if len(errors) > 0 {
		return RetryableError{errors}
	}

	return nil
}

// Whenever we stop watching a cluster, we need to cleanup everything that we have
// created. This piece of code is responsible for doing just that. It takes care of
// services, endpoints and namespaces (if needed)
func (rcsw *RemoteClusterServiceWatcher) cleanupMirroredResources(ctx context.Context) error {
	matchLabels := rcsw.getMirroredServiceLabels()

	services, err := rcsw.localAPIClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		innerErr := fmt.Errorf("could not retrieve mirrored services that need cleaning up: %s", err)
		if kerrors.IsNotFound(err) {
			return innerErr
		}
		// if its not notFound then something else went wrong, so we can retry
		return RetryableError{[]error{innerErr}}
	}

	var errors []error
	for _, svc := range services {
		if err := rcsw.localAPIClient.Client.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
			if kerrors.IsNotFound(err) {
				continue
			}
			errors = append(errors, fmt.Errorf("Could not delete  service %s/%s: %s", svc.Namespace, svc.Name, err))
		} else {
			rcsw.log.Infof("Deleted service %s/%s", svc.Namespace, svc.Name)
		}
	}

	endpoints, err := rcsw.localAPIClient.Endpoint().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		innerErr := fmt.Errorf("could not retrieve Endpoints that need cleaning up: %s", err)
		if kerrors.IsNotFound(err) {
			return innerErr
		}
		return RetryableError{[]error{innerErr}}
	}

	for _, endpoint := range endpoints {
		if err := rcsw.localAPIClient.Client.CoreV1().Endpoints(endpoint.Namespace).Delete(ctx, endpoint.Name, metav1.DeleteOptions{}); err != nil {
			if kerrors.IsNotFound(err) {
				continue
			}
			errors = append(errors, fmt.Errorf("Could not delete  Endpoints %s/%s: %s", endpoint.Namespace, endpoint.Name, err))
		} else {
			rcsw.log.Infof("Deleted Endpoints %s/%s", endpoint.Namespace, endpoint.Name)
		}
	}

	if len(errors) > 0 {
		return RetryableError{errors}
	}
	return nil
}

// Deletes a locally mirrored service as it is not present on the remote cluster anymore
func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceDeleted(ctx context.Context, ev *RemoteServiceDeleted) error {
	localServiceName := rcsw.mirroredResourceName(ev.Name)
	rcsw.log.Infof("Deleting mirrored service %s/%s", ev.Namespace, localServiceName)
	var errors []error
	if err := rcsw.localAPIClient.Client.CoreV1().Services(ev.Namespace).Delete(ctx, localServiceName, metav1.DeleteOptions{}); err != nil {
		if !kerrors.IsNotFound(err) {
			errors = append(errors, fmt.Errorf("could not delete Service: %s/%s: %s", ev.Namespace, localServiceName, err))
		}
	}

	if len(errors) > 0 {
		return RetryableError{errors}
	}

	rcsw.log.Infof("Successfully deleted Service: %s/%s", ev.Namespace, localServiceName)
	return nil
}

// Updates a locally mirrored service. There might have been some pretty fundamental changes such as
// new gateway being assigned or additional ports exposed. This method takes care of that.
func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceUpdated(ctx context.Context, ev *RemoteServiceUpdated) error {
	rcsw.log.Infof("Updating mirror service %s/%s", ev.localService.Namespace, ev.localService.Name)
	gatewayAddresses, err := rcsw.resolveGatewayAddress()
	if err != nil {
		return err
	}

	copiedEndpoints := ev.localEndpoints.DeepCopy()
	copiedEndpoints.Subsets = []corev1.EndpointSubset{
		{
			Addresses: gatewayAddresses,
			Ports:     rcsw.getEndpointsPorts(ev.remoteUpdate),
		},
	}

	if copiedEndpoints.Annotations == nil {
		copiedEndpoints.Annotations = make(map[string]string)
	}
	copiedEndpoints.Annotations[consts.RemoteGatewayIdentity] = rcsw.link.GatewayIdentity

	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(copiedEndpoints.Namespace).Update(ctx, copiedEndpoints, metav1.UpdateOptions{}); err != nil {
		return RetryableError{[]error{err}}
	}

	ev.localService.Labels = rcsw.getMirroredServiceLabels()
	ev.localService.Annotations = rcsw.getMirroredServiceAnnotations(ev.remoteUpdate)
	ev.localService.Spec.Ports = remapRemoteServicePorts(ev.remoteUpdate.Spec.Ports)

	if _, err := rcsw.localAPIClient.Client.CoreV1().Services(ev.localService.Namespace).Update(ctx, ev.localService, metav1.UpdateOptions{}); err != nil {
		return RetryableError{[]error{err}}
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

func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceCreated(ctx context.Context, ev *RemoteServiceCreated) error {
	gatewayAddresses, err := rcsw.resolveGatewayAddress()
	if err != nil {
		return err
	}

	remoteService := ev.service.DeepCopy()
	serviceInfo := fmt.Sprintf("%s/%s", remoteService.Namespace, remoteService.Name)
	localServiceName := rcsw.mirroredResourceName(remoteService.Name)

	if err := rcsw.mirrorNamespaceIfNecessary(ctx, remoteService.Namespace); err != nil {
		return err
	}

	serviceToCreate := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        localServiceName,
			Namespace:   remoteService.Namespace,
			Annotations: rcsw.getMirroredServiceAnnotations(remoteService),
			Labels:      rcsw.getMirroredServiceLabels(),
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
				consts.RemoteClusterNameLabel: rcsw.link.TargetClusterName,
			},
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.%s", remoteService.Name, remoteService.Namespace, rcsw.link.TargetClusterDomain),
			},
		},
	}

	// only if we resolve it, we are updating the endpoints addresses and ports
	rcsw.log.Infof("Resolved gateway [%v:%d] for %s", gatewayAddresses, rcsw.link.GatewayPort, serviceInfo)

	if len(gatewayAddresses) > 0 {
		endpointsToCreate.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayAddresses,
				Ports:     rcsw.getEndpointsPorts(ev.service),
			},
		}
	} else {
		rcsw.log.Warnf("gateway for %s does not have ready addresses, skipping subsets", serviceInfo)
	}
	if rcsw.link.GatewayIdentity != "" {
		endpointsToCreate.Annotations[consts.RemoteGatewayIdentity] = rcsw.link.GatewayIdentity
	}

	rcsw.log.Infof("Creating a new service mirror for %s", serviceInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Services(remoteService.Namespace).Create(ctx, serviceToCreate, metav1.CreateOptions{}); err != nil {
		if !kerrors.IsAlreadyExists(err) {
			// we might have created it during earlier attempt, if that is not the case, we retry
			return RetryableError{[]error{err}}
		}
	}

	rcsw.log.Infof("Creating a new Endpoints for %s", serviceInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(ev.service.Namespace).Create(ctx, endpointsToCreate, metav1.CreateOptions{}); err != nil {
		// we clean up after ourselves
		rcsw.localAPIClient.Client.CoreV1().Services(ev.service.Namespace).Delete(ctx, localServiceName, metav1.DeleteOptions{})
		// and retry
		return RetryableError{[]error{err}}
	}
	return nil
}

func (rcsw *RemoteClusterServiceWatcher) isExportedService(service *corev1.Service) bool {
	selector, err := metav1.LabelSelectorAsSelector(&rcsw.link.Selector)
	if err != nil {
		rcsw.log.Errorf("Invalid service selector: %s", err)
		return false
	}
	return selector.Matches(labels.Set(service.Labels))
}

// this method is common to both CREATE and UPDATE because if we have been
// offline for some time due to a crash a CREATE for a service that we have
// observed before is simply a case of UPDATE
func (rcsw *RemoteClusterServiceWatcher) createOrUpdateService(service *corev1.Service) error {
	localName := rcsw.mirroredResourceName(service.Name)

	if rcsw.isExportedService(service) {
		localService, err := rcsw.localAPIClient.Svc().Lister().Services(service.Namespace).Get(localName)
		if err != nil {
			if kerrors.IsNotFound(err) {
				rcsw.eventsQueue.Add(&RemoteServiceCreated{
					service: service,
				})
				return nil
			}
			return RetryableError{[]error{err}}
		}
		// if we have the local service present, we need to issue an update
		lastMirroredRemoteVersion, ok := localService.Annotations[consts.RemoteResourceVersionAnnotation]
		if ok && lastMirroredRemoteVersion != service.ResourceVersion {
			endpoints, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(service.Namespace).Get(localName)
			if err == nil {
				rcsw.eventsQueue.Add(&RemoteServiceUpdated{
					localService:   localService,
					localEndpoints: endpoints,
					remoteUpdate:   service,
				})
				return nil
			}
			return RetryableError{[]error{err}}
		}
		return nil
	}
	localSvc, err := rcsw.localAPIClient.Svc().Lister().Services(service.Namespace).Get(localName)
	if err == nil {
		if localSvc.Labels != nil {
			_, isMirroredRes := localSvc.Labels[consts.MirroredResourceLabel]
			clusterName := localSvc.Labels[consts.RemoteClusterNameLabel]
			if isMirroredRes && (clusterName == rcsw.link.TargetClusterName) {
				rcsw.eventsQueue.Add(&RemoteServiceDeleted{
					Name:      service.Name,
					Namespace: service.Namespace,
				})
			}
		}
	}
	return nil
}

func (rcsw *RemoteClusterServiceWatcher) getMirrorServices() ([]*corev1.Service, error) {
	matchLabels := map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteClusterNameLabel: rcsw.link.TargetClusterName,
	}

	services, err := rcsw.localAPIClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		return nil, err
	}
	return services, nil
}

func (rcsw *RemoteClusterServiceWatcher) handleOnDelete(service *corev1.Service) {
	if rcsw.isExportedService(service) {
		rcsw.eventsQueue.Add(&RemoteServiceDeleted{
			Name:      service.Name,
			Namespace: service.Namespace,
		})
	} else {
		rcsw.log.Infof("Skipping OnDelete for service %s", service)
	}
}

func (rcsw *RemoteClusterServiceWatcher) processNextEvent(ctx context.Context) (bool, interface{}, error) {
	event, done := rcsw.eventsQueue.Get()
	if event != nil {
		rcsw.log.Infof("Received: %s", event)
	} else {
		if done {
			rcsw.log.Infof("Received: Stop")
		}
	}

	var err error
	switch ev := event.(type) {
	case *OnAddCalled:
		err = rcsw.createOrUpdateService(ev.svc)
	case *OnUpdateCalled:
		err = rcsw.createOrUpdateService(ev.svc)
	case *OnDeleteCalled:
		rcsw.handleOnDelete(ev.svc)
	case *RemoteServiceCreated:
		err = rcsw.handleRemoteServiceCreated(ctx, ev)
	case *RemoteServiceUpdated:
		err = rcsw.handleRemoteServiceUpdated(ctx, ev)
	case *RemoteServiceDeleted:
		err = rcsw.handleRemoteServiceDeleted(ctx, ev)
	case *ClusterUnregistered:
		err = rcsw.cleanupMirroredResources(ctx)
	case *OrphanedServicesGcTriggered:
		err = rcsw.cleanupOrphanedServices(ctx)
	case *RepairEndpoints:
		err = rcsw.repairEndpoints(ctx)
	default:
		if ev != nil || !done { // we get a nil in case we are shutting down...
			rcsw.log.Warnf("Received unknown event: %v", ev)
		}
	}

	return done, event, err

}

// the main processing loop in which we handle more domain specific events
// and deal with retries
func (rcsw *RemoteClusterServiceWatcher) processEvents(ctx context.Context) {
	for {
		done, event, err := rcsw.processNextEvent(ctx)
		rcsw.eventsQueue.Done(event)
		// the logic here is that there might have been an API
		// connectivity glitch or something. So its not a bad idea to requeue
		// the event and try again up to a number of limits, just to ensure
		// that we are not diverging in states due to bad luck...
		if err == nil {
			rcsw.eventsQueue.Forget(event)
		} else {
			switch e := err.(type) {
			case RetryableError:
				{
					rcsw.log.Warnf("Requeues: %d, Limit: %d for event %s", rcsw.eventsQueue.NumRequeues(event), rcsw.requeueLimit, event)
					if (rcsw.eventsQueue.NumRequeues(event) < rcsw.requeueLimit) && !done {
						rcsw.log.Errorf("Error processing %s (will retry): %s", event, e)
						rcsw.eventsQueue.AddRateLimited(event)
					} else {
						rcsw.log.Errorf("Error processing %s (giving up): %s", event, e)
						rcsw.eventsQueue.Forget(event)
					}
				}
			default:
				rcsw.log.Errorf("Error processing %s (will not retry): %s", event, e)
				rcsw.log.Error(e)
			}
		}
		if done {
			rcsw.log.Infof("Shutting down events processor")
			return
		}
	}
}

// Start starts watching the remote cluster
func (rcsw *RemoteClusterServiceWatcher) Start(ctx context.Context) error {
	rcsw.remoteAPIClient.Sync(rcsw.stopper)
	rcsw.eventsQueue.Add(&OrphanedServicesGcTriggered{})
	rcsw.remoteAPIClient.Svc().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(svc interface{}) {
				rcsw.eventsQueue.Add(&OnAddCalled{svc.(*corev1.Service)})
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
				rcsw.eventsQueue.Add(&OnDeleteCalled{service})
			},
			UpdateFunc: func(old, new interface{}) {
				rcsw.eventsQueue.Add(&OnUpdateCalled{new.(*corev1.Service)})
			},
		},
	)
	go rcsw.processEvents(ctx)

	// We need to issue a RepairEndpoints immediately to populate the gateway
	// mirror endpoints.
	ev := RepairEndpoints{}
	rcsw.eventsQueue.Add(&ev)

	go func() {
		ticker := time.NewTicker(rcsw.repairPeriod)
		for {
			select {
			case <-ticker.C:
				ev := RepairEndpoints{}
				rcsw.eventsQueue.Add(&ev)
			case <-rcsw.stopper:
				return
			}
		}
	}()

	return nil
}

// Stop stops watching the cluster and cleans up all mirrored resources
func (rcsw *RemoteClusterServiceWatcher) Stop(cleanupState bool) {
	close(rcsw.stopper)
	if cleanupState {
		rcsw.eventsQueue.Add(&ClusterUnregistered{})
	}
	rcsw.eventsQueue.ShutDown()
}

func (rcsw *RemoteClusterServiceWatcher) resolveGatewayAddress() ([]corev1.EndpointAddress, error) {
	var gatewayEndpoints []corev1.EndpointAddress
	var errors []error
	for _, addr := range strings.Split(rcsw.link.GatewayAddress, ",") {
		ipAddr, err := net.ResolveIPAddr("ip", addr)
		if err == nil {
			gatewayEndpoints = append(gatewayEndpoints, corev1.EndpointAddress{
				IP: ipAddr.String(),
			})
		} else {
			err = fmt.Errorf("Error resolving '%s': %s", addr, err)
			rcsw.log.Warn(err)
			errors = append(errors, err)
		}
	}
	// one resolved address is enough
	if len(gatewayEndpoints) > 0 {
		return gatewayEndpoints, nil
	}
	return nil, RetryableError{errors}
}

func (rcsw *RemoteClusterServiceWatcher) repairEndpoints(ctx context.Context) error {
	gatewayAddresses, err := rcsw.resolveGatewayAddress()
	if err != nil {
		return err
	}

	endpointRepairCounter.With(prometheus.Labels{
		gatewayClusterName: rcsw.link.TargetClusterName,
	}).Inc()

	// Create or update gateway mirror endpoints.
	gatewayMirrorName := fmt.Sprintf("probe-gateway-%s", rcsw.link.TargetClusterName)

	gatewayMirrorEndpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayMirrorName,
			Namespace: rcsw.serviceMirrorNamespace,
			Labels: map[string]string{
				consts.RemoteClusterNameLabel: rcsw.link.TargetClusterName,
			},
			Annotations: map[string]string{
				consts.RemoteGatewayIdentity: rcsw.link.GatewayIdentity,
			},
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: gatewayAddresses,
				Ports: []corev1.EndpointPort{
					{
						Name:     "mc-probe",
						Port:     int32(rcsw.link.ProbeSpec.Port),
						Protocol: "TCP",
					},
				},
			},
		},
	}

	err = rcsw.createOrUpdateEndpoints(ctx, gatewayMirrorEndpoints)
	if err != nil {
		rcsw.log.Errorf("Failed to create/update gateway mirror endpoints: %s", err)
	}

	// Repair mirror service endpoints.
	mirrorServices, err := rcsw.getMirrorServices()
	if err != nil {
		rcsw.log.Errorf("Failed to list mirror services: %s", err)
	}
	for _, svc := range mirrorServices {
		updatedService := svc.DeepCopy()

		endpoints, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(svc.Namespace).Get(svc.Name)
		if err != nil {
			rcsw.log.Errorf("Could not get endpoints: %s", err)
			continue
		}

		updatedEndpoints := endpoints.DeepCopy()
		updatedEndpoints.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayAddresses,
				Ports:     rcsw.getEndpointsPorts(updatedService),
			},
		}

		if updatedEndpoints.Annotations == nil {
			updatedEndpoints.Annotations = make(map[string]string)
		}
		updatedEndpoints.Annotations[consts.RemoteGatewayIdentity] = rcsw.link.GatewayIdentity

		_, err = rcsw.localAPIClient.Client.CoreV1().Services(updatedService.Namespace).Update(ctx, updatedService, metav1.UpdateOptions{})
		if err != nil {
			rcsw.log.Error(err)
			continue
		}

		_, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(updatedService.Namespace).Update(ctx, updatedEndpoints, metav1.UpdateOptions{})
		if err != nil {
			rcsw.log.Error(err)
		}
	}

	return nil
}

func (rcsw *RemoteClusterServiceWatcher) createOrUpdateEndpoints(ctx context.Context, ep *corev1.Endpoints) error {
	_, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(ep.Namespace).Get(ctx, ep.Name, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			// Does not exist so we should create it.
			_, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(ep.Namespace).Create(ctx, ep, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	// Exists so we should update it.
	_, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(ep.Namespace).Update(ctx, ep, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}
