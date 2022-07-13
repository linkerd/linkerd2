package servicemirror

import (
	"context"
	"errors"
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

const (
	eventTypeSkipped = "ServiceMirroringSkipped"
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
		gatewayAlive            bool
		liveness                chan bool
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

	// OnLocalNamespaceAdded is issued when when a new namespace is added to
	// the local cluster. This means that we should check the remote cluster
	// for exported service in that namespace.
	OnLocalNamespaceAdded struct {
		ns *corev1.Namespace
	}

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
	liveness chan bool,
	enableHeadlessSvc bool,
) (*RemoteClusterServiceWatcher, error) {
	remoteAPI, err := k8s.InitializeAPIForConfig(ctx, cfg, false, k8s.Svc, k8s.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize api for target cluster %s: %w", clusterName, err)
	}
	_, err = remoteAPI.Client.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to api for target cluster %s: %w", clusterName, err)
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
		liveness:                liveness,
		headlessServicesEnabled: enableHeadlessSvc,
		// always instantiate the gatewayAlive=true to prevent unexpected service fail fast
		gatewayAlive: true,
	}, nil
}

func (rcsw *RemoteClusterServiceWatcher) mirroredResourceName(remoteName string) string {
	return fmt.Sprintf("%s-%s", remoteName, rcsw.link.TargetClusterName)
}

func (rcsw *RemoteClusterServiceWatcher) targetResourceName(mirrorName string) string {
	return strings.TrimSuffix(mirrorName, "-"+rcsw.link.TargetClusterName)
}

func (rcsw *RemoteClusterServiceWatcher) originalResourceName(mirroredName string) string {
	return strings.TrimSuffix(mirroredName, fmt.Sprintf("-%s", rcsw.link.TargetClusterName))
}

// Provides labels for mirrored service.
// "remoteService" is an optional parameter. If provided, copies all labels
// from the remote service to mirrored service (except labels with the
// "SvcMirrorPrefix").
func (rcsw *RemoteClusterServiceWatcher) getMirroredServiceLabels(remoteService *corev1.Service) map[string]string {
	labels := map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteClusterNameLabel: rcsw.link.TargetClusterName,
	}

	if remoteService == nil {
		return labels
	}

	for key, value := range remoteService.ObjectMeta.Labels {
		if strings.HasPrefix(key, consts.SvcMirrorPrefix) {
			continue
		}
		labels[key] = value
	}

	return labels
}

// Provides annotations for mirrored service
func (rcsw *RemoteClusterServiceWatcher) getMirroredServiceAnnotations(remoteService *corev1.Service) map[string]string {
	annotations := map[string]string{
		consts.RemoteResourceVersionAnnotation: remoteService.ResourceVersion, // needed to detect real changes
		consts.RemoteServiceFqName:             fmt.Sprintf("%s.%s.svc.%s", remoteService.Name, remoteService.Namespace, rcsw.link.TargetClusterDomain),
	}

	for key, value := range remoteService.ObjectMeta.Annotations {
		annotations[key] = value
	}

	value, ok := remoteService.GetAnnotations()[consts.ProxyOpaquePortsAnnotation]
	if ok {
		annotations[consts.ProxyOpaquePortsAnnotation] = value
	}

	return annotations
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
		innerErr := fmt.Errorf("failed to list services while cleaning up mirror services: %w", err)
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
	matchLabels := rcsw.getMirroredServiceLabels(nil)

	services, err := rcsw.localAPIClient.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		innerErr := fmt.Errorf("could not retrieve mirrored services that need cleaning up: %w", err)
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
			errors = append(errors, fmt.Errorf("Could not delete service %s/%s: %w", svc.Namespace, svc.Name, err))
		} else {
			rcsw.log.Infof("Deleted service %s/%s", svc.Namespace, svc.Name)
		}
	}

	endpoints, err := rcsw.localAPIClient.Endpoint().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		innerErr := fmt.Errorf("could not retrieve endpoints that need cleaning up: %w", err)
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
			errors = append(errors, fmt.Errorf("Could not delete endpoints %s/%s: %w", endpoint.Namespace, endpoint.Name, err))
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
		errors = append(errors, fmt.Errorf("could not fetch service %s/%s: %w", ev.Namespace, localServiceName, err))
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
				errors = append(errors, fmt.Errorf("could not fetch endpoint mirrors for mirror service %s/%s: %w", ev.Namespace, localServiceName, err))
			}
		}

		for _, endpointMirror := range endpointMirrorServices {
			err = rcsw.localAPIClient.Client.CoreV1().Services(endpointMirror.Namespace).Delete(ctx, endpointMirror.Name, metav1.DeleteOptions{})
			if err != nil {
				if !kerrors.IsNotFound(err) {
					errors = append(errors, fmt.Errorf("could not delete endpoint mirror %s/%s: %w", endpointMirror.Namespace, endpointMirror.Name, err))
				}
			}
		}
	}

	rcsw.log.Infof("Deleting mirrored service %s/%s", ev.Namespace, localServiceName)
	if err := rcsw.localAPIClient.Client.CoreV1().Services(ev.Namespace).Delete(ctx, localServiceName, metav1.DeleteOptions{}); err != nil {
		if !kerrors.IsNotFound(err) {
			errors = append(errors, fmt.Errorf("could not delete service: %s/%s: %w", ev.Namespace, localServiceName, err))
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

	err = rcsw.updateMirrorEndpoints(ctx, copiedEndpoints)
	if err != nil {
		return RetryableError{[]error{err}}
	}

	ev.localService.Labels = rcsw.getMirroredServiceLabels(ev.remoteUpdate)
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
	if rcsw.headlessServicesEnabled && remoteService.Spec.ClusterIP == corev1.ClusterIPNone {
		return nil
	}

	serviceInfo := fmt.Sprintf("%s/%s", remoteService.Namespace, remoteService.Name)
	localServiceName := rcsw.mirroredResourceName(remoteService.Name)

	// Ensure the namespace exists, and skip mirroring if it doesn't
	if _, err := rcsw.localAPIClient.Client.CoreV1().Namespaces().Get(ctx, remoteService.Namespace, metav1.GetOptions{}); err != nil {
		if kerrors.IsNotFound(err) {
			rcsw.recorder.Event(remoteService, v1.EventTypeNormal, eventTypeSkipped, "Skipped mirroring service: namespace does not exist")
			rcsw.log.Warnf("Skipping mirroring of service %s: namespace %s does not exist", serviceInfo, remoteService.Namespace)
			return nil
		}
		// something else went wrong, so we can just retry
		return RetryableError{[]error{err}}
	}

	serviceToCreate := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        localServiceName,
			Namespace:   remoteService.Namespace,
			Annotations: rcsw.getMirroredServiceAnnotations(remoteService),
			Labels:      rcsw.getMirroredServiceLabels(remoteService),
		},
		Spec: corev1.ServiceSpec{
			Ports: remapRemoteServicePorts(remoteService.Spec.Ports),
		},
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

func (rcsw *RemoteClusterServiceWatcher) handleLocalNamespaceAdded(ns *corev1.Namespace) error {
	// When a local namespace is added, we issue a create event for all the services in the corresponding namespace in
	// case any of them are exported and need to be mirrored.
	svcs, err := rcsw.remoteAPIClient.Svc().Lister().Services(ns.Name).List(labels.Everything())
	if err != nil {
		return RetryableError{[]error{err}}
	}
	for _, svc := range svcs {
		rcsw.eventsQueue.Add(&OnAddCalled{
			svc: svc,
		})
	}
	return nil
}

// isEmptyService returns true if any of these conditions are true:
// - svc's Endpoint is not found
// - svc's Endpoint has no Subsets (happens when there's no associated Pod)
// - svc's Endpoint has Subsets, but none have addresses (only notReadyAddresses,
// when the pod is not ready yet)
func (rcsw *RemoteClusterServiceWatcher) isEmptyService(svc *corev1.Service) (bool, error) {
	ep, err := rcsw.remoteAPIClient.Endpoint().Lister().Endpoints(svc.Namespace).Get(svc.Name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			rcsw.log.Debugf("target endpoint %s/%s not found", svc.Namespace, svc.Name)
			return true, nil
		}

		return true, err
	}
	return rcsw.isEmptyEndpoints(ep), nil
}

// isEmptyEndpoints returns true if any of these conditions are true:
// - The Endpoint is not found
// - The Endpoint has no Subsets (happens when there's no associated Pod)
// - The Endpoint has Subsets, but none have addresses (only notReadyAddresses,
// when the pod is not ready yet)
func (rcsw *RemoteClusterServiceWatcher) isEmptyEndpoints(ep *corev1.Endpoints) bool {
	if len(ep.Subsets) == 0 {
		rcsw.log.Debugf("endpoint %s/%s has no Subsets", ep.Namespace, ep.Name)
		return true
	}
	for _, subset := range ep.Subsets {
		if len(subset.Addresses) > 0 {
			return false
		}
	}
	rcsw.log.Debugf("endpoint %s/%s has no ready addresses", ep.Namespace, ep.Name)
	return true
}

func (rcsw *RemoteClusterServiceWatcher) createGatewayEndpoints(ctx context.Context, exportedService *corev1.Service) error {
	empty, err := rcsw.isEmptyService(exportedService)
	if err != nil {
		return RetryableError{[]error{err}}
	}

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

	if !empty && len(gatewayAddresses) > 0 {
		endpointsToCreate.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayAddresses,
				Ports:     rcsw.getEndpointsPorts(exportedService),
			},
		}
	} else if !empty {
		endpointsToCreate.Subsets = []corev1.EndpointSubset{
			{
				NotReadyAddresses: gatewayAddresses,
				Ports:             rcsw.getEndpointsPorts(exportedService),
			},
		}
		rcsw.log.Warnf("could not resolve gateway addresses for %s; setting endpoint subsets to not ready", serviceInfo)
	} else {
		rcsw.log.Warnf("exported service %s is empty", serviceInfo)
	}

	if rcsw.link.GatewayIdentity != "" {
		endpointsToCreate.Annotations[consts.RemoteGatewayIdentity] = rcsw.link.GatewayIdentity
	}

	rcsw.log.Infof("Creating a new endpoints for %s", serviceInfo)
	err = rcsw.createMirrorEndpoints(ctx, endpointsToCreate)
	if err != nil {
		if svcErr := rcsw.localAPIClient.Client.CoreV1().Services(exportedService.Namespace).Delete(ctx, localServiceName, metav1.DeleteOptions{}); svcErr != nil {
			rcsw.log.Errorf("Failed to delete service %s after endpoints creation failed: %s", localServiceName, svcErr)
		}
		return RetryableError{[]error{err}}
	}
	return nil
}

// this method is common to both CREATE and UPDATE because if we have been
// offline for some time due to a crash a CREATE for a service that we have
// observed before is simply a case of UPDATE
func (rcsw *RemoteClusterServiceWatcher) createOrUpdateService(service *corev1.Service) error {
	localName := rcsw.mirroredResourceName(service.Name)

	if rcsw.isExported(service.Labels) {
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

func (rcsw *RemoteClusterServiceWatcher) getMirrorServices() (*corev1.ServiceList, error) {
	matchLabels := map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteClusterNameLabel: rcsw.link.TargetClusterName,
	}
	services, err := rcsw.localAPIClient.Client.CoreV1().Services("").List(context.Background(), metav1.ListOptions{LabelSelector: labels.SelectorFromSet(matchLabels).String()})
	if err != nil {
		return nil, err
	}
	return services, nil
}

func (rcsw *RemoteClusterServiceWatcher) handleOnDelete(service *corev1.Service) {
	if rcsw.isExported(service.Labels) {
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
	} else if done {
		rcsw.log.Infof("Received: Stop")
	}

	var err error
	switch ev := event.(type) {
	case *OnAddCalled:
		err = rcsw.createOrUpdateService(ev.svc)
	case *OnAddEndpointsCalled:
		err = rcsw.handleCreateOrUpdateEndpoints(ctx, ev.ep)
	case *OnUpdateCalled:
		err = rcsw.createOrUpdateService(ev.svc)
	case *OnUpdateEndpointsCalled:
		err = rcsw.handleCreateOrUpdateEndpoints(ctx, ev.ep)
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
	case *OnLocalNamespaceAdded:
		err = rcsw.handleLocalNamespaceAdded(ev.ns)
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
			var re RetryableError
			if errors.As(err, &re) {
				rcsw.log.Warnf("Requeues: %d, Limit: %d for event %s", rcsw.eventsQueue.NumRequeues(event), rcsw.requeueLimit, event)
				if (rcsw.eventsQueue.NumRequeues(event) < rcsw.requeueLimit) && !done {
					rcsw.log.Errorf("Error processing %s (will retry): %s", event, re)
					rcsw.eventsQueue.AddRateLimited(event)
				} else {
					rcsw.log.Errorf("Error processing %s (giving up): %s", event, re)
					rcsw.eventsQueue.Forget(event)
				}
			} else {
				rcsw.log.Errorf("Error processing %s (will not retry): %s", event, err)
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
			UpdateFunc: func(_, new interface{}) {
				rcsw.eventsQueue.Add(&OnUpdateCalled{new.(*corev1.Service)})
			},
		},
	)

	rcsw.remoteAPIClient.Endpoint().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			// AddFunc only relevant for exported headless endpoints
			AddFunc: func(obj interface{}) {
				ep, ok := obj.(*corev1.Endpoints)
				if !ok {
					rcsw.log.Errorf("error processing endpoints object: got %#v, expected *corev1.Endpoints", ep)
					return
				}

				if !rcsw.isExported(ep.Labels) {
					rcsw.log.Debugf("skipped processing endpoints object %s/%s: missing %s label", ep.Namespace, ep.Name, consts.DefaultExportedServiceSelector)
					return
				}

				if !isHeadlessEndpoints(ep, rcsw.log) {
					return
				}

				rcsw.eventsQueue.Add(&OnAddEndpointsCalled{obj.(*corev1.Endpoints)})
			},
			// AddFunc relevant for all kind of exported endpoints
			UpdateFunc: func(_, new interface{}) {
				epNew, ok := new.(*corev1.Endpoints)
				if !ok {
					rcsw.log.Errorf("error processing endpoints object: got %#v, expected *corev1.Endpoints", epNew)
					return
				}
				if !rcsw.isExported(epNew.Labels) {
					rcsw.log.Debugf("skipped processing endpoints object %s/%s: missing %s label", epNew.Namespace, epNew.Name, consts.DefaultExportedServiceSelector)
					return
				}
				rcsw.eventsQueue.Add(&OnUpdateEndpointsCalled{epNew})
			},
		},
	)

	rcsw.localAPIClient.NS().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				rcsw.eventsQueue.Add(&OnLocalNamespaceAdded{obj.(*corev1.Namespace)})
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
			case alive := <-rcsw.liveness:
				rcsw.log.Debugf("gateway liveness change from %t to %t", rcsw.gatewayAlive, alive)
				rcsw.gatewayAlive = alive
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
			err = fmt.Errorf("Error resolving '%s': %w", addr, err)
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
	endpointRepairCounter.With(prometheus.Labels{
		gatewayClusterName: rcsw.link.TargetClusterName,
	}).Inc()

	// Create or update the gateway mirror endpoints responsible for driving
	// the cluster watcher's gateway liveness status.
	gatewayAddresses, err := rcsw.resolveGatewayAddress()
	if err != nil {
		return err
	}
	err = rcsw.createOrUpdateGatewayEndpoints(ctx, gatewayAddresses)
	if err != nil {
		rcsw.log.Errorf("Failed to create/update gateway mirror endpoints: %s", err)
	}

	// Repair mirror service endpoints.
	mirrorServices, err := rcsw.getMirrorServices()
	if err != nil {
		rcsw.log.Errorf("Failed to list mirror services: %s", err)
	}
	for _, svc := range mirrorServices.Items {
		svc := svc

		// Mirrors for headless services are also headless, and their
		// Endpoints point to auxiliary services instead of pointing to
		// the gateway, so they're skipped.
		if svc.Spec.ClusterIP == corev1.ClusterIPNone {
			rcsw.log.Debugf("Skipped repairing endpoints for headless mirror %s/%s", svc.Namespace, svc.Name)
			continue
		}

		endpoints, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(svc.Namespace).Get(svc.Name)
		if err != nil {
			if !kerrors.IsNotFound(err) {
				rcsw.log.Errorf("Failed to list local endpoints: %s", err)
				continue
			}
			endpoints, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
			if err != nil {
				rcsw.log.Errorf("Failed to get local endpoints %s/%s: %s", svc.Namespace, svc.Name, err)
			}
		}
		updatedEndpoints := endpoints.DeepCopy()
		updatedEndpoints.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayAddresses,
				Ports:     rcsw.getEndpointsPorts(&svc),
			},
		}

		// We want to skip this service empty check for auxiliary services --
		// services which are not headless but do belong to a headless
		// mirrored service. This is because they do not have a corresponding
		// endpoint on the target cluster, only a pod. If we attempt to find
		// endpoints for services like this, they'll always be set to empty.
		if _, found := svc.Labels[consts.MirroredHeadlessSvcNameLabel]; !found {
			targetService := svc.DeepCopy()
			targetService.Name = rcsw.targetResourceName(svc.Name)
			empty, err := rcsw.isEmptyService(targetService)
			if err != nil {
				rcsw.log.Errorf("Could not check service emptiness: %s", err)
				continue
			}
			if empty {
				rcsw.log.Warnf("Exported service %s/%s is empty", targetService.Namespace, targetService.Name)
				updatedEndpoints.Subsets = []corev1.EndpointSubset{}
			}
		}

		if updatedEndpoints.Annotations == nil {
			updatedEndpoints.Annotations = make(map[string]string)
		}
		updatedEndpoints.Annotations[consts.RemoteGatewayIdentity] = rcsw.link.GatewayIdentity

		err = rcsw.updateMirrorEndpoints(ctx, updatedEndpoints)
		if err != nil {
			rcsw.log.Error(err)
		}
	}

	return nil
}

// createOrUpdateGatewayEndpoints will create or update the gateway mirror
// endpoints for a remote cluster. These endpoints are required for the probe
// worker responsible for probing gateway liveness, so these endpoints are
// never in a not ready state.
func (rcsw *RemoteClusterServiceWatcher) createOrUpdateGatewayEndpoints(ctx context.Context, addressses []v1.EndpointAddress) error {
	gatewayMirrorName := fmt.Sprintf("probe-gateway-%s", rcsw.link.TargetClusterName)
	endpoints := &corev1.Endpoints{
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
				Addresses: addressses,
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
	_, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(endpoints.Namespace).Get(ctx, endpoints.Name, metav1.GetOptions{})
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		// Mirror endpoints for the gateway do not exist so they need to be
		// created. As mentioned above, these endpoints are required for the
		// probe worker and therefore should never be put in a not ready
		// state.
		_, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(endpoints.Namespace).Create(ctx, endpoints, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		return nil
	}

	// Mirror endpoints for the gateway already exist so they need to be
	// updated. As mentioned above, these endpoints are required for the probe
	// worker and therefore should never be put in a not ready state.
	_, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(endpoints.Namespace).Update(ctx, endpoints, metav1.UpdateOptions{})
	return err
}

// handleCreateOrUpdateEndpoints forwards the call to
// createOrUpdateHeadlessEndpoints when adding/updating exported headless
// endpoints. Otherwise, it handles updates to endpoints to check if they've
// become empty/filled since their creation, in order to empty/fill the
// mirrored endpoints as well
func (rcsw *RemoteClusterServiceWatcher) handleCreateOrUpdateEndpoints(
	ctx context.Context,
	exportedEndpoints *corev1.Endpoints,
) error {
	if isHeadlessEndpoints(exportedEndpoints, rcsw.log) {
		if rcsw.headlessServicesEnabled {
			return rcsw.createOrUpdateHeadlessEndpoints(ctx, exportedEndpoints)
		}
		return nil
	}

	localServiceName := rcsw.mirroredResourceName(exportedEndpoints.Name)
	ep, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(exportedEndpoints.Namespace).Get(localServiceName)
	if err != nil {
		return RetryableError{[]error{err}}
	}

	if (rcsw.isEmptyEndpoints(ep) && rcsw.isEmptyEndpoints(exportedEndpoints)) ||
		(!rcsw.isEmptyEndpoints(ep) && !rcsw.isEmptyEndpoints(exportedEndpoints)) {
		return nil
	}

	rcsw.log.Infof("Updating subsets for mirror endpoint %s/%s", exportedEndpoints.Namespace, exportedEndpoints.Name)
	if rcsw.isEmptyEndpoints(exportedEndpoints) {
		ep.Subsets = []corev1.EndpointSubset{}
	} else {
		exportedService, err := rcsw.remoteAPIClient.Svc().Lister().Services(exportedEndpoints.Namespace).Get(exportedEndpoints.Name)
		if err != nil {
			return RetryableError{[]error{
				fmt.Errorf("error retrieving exported service %s/%s: %w", exportedEndpoints.Namespace, exportedEndpoints.Name, err),
			}}
		}
		gatewayAddresses, err := rcsw.resolveGatewayAddress()
		if err != nil {
			return err
		}
		ep.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayAddresses,
				Ports:     rcsw.getEndpointsPorts(exportedService),
			},
		}
	}
	return rcsw.updateMirrorEndpoints(ctx, ep)
}

// createMirrorEndpoints will create endpoints based off gateway liveness. If
// the gateway is not alive, then the addresses in each subset will be set to
// not ready.
func (rcsw *RemoteClusterServiceWatcher) createMirrorEndpoints(ctx context.Context, endpoints *corev1.Endpoints) error {
	rcsw.updateReadiness(endpoints)
	_, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(endpoints.Namespace).Create(ctx, endpoints, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create mirror endpoints for %s/%s: %w", endpoints.Namespace, endpoints.Name, err)
	}
	return nil
}

// updateMirrorEndpoints will update endpoints based off gateway liveness. If
// the gateway is not alive, then the addresses in each subset will be set to
// not ready. Future calls to updateMirrorEndpoints can set the addresses back
// to ready if the gateway is alive.
func (rcsw *RemoteClusterServiceWatcher) updateMirrorEndpoints(ctx context.Context, endpoints *corev1.Endpoints) error {
	rcsw.updateReadiness(endpoints)
	_, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(endpoints.Namespace).Update(ctx, endpoints, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update mirror endpoints for %s/%s: %w", endpoints.Namespace, endpoints.Name, err)
	}
	return err
}

func (rcsw *RemoteClusterServiceWatcher) updateReadiness(endpoints *corev1.Endpoints) {
	if !rcsw.gatewayAlive {
		rcsw.log.Warnf("gateway for %s/%s does not have ready addresses; setting addresses to not ready", endpoints.Namespace, endpoints.Name)
		for i := range endpoints.Subsets {
			endpoints.Subsets[i].NotReadyAddresses = append(endpoints.Subsets[i].NotReadyAddresses, endpoints.Subsets[i].Addresses...)
			endpoints.Subsets[i].Addresses = nil
		}
	}
}

func (rcsw *RemoteClusterServiceWatcher) isExported(l map[string]string) bool {
	selector, err := metav1.LabelSelectorAsSelector(&rcsw.link.Selector)
	if err != nil {
		rcsw.log.Errorf("Invalid selector: %s", err)
		return false
	}
	return selector.Matches(labels.Set(l))
}
