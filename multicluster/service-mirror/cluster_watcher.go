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
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

const eventTypeSkipped = "ServiceMirroringSkipped"

type (
	// RemoteClusterServiceWatcher is a watcher instantiated for every cluster that is being watched
	// Its main job is to listen to events coming from the remote cluster and react accordingly, keeping
	// the state of the mirrored services in sync. This is achieved by maintaining a SharedInformer
	// on the remote cluster. The basic add/update/delete operations are mapped to a more domain specific
	// events, put onto a work queue and handled by the processing loop. In case processing an event fails
	// it can be requeued up to N times, to ensure that the failure is not due to some temporary network
	// problems or general glitch in the Matrix.
	RemoteClusterServiceWatcher struct {
		serviceMirrorNamespace  string
		link                    *multicluster.Link
		remoteAPIClient         *k8s.API
		localAPIClient          *k8s.API
		stopper                 chan struct{}
		recorder                record.EventRecorder
		log                     *logging.Entry
		eventsQueue             workqueue.RateLimitingInterface
		requeueLimit            int
		repairPeriod            time.Duration
		headlessServicesEnabled bool
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

	// OnAddEndpointsCalled is issued when the onAdd function of the Endpoints
	// shared informer is called
	OnAddEndpointsCalled struct {
		ep *corev1.Endpoints
	}

	// OnUpdateCalled is issued when the onUpdate function of the
	// shared informer is called
	OnUpdateCalled struct {
		svc *corev1.Service
	}

	// OnUpdateEndpointsCalled is issued when the onUpdate function of the
	// shared Endpoints informer is called
	OnUpdateEndpointsCalled struct {
		ep *corev1.Endpoints
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
	enableHeadlessSvc bool,
) (*RemoteClusterServiceWatcher, error) {
	remoteAPI, err := k8s.InitializeAPIForConfig(ctx, cfg, false, k8s.Svc, k8s.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize api for target cluster %s: %s", clusterName, err)
	}
	_, err = remoteAPI.Client.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to api for target cluster %s: %s", clusterName, err)
	}

	// Create k8s event recorder
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: remoteAPI.Client.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{
		Component: fmt.Sprintf("linkerd-service-mirror-%s", clusterName),
	})

	stopper := make(chan struct{})
	return &RemoteClusterServiceWatcher{
		serviceMirrorNamespace: serviceMirrorNamespace,
		link:                   link,
		remoteAPIClient:        remoteAPI,
		localAPIClient:         localAPI,
		stopper:                stopper,
		recorder:               recorder,
		log: logging.WithFields(logging.Fields{
			"cluster":    clusterName,
			"apiAddress": cfg.Host,
		}),
		eventsQueue:             workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		requeueLimit:            requeueLimit,
		repairPeriod:            repairPeriod,
		headlessServicesEnabled: enableHeadlessSvc,
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
			errors = append(errors, fmt.Errorf("Could not delete service %s/%s: %s", svc.Namespace, svc.Name, err))
		} else {
			rcsw.log.Infof("Deleted service %s/%s", svc.Namespace, svc.Name)
		}
	}

	endpoints, err := rcsw.localAPIClient.Endpoint().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		innerErr := fmt.Errorf("could not retrieve endpoints that need cleaning up: %s", err)
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
			errors = append(errors, fmt.Errorf("Could not delete endpoints %s/%s: %s", endpoint.Namespace, endpoint.Name, err))
		} else {
			rcsw.log.Infof("Deleted endpoints %s/%s", endpoint.Namespace, endpoint.Name)
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
	localService, err := rcsw.localAPIClient.Svc().Lister().Services(ev.Namespace).Get(localServiceName)
	var errors []error
	if err != nil {
		if kerrors.IsNotFound(err) {
			rcsw.log.Debugf("Failed to delete mirror service %s/%s: %v", ev.Namespace, ev.Name, err)
			return nil
		}
		errors = append(errors, fmt.Errorf("could not fetch service %s/%s: %s", ev.Namespace, localServiceName, err))
	}

	// If the mirror service is headless, also delete its endpoint mirror
	// services.
	if localService.Spec.ClusterIP == corev1.ClusterIPNone {
		matchLabels := map[string]string{
			consts.MirroredHeadlessSvcNameLabel: localServiceName,
		}
		endpointMirrorServices, err := rcsw.localAPIClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
		if err != nil {
			if !kerrors.IsNotFound(err) {
				errors = append(errors, fmt.Errorf("could not fetch endpoint mirrors for mirror service %s/%s: %s", ev.Namespace, localServiceName, err))
			}
		}

		for _, endpointMirror := range endpointMirrorServices {
			err = rcsw.localAPIClient.Client.CoreV1().Services(endpointMirror.Namespace).Delete(ctx, endpointMirror.Name, metav1.DeleteOptions{})
			if err != nil {
				if !kerrors.IsNotFound(err) {
					errors = append(errors, fmt.Errorf("could not delete endpoint mirror %s/%s: %s", endpointMirror.Namespace, endpointMirror.Name, err))
				}
			}
		}
	}

	rcsw.log.Infof("Deleting mirrored service %s/%s", ev.Namespace, localServiceName)
	if err := rcsw.localAPIClient.Client.CoreV1().Services(ev.Namespace).Delete(ctx, localServiceName, metav1.DeleteOptions{}); err != nil {
		if !kerrors.IsNotFound(err) {
			errors = append(errors, fmt.Errorf("could not delete service: %s/%s: %s", ev.Namespace, localServiceName, err))
		}
	}

	if len(errors) > 0 {
		return RetryableError{errors}
	}

	rcsw.log.Infof("Successfully deleted service: %s/%s", ev.Namespace, localServiceName)
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

	if rcsw.headlessServicesEnabled && remoteService.Spec.ClusterIP == corev1.ClusterIPNone {
		return nil
	}

	rcsw.log.Infof("Creating a new service mirror for %s", serviceInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Services(remoteService.Namespace).Create(ctx, serviceToCreate, metav1.CreateOptions{}); err != nil {
		if !kerrors.IsAlreadyExists(err) {
			// we might have created it during earlier attempt, if that is not the case, we retry
			return RetryableError{[]error{err}}
		}
	}

	return rcsw.createGatewayEndpoints(ctx, remoteService)
}

func (rcsw *RemoteClusterServiceWatcher) createGatewayEndpoints(ctx context.Context, exportedService *corev1.Service) error {
	gatewayAddresses, err := rcsw.resolveGatewayAddress()
	if err != nil {
		return err
	}

	localServiceName := rcsw.mirroredResourceName(exportedService.Name)
	serviceInfo := fmt.Sprintf("%s/%s", exportedService.Namespace, exportedService.Name)
	endpointsToCreate := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      localServiceName,
			Namespace: exportedService.Namespace,
			Labels: map[string]string{
				consts.MirroredResourceLabel:  "true",
				consts.RemoteClusterNameLabel: rcsw.link.TargetClusterName,
			},
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.%s", exportedService.Name, exportedService.Namespace, rcsw.link.TargetClusterDomain),
			},
		},
	}

	rcsw.log.Infof("Resolved gateway [%v:%d] for %s", gatewayAddresses, rcsw.link.GatewayPort, serviceInfo)

	if len(gatewayAddresses) > 0 {
		endpointsToCreate.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayAddresses,
				Ports:     rcsw.getEndpointsPorts(exportedService),
			},
		}
	} else {
		rcsw.log.Warnf("gateway for %s does not have ready addresses, skipping subsets", serviceInfo)
	}

	if rcsw.link.GatewayIdentity != "" {
		endpointsToCreate.Annotations[consts.RemoteGatewayIdentity] = rcsw.link.GatewayIdentity
	}

	rcsw.log.Infof("Creating a new endpoints for %s", serviceInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(exportedService.Namespace).Create(ctx, endpointsToCreate, metav1.CreateOptions{}); err != nil {
		// we clean up after ourselves
		rcsw.localAPIClient.Client.CoreV1().Services(exportedService.Namespace).Delete(ctx, localServiceName, metav1.DeleteOptions{})
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
	case *OnAddEndpointsCalled:
		err = rcsw.createOrUpdateHeadlessEndpoints(ctx, ev.ep)
	case *OnUpdateCalled:
		err = rcsw.createOrUpdateService(ev.svc)
	case *OnUpdateEndpointsCalled:
		err = rcsw.createOrUpdateHeadlessEndpoints(ctx, ev.ep)
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
	if rcsw.headlessServicesEnabled {
		rcsw.remoteAPIClient.Endpoint().Informer().AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					if obj.(metav1.Object).GetNamespace() == "kube-system" {
						return
					}

					if ok := isExportedHeadlessEndpoints(obj, rcsw.log); !ok {
						return
					}

					rcsw.eventsQueue.Add(&OnAddEndpointsCalled{obj.(*corev1.Endpoints)})
				},
				UpdateFunc: func(old, new interface{}) {
					if new.(metav1.Object).GetNamespace() == "kube-system" {
						return
					}

					if ok := isExportedHeadlessEndpoints(new, rcsw.log); !ok {
						return
					}

					rcsw.eventsQueue.Add(&OnUpdateEndpointsCalled{new.(*corev1.Endpoints)})
				},
			},
		)
	}
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

		// If the Service is headless we should skip repairing its Endpoints.
		// Headless Services that are mirrored on a remote cluster will have
		// their Endpoints created with hostnames and nested clusterIP services,
		// we should avoid replacing these with the gateway address.
		if svc.Spec.ClusterIP == corev1.ClusterIPNone {
			rcsw.log.Debugf("Skipped repairing endpoints for %s/%s", svc.Namespace, svc.Name)
			continue
		}
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

// createOrUpdateHeadlessEndpoints processes endpoints objects for exported
// headless services. When an endpoints object is created or updated in the
// remote cluster, it will be processed here in order to reconcile the local
// cluster state with the remote cluster state.
//
// createOrUpdateHeadlessEndpoints is also responsible for creating the service
// mirror in the source cluster. In order for an exported headless service to be
// mirrored as headless, it must have at least one port defined and at least one
// named address in its endpoints object (e.g a deployment would not work since
// pods may not have arbitrary hostnames). As such, when an endpoints object is
// first processed, if there is no mirror service, we create one, by looking at
// the endpoints object itself. If the exported service is deemed to be valid
// for headless mirroring, then the function will create the headless mirror and
// then create an endpoints object for it in the source cluster. If it is not
// valid, the exported service will be mirrored as clusterIP and its endpoints
// will point to the gateway.
//
// When creating endpoints for a headless mirror, we also create an endpoint
// mirror (clusterIP) service for each of the endpoints' named addresses. If the
// headless mirror exists and has an endpoints object, we simply update by
// either creating or deleting endpoint mirror services.
func (rcsw *RemoteClusterServiceWatcher) createOrUpdateHeadlessEndpoints(ctx context.Context, exportedEndpoints *corev1.Endpoints) error {
	exportedService, err := rcsw.remoteAPIClient.Svc().Lister().Services(exportedEndpoints.Namespace).Get(exportedEndpoints.Name)
	if err != nil {
		rcsw.log.Debugf("failed to retrieve exported service %s/%s when updating its headless mirror endpoints: %v", exportedEndpoints.Namespace, exportedEndpoints.Name, err)
		return fmt.Errorf("error retrieving exported service %s/%s: %v", exportedEndpoints.Namespace, exportedEndpoints.Name, err)
	}

	// Check whether the endpoints should be processed for a headless exported
	// service. If the exported service does not have any ports exposed, then
	// neither will its corresponding endpoint mirrors, it should not be created
	// as a headless mirror. If the service does not have any named addresses in
	// its Endpoints object, then the endpoints should not be processed.
	if len(exportedService.Spec.Ports) == 0 {
		rcsw.recorder.Event(exportedService, v1.EventTypeNormal, eventTypeSkipped, "Skipped mirroring service: object spec has no exposed ports")
		rcsw.log.Infof("Skipped creating headless mirror for %s/%s: service object spec has no exposed ports", exportedService.Namespace, exportedService.Name)
		return nil
	}

	mirrorServiceName := rcsw.mirroredResourceName(exportedService.Name)
	mirrorService, err := rcsw.localAPIClient.Svc().Lister().Services(exportedService.Namespace).Get(mirrorServiceName)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		// If the mirror service does not exist, create it, either as clusterIP
		// or as headless.
		mirrorService, err = rcsw.createRemoteHeadlessService(ctx, exportedService, exportedEndpoints)
		if err != nil {
			return err
		}
	}

	headlessMirrorEpName := rcsw.mirroredResourceName(exportedEndpoints.Name)
	headlessMirrorEndpoints, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(exportedEndpoints.Namespace).Get(headlessMirrorEpName)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		if mirrorService.Spec.ClusterIP != corev1.ClusterIPNone {
			return rcsw.createGatewayEndpoints(ctx, exportedService)
		}

		// Create endpoint mirrors for headless mirror
		if err := rcsw.createHeadlessMirrorEndpoints(ctx, exportedService, exportedEndpoints); err != nil {
			rcsw.log.Debugf("failed to create headless mirrors for endpoints %s/%s: %v", exportedEndpoints.Namespace, exportedEndpoints.Name, err)
			return err
		}

		return nil
	}

	// Past this point, we do not want to process a mirror service that is not
	// headless. We want to process only endpoints for headless mirrors; before
	// this point it would have been possible to have a clusterIP mirror, since
	// we are creating the mirror service in the scope of the function.
	if mirrorService.Spec.ClusterIP != corev1.ClusterIPNone {
		return nil
	}

	mirrorEndpoints := headlessMirrorEndpoints.DeepCopy()
	endpointMirrors := make(map[string]struct{})
	newSubsets := make([]corev1.EndpointSubset, 0, len(exportedEndpoints.Subsets))
	for _, subset := range exportedEndpoints.Subsets {
		newAddresses := make([]corev1.EndpointAddress, 0, len(subset.Addresses))
		for _, address := range subset.Addresses {
			if address.Hostname == "" {
				continue
			}

			endpointMirrorName := rcsw.mirroredResourceName(address.Hostname)
			endpointMirrorService, err := rcsw.localAPIClient.Svc().Lister().Services(exportedEndpoints.Namespace).Get(endpointMirrorName)
			if err != nil {
				if !kerrors.IsNotFound(err) {
					return err
				}
				// If the error is 'NotFound' then the Endpoint Mirror service
				// does not exist, so create it.
				endpointMirrorService, err = rcsw.createEndpointMirrorService(ctx, address.Hostname, exportedEndpoints.ResourceVersion, endpointMirrorName, exportedService)
				if err != nil {
					return err
				}
			}

			endpointMirrors[endpointMirrorName] = struct{}{}
			newAddresses = append(newAddresses, corev1.EndpointAddress{
				Hostname: address.Hostname,
				IP:       endpointMirrorService.Spec.ClusterIP,
			})
		}

		if len(newAddresses) == 0 {
			continue
		}

		// copy ports, create subset
		newSubsets = append(newSubsets, corev1.EndpointSubset{
			Addresses: newAddresses,
			Ports:     subset.DeepCopy().Ports,
		})
	}

	headlessMirrorName := rcsw.mirroredResourceName(exportedService.Name)
	matchLabels := map[string]string{
		consts.MirroredHeadlessSvcNameLabel: headlessMirrorName,
	}

	// Fetch all Endpoint Mirror services that belong to the same Headless Mirror
	endpointMirrorServices, err := rcsw.localAPIClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		return err
	}

	var errors []error
	for _, service := range endpointMirrorServices {
		// If the service's name does not show up in the up-to-date map of
		// Endpoint Mirror names, then we should delete it.
		if _, found := endpointMirrors[service.Name]; found {
			continue
		}
		err := rcsw.localAPIClient.Client.CoreV1().Services(service.Namespace).Delete(ctx, service.Name, metav1.DeleteOptions{})
		if err != nil {
			if !kerrors.IsNotFound(err) {
				errors = append(errors, fmt.Errorf("error deleting Endpoint Mirror service %s/%s: %v", service.Namespace, service.Name, err))
			}
		}
	}
	if len(errors) > 0 {
		return RetryableError{errors}
	}

	// Update
	mirrorEndpoints.Subsets = newSubsets
	_, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(mirrorEndpoints.Namespace).Update(ctx, mirrorEndpoints, metav1.UpdateOptions{})
	if err != nil {
		return RetryableError{[]error{err}}
	}

	return nil
}

// createRemoteHeadlessService creates a mirror service for an exported headless
// service. Whether the mirror will be created as a headless or clusterIP
// service depends on the endpoints object associated with the exported service.
// If there is at least one named address, then the service will be mirrored as
// headless.
//
// Note: we do not check for any exposed ports because it was previously done
// when the service was picked up by the service mirror. We also do not need to
// check if the exported service is headless; its endpoints will be processed
// only if it is headless so we are certain at this point that is the case.
func (rcsw *RemoteClusterServiceWatcher) createRemoteHeadlessService(ctx context.Context, exportedService *corev1.Service, exportedEndpoints *corev1.Endpoints) (*corev1.Service, error) {
	// If we don't have any subsets to process then avoid creating the service.
	// We need at least one address to be make a decision (whether we should
	// create as clusterIP or headless), rely on the endpoints being eventually
	// consistent, will likely receive an update with subsets.
	if len(exportedEndpoints.Subsets) == 0 {
		return &corev1.Service{}, nil
	}

	remoteService := exportedService.DeepCopy()
	serviceInfo := fmt.Sprintf("%s/%s", remoteService.Namespace, remoteService.Name)
	localServiceName := rcsw.mirroredResourceName(remoteService.Name)

	if err := rcsw.mirrorNamespaceIfNecessary(ctx, remoteService.Namespace); err != nil {
		return &corev1.Service{}, err
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

	if shouldExportAsHeadlessService(exportedEndpoints, rcsw.log) {
		serviceToCreate.Spec.ClusterIP = corev1.ClusterIPNone
		rcsw.log.Infof("Creating a new headless service mirror for %s", serviceInfo)
	} else {
		rcsw.log.Infof("Creating a new service mirror for %s", serviceInfo)
	}

	svc, err := rcsw.localAPIClient.Client.CoreV1().Services(remoteService.Namespace).Create(ctx, serviceToCreate, metav1.CreateOptions{})
	if err != nil {
		if !kerrors.IsAlreadyExists(err) {
			// we might have created it during earlier attempt, if that is not the case, we retry
			return &corev1.Service{}, RetryableError{[]error{err}}
		}
	}

	return svc, err
}

// createHeadlessMirrorEndpoints creates an endpoints object for a Headless
// Mirror service. The endpoints object will contain the same subsets and hosts
// as the endpoints object of the exported headless service. Each host in the
// Headless Mirror's endpoints object will point to an Endpoint Mirror service.
func (rcsw *RemoteClusterServiceWatcher) createHeadlessMirrorEndpoints(ctx context.Context, exportedService *corev1.Service, exportedEndpoints *corev1.Endpoints) error {
	exportedServiceInfo := fmt.Sprintf("%s/%s", exportedService.Namespace, exportedService.Name)
	endpointsHostnames := make(map[string]struct{})
	subsetsToCreate := make([]corev1.EndpointSubset, 0, len(exportedEndpoints.Subsets))
	for _, subset := range exportedEndpoints.Subsets {
		newAddresses := make([]corev1.EndpointAddress, 0, len(subset.Addresses))
		for _, addr := range subset.Addresses {
			if addr.Hostname == "" {
				continue
			}

			endpointMirrorName := rcsw.mirroredResourceName(addr.Hostname)
			createdService, err := rcsw.createEndpointMirrorService(ctx, addr.Hostname, exportedEndpoints.ResourceVersion, endpointMirrorName, exportedService)
			if err != nil {
				rcsw.log.Errorf("error creating endpoint mirror service %s/%s for exported headless service %s: %v", endpointMirrorName, exportedService.Namespace, exportedServiceInfo, err)
				continue
			}

			endpointsHostnames[addr.Hostname] = struct{}{}
			newAddresses = append(newAddresses, corev1.EndpointAddress{
				Hostname: addr.TargetRef.Name,
				IP:       createdService.Spec.ClusterIP,
			})

		}

		if len(newAddresses) == 0 {
			continue
		}

		subsetsToCreate = append(subsetsToCreate, corev1.EndpointSubset{
			Addresses: newAddresses,
			Ports:     subset.DeepCopy().Ports,
		})
	}

	headlessMirrorServiceName := rcsw.mirroredResourceName(exportedService.Name)
	headlessMirrorEndpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headlessMirrorServiceName,
			Namespace: exportedService.Namespace,
			Labels: map[string]string{
				consts.MirroredResourceLabel:  "true",
				consts.RemoteClusterNameLabel: rcsw.link.TargetClusterName,
			},
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.%s", exportedService.Name, exportedService.Namespace, rcsw.link.TargetClusterDomain),
			},
		},
		Subsets: subsetsToCreate,
	}

	if rcsw.link.GatewayIdentity != "" {
		headlessMirrorEndpoints.Annotations[consts.RemoteGatewayIdentity] = rcsw.link.GatewayIdentity
	}

	rcsw.log.Infof("Creating a new headless mirror endpoints object for headless mirror %s/%s", headlessMirrorServiceName, exportedService.Namespace)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(exportedService.Namespace).Create(ctx, headlessMirrorEndpoints, metav1.CreateOptions{}); err != nil {
		// we clean up after ourselves
		rcsw.localAPIClient.Client.CoreV1().Services(exportedService.Namespace).Delete(ctx, headlessMirrorServiceName, metav1.DeleteOptions{})
		// and retry
		return RetryableError{[]error{err}}
	}

	return nil
}

// createEndpointMirrorService creates a new Endpoint Mirror service and its
// corresponding endpoints object. It returns the newly created Endpoint Mirror
// service object. When a headless service is exported, we create a Headless
// Mirror service in the source cluster and then for each hostname in the
// exported service's endpoints object, we also create an Endpoint Mirror
// service (and its corresponding endpoints object).
func (rcsw *RemoteClusterServiceWatcher) createEndpointMirrorService(ctx context.Context, endpointHostname, resourceVersion, endpointMirrorName string, exportedService *corev1.Service) (*corev1.Service, error) {
	gatewayAddresses, err := rcsw.resolveGatewayAddress()
	if err != nil {
		return nil, err
	}

	endpointMirrorAnnotations := map[string]string{
		// needed to detect real changes
		consts.RemoteResourceVersionAnnotation: resourceVersion,
		consts.RemoteServiceFqName:             fmt.Sprintf("%s.%s.%s.svc.%s", endpointHostname, exportedService.Name, exportedService.Namespace, rcsw.link.TargetClusterDomain),
	}

	endpointMirrorLabels := rcsw.getMirroredServiceLabels()
	mirrorServiceName := rcsw.mirroredResourceName(exportedService.Name)
	endpointMirrorLabels[consts.MirroredHeadlessSvcNameLabel] = mirrorServiceName

	// Create service spec, clusterIP
	endpointMirrorService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        endpointMirrorName,
			Namespace:   exportedService.Namespace,
			Annotations: endpointMirrorAnnotations,
			Labels:      endpointMirrorLabels,
		},
		Spec: corev1.ServiceSpec{
			Ports: remapRemoteServicePorts(exportedService.Spec.Ports),
		},
	}
	endpointMirrorEndpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      endpointMirrorService.Name,
			Namespace: endpointMirrorService.Namespace,
			Labels:    endpointMirrorLabels,
			Annotations: map[string]string{
				consts.RemoteServiceFqName: endpointMirrorService.Annotations[consts.RemoteServiceFqName],
			},
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: gatewayAddresses,
				Ports:     rcsw.getEndpointsPorts(exportedService),
			},
		},
	}

	if rcsw.link.GatewayIdentity != "" {
		endpointMirrorEndpoints.Annotations[consts.RemoteGatewayIdentity] = rcsw.link.GatewayIdentity
	}

	exportedServiceInfo := fmt.Sprintf("%s/%s", exportedService.Namespace, exportedService.Name)
	endpointMirrorInfo := fmt.Sprintf("%s/%s", endpointMirrorService.Namespace, endpointMirrorName)
	rcsw.log.Infof("Creating a new endpoint mirror service %s for exported headless service %s", endpointMirrorInfo, exportedServiceInfo)
	createdService, err := rcsw.localAPIClient.Client.CoreV1().Services(endpointMirrorService.Namespace).Create(ctx, endpointMirrorService, metav1.CreateOptions{})
	if err != nil {
		if !kerrors.IsAlreadyExists(err) {
			// we might have created it during earlier attempt, if that is not the case, we retry
			return createdService, RetryableError{[]error{err}}
		}
	}

	rcsw.log.Infof("Creating a new endpoints object for endpoint mirror service %s", endpointMirrorInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(endpointMirrorService.Namespace).Create(ctx, endpointMirrorEndpoints, metav1.CreateOptions{}); err != nil {
		// If we cannot create an Endpoints object for the Endpoint Mirror
		// service, then delete the Endpoint Mirror service we just created
		rcsw.localAPIClient.Client.CoreV1().Services(endpointMirrorService.Namespace).Delete(ctx, endpointMirrorName, metav1.DeleteOptions{})
		// and retry
		return createdService, RetryableError{[]error{err}}
	}

	return createdService, nil
}

// shouldExportAsHeadlessService checks if an exported service should be
// mirrored as a headless service or as a clusterIP service, based on its
// endpoints object. For an exported service to be a headless mirror, it needs
// to have at least one named address in its endpoints (that is, a pod with a
// `hostname`). If the endpoints object does not contain at least one named
// address, it should be exported as clusterIP.
func shouldExportAsHeadlessService(endpoints *corev1.Endpoints, log *logging.Entry) bool {
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			if addr.Hostname != "" {
				return true
			}
		}

		for _, addr := range subset.NotReadyAddresses {
			if addr.Hostname != "" {
				return true
			}
		}
	}
	log.Infof("Service %s/%s should not be exported as headless: no named addresses in its endpoints object", endpoints.Namespace, endpoints.Name)
	return false
}

// isExportedHeadlessEndpoints checks if an endpoints object belongs to a
// headless exported service.
func isExportedHeadlessEndpoints(obj interface{}, log *logging.Entry) bool {
	ep, ok := obj.(*corev1.Endpoints)
	if !ok {
		log.Errorf("error processing endpoints object: got %#v, expected *corev1.Endpoints", ep)
		return false
	}

	if _, found := ep.Labels[corev1.IsHeadlessService]; !found {
		// Not an Endpoints object for a headless service? Then we likely don't want
		// to update anything.
		log.Debugf("skipped processing endpoints object %s/%s: missing %s label", ep.Namespace, ep.Name, corev1.IsHeadlessService)
		return false
	}

	// If Endpoints belong to an unexported service, ignore.
	if _, found := ep.Labels[consts.DefaultExportedServiceSelector]; !found {
		log.Debugf("skipped processing endpoints object %s/%s: missing %s label", ep.Namespace, ep.Name, consts.DefaultExportedServiceSelector)
		return false
	}

	return true
}
