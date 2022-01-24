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

const (
	eventTypeSkipped = "ServiceMirroringSkipped"
	kubeSystem       = "kube-system"
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
		serviceMirrorNamespace     string
		link                       *multicluster.Link
		remoteAPIClient            *k8s.API
		localAPIClient             *k8s.API
		stopper                    chan struct{}
		recorder                   record.EventRecorder
		log                        *logging.Entry
		eventsQueue                workqueue.RateLimitingInterface
		requeueLimit               int
		repairPeriod               time.Duration
		headlessServicesEnabled    bool
		endpointMirrorServiceCache cache.Store
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
		Name       string
		Namespace  string
		GlobalName *string
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
		eventsQueue:                workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		requeueLimit:               requeueLimit,
		repairPeriod:               repairPeriod,
		headlessServicesEnabled:    enableHeadlessSvc,
		endpointMirrorServiceCache: NewEndpointMirrorServiceCache(),
	}, nil
}

func NewEndpointMirrorServiceCache() cache.Store {
	return cache.NewTTLStore(func(obj interface{}) (string, error) {
		svc, ok := obj.(*corev1.Service)
		if ok {
			return fmt.Sprintf("%s/%s", svc.Namespace, svc.Name), nil
		}
		return "", fmt.Errorf("object inserted into endpoint mirror service cache is not a valid service")
	}, 30*time.Second)
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

	globalName, found := remoteService.ObjectMeta.Labels[consts.GlobalServiceNameLabel]

	if found {
		labels[consts.GlobalServiceNameLabel] = globalName
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

func (rcsw *RemoteClusterServiceWatcher) cleanupOrphans(ctx context.Context) error {
	var errors []error

	err := rcsw.cleanupOrphanedServices(ctx)
	if err != nil {
		errors = append(errors, err)
	}

	err = rcsw.cleanupOrphanedEndpointSlices(ctx)
	if err != nil {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return RetryableError{errors}
	}

	return nil
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

func (rcsw *RemoteClusterServiceWatcher) cleanupOrphanedEndpointSlices(ctx context.Context) error {
	matchLabels := map[string]string{
		consts.MirroredResourceLabel: "true",
		// Omit the remote cluster name because this GC step can be performed by any service mirror.
		// The redundancy here is OK. If we're raced on deleting a resource, we'll log and continue.
	}

	slicesOnLocalCluster, err := rcsw.localAPIClient.ES().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		innerErr := fmt.Errorf("failed to list services while cleaning up mirror services: %s", err)
		if kerrors.IsNotFound(err) {
			return innerErr
		}
		// if it is something else, we can just retry
		return RetryableError{[]error{innerErr}}
	}

	var errors []error
	for _, es := range slicesOnLocalCluster {
		// Each endpoint slice will have a label "kubernetes.io/service-name: ..."
		// If that service no longer exists, we should prune it.

		_, err := rcsw.remoteAPIClient.Svc().Lister().Services(es.Namespace).Get(es.Labels["kubernetes.io/service_name"])
		if err != nil {
			if kerrors.IsNotFound(err) {
				// service does not exist anymore. Need to delete
				if err := rcsw.localAPIClient.Client.DiscoveryV1beta1().EndpointSlices(es.Namespace).Delete(ctx, es.Name, metav1.DeleteOptions{}); err != nil {
					if kerrors.IsNotFound(err) {
						rcsw.log.Infof("Would delete endpoint slice %s/%s while cleaning up, but already deleted", es.Namespace, es.Name)
					} else {
						// something went wrong with deletion, we need to retry
						errors = append(errors, err)
					}
				} else {
					rcsw.log.Infof("Deleted endpoint slice %s/%s while cleaning up", es.Namespace, es.Name)
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

	endpointSlices, err := rcsw.localAPIClient.ES().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		innerErr := fmt.Errorf("could not retrieve endpoint slices that need cleaning up: %s", err)
		if kerrors.IsNotFound(err) {
			return innerErr
		}
		return RetryableError{[]error{innerErr}}
	}

	for _, endpoint := range endpointSlices {
		if err := rcsw.localAPIClient.Client.DiscoveryV1beta1().EndpointSlices(endpoint.Namespace).Delete(ctx, endpoint.Name, metav1.DeleteOptions{}); err != nil {
			if kerrors.IsNotFound(err) {
				continue
			}
			errors = append(errors, fmt.Errorf("Could not delete endpoint slices %s/%s: %s", endpoint.Namespace, endpoint.Name, err))
		} else {
			rcsw.log.Infof("Deleted endpoint slices %s/%s", endpoint.Namespace, endpoint.Name)
		}
	}

	if len(errors) > 0 {
		return RetryableError{errors}
	}
	return nil
}

// Deletes a locally mirrored service as it is not present on the remote cluster anymore
func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceDeleted(ctx context.Context, ev *RemoteServiceDeleted) error {
	var errors []error

	errs := rcsw.handleRemoteServiceDeletedMirrors(ctx, ev)
	if len(errs) > 0 {
		errors = append(errors, errs...)
	}

	if ev.GlobalName != nil {
		errs := rcsw.cleanupGlobalMirror(ctx, ev.Namespace, *ev.GlobalName)
		if len(errs) > 0 {
			errors = append(errors, errs...)
		}
	}

	if len(errors) > 0 {
		return RetryableError{errors}
	}

	return nil
}

// Deletes a locally mirrored service as it is not present on the remote cluster anymore
func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceDeletedMirrors(ctx context.Context, ev *RemoteServiceDeleted) []error {
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

	rcsw.log.Infof("Successfully deleted service: %s/%s", ev.Namespace, localServiceName)
	return errors
}

func (rcsw *RemoteClusterServiceWatcher) cleanupGlobalMirror(ctx context.Context, namespace string, globalName string) []error {
	var errors []error

	remainingMirrors, err := rcsw.localAPIClient.Client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(&metav1.LabelSelector{
			MatchLabels: map[string]string{
				consts.GlobalServiceNameLabel: globalName,
				consts.MirroredResourceLabel:  "true",
			},
		}),
	})

	if err != nil {
		errors = append(errors, fmt.Errorf("could not list remotes used by global service %s/%s: %s", namespace, globalName, err))
	}

	remainingMirrorCount := len(remainingMirrors.Items)
	// The global mirror itself will be the last to match our label selector:
	if remainingMirrorCount > 1 {
		rcsw.log.Infof("Refraining from deleting global service %s/%s, found %d remaining mirrors", namespace, globalName, remainingMirrorCount)
	} else {
		if err := rcsw.localAPIClient.Client.CoreV1().Services(namespace).Delete(ctx, globalName, metav1.DeleteOptions{}); err != nil {
			if !kerrors.IsNotFound(err) {
				errors = append(errors, fmt.Errorf("could not delete global service: %s/%s: %s", namespace, globalName, err))
			}
		}
	}

	return errors
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
	} else {
		rcsw.log.Warnf("exported service is empty or gateway for %s does not have ready addresses, skipping subsets", serviceInfo)
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
				rcsw.eventsQueue.Add(remoteServiceDeletedEvent(service))
			}
		}
	}
	return nil
}

func remoteServiceDeletedEvent(service *v1.Service) *RemoteServiceDeleted {
	var GlobalName *string
	value, found := service.Labels[consts.GlobalServiceNameLabel]
	if found {
		GlobalName = &value
	}

	event := RemoteServiceDeleted{
		Name:       service.Name,
		Namespace:  service.Namespace,
		GlobalName: GlobalName,
	}
	return &event
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
		rcsw.eventsQueue.Add(remoteServiceDeletedEvent(service))
	} else {
		rcsw.log.Infof("Skipping OnDelete for service %s", service)
	}
}

func (rcsw *RemoteClusterServiceWatcher) processNextEvent(ctx context.Context) (bool, interface{}, error) {
	event, done := rcsw.eventsQueue.Get()
	if event != nil {
		stringlike, ok := event.(fmt.Stringer)
		if ok {
			rcsw.log.Infof("Received: %s", stringlike.String())
		} else {
			rcsw.log.Infof("Received: %s", event)
		}
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
		err = rcsw.cleanupOrphans(ctx)
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

	rcsw.remoteAPIClient.Endpoint().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			// AddFunc only relevant for exported headless endpoints
			AddFunc: func(obj interface{}) {
				if obj.(metav1.Object).GetNamespace() == kubeSystem {
					return
				}

				if !isExportedEndpoints(obj, rcsw.log) || !isHeadlessEndpoints(obj, rcsw.log) {
					return
				}

				rcsw.eventsQueue.Add(&OnAddEndpointsCalled{obj.(*corev1.Endpoints)})
			},
			// AddFunc relevant for all kind of exported endpoints
			UpdateFunc: func(old, new interface{}) {
				if new.(metav1.Object).GetNamespace() == kubeSystem {
					return
				}

				if !isExportedEndpoints(old, rcsw.log) {
					return
				}

				rcsw.eventsQueue.Add(&OnUpdateEndpointsCalled{new.(*corev1.Endpoints)})
			},
		},
	)

	rcsw.localAPIClient.NS().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if obj.(metav1.Object).GetName() == kubeSystem {
					return
				}

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
			rcsw.log.Errorf("Could not get local endpoints: %s", err)
			continue
		}

		updatedEndpoints := endpoints.DeepCopy()
		updatedEndpoints.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayAddresses,
				Ports:     rcsw.getEndpointsPorts(updatedService),
			},
		}

		// If the Service's Endpoints has no Subsets, use an empty Subset locally as well
		targetService := svc.DeepCopy()
		targetService.Name = rcsw.targetResourceName(svc.Name)
		empty, err := rcsw.isEmptyService(targetService)
		if err != nil {
			rcsw.log.Errorf("could not check service emptiness: %s", err)
			continue
		}
		if empty {
			updatedEndpoints.Subsets = []corev1.EndpointSubset{}
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

// handleCreateOrUpdateEndpoints forwards the call to
// createOrUpdateHeadlessEndpoints when adding/updating exported headless
// endpoints. Otherwise, it handles updates to endpoints to check if they've
// becomed empty/filled since their creation, in order to empty/fill the
// mirrored endpoints as well
func (rcsw *RemoteClusterServiceWatcher) handleCreateOrUpdateEndpoints(
	ctx context.Context,
	exportedEndpoints *corev1.Endpoints,
) error {
	if isHeadlessEndpoints(exportedEndpoints, rcsw.log) {
		if rcsw.headlessServicesEnabled {
			var errors []error
			if err := rcsw.createOrUpdateHeadlessEndpoints(ctx, exportedEndpoints); err != nil {
				errors = append(errors, err)
			}

			if err := rcsw.createOrUpdateGlobalEndpointSlices(ctx, exportedEndpoints); err != nil {
				errors = append(errors, err)
			}

			if len(errors) > 0 {
				return RetryableError{errors}
			}
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
				fmt.Errorf("error retrieving exported service %s/%s: %v", exportedEndpoints.Namespace, exportedEndpoints.Name, err),
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

	_, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(exportedEndpoints.Namespace).Update(ctx, ep, metav1.UpdateOptions{})
	return err
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
		consts.RemoteResourceVersionAnnotation: resourceVersion, // needed to detect real changes
		consts.RemoteServiceFqName:             fmt.Sprintf("%s.%s.%s.svc.%s", endpointHostname, exportedService.Name, exportedService.Namespace, rcsw.link.TargetClusterDomain),
	}

	endpointMirrorLabels := rcsw.getMirroredServiceLabels(nil)
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
	rcsw.endpointMirrorServiceCache.Add(createdService)

	rcsw.log.Infof("Creating a new endpoints object for endpoint mirror service %s", endpointMirrorInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(endpointMirrorService.Namespace).Create(ctx, endpointMirrorEndpoints, metav1.CreateOptions{}); err != nil {
		// If we cannot create an Endpoints object for the Endpoint Mirror
		// service, then delete the Endpoint Mirror service we just created
		rcsw.log.Errorf("Deleting mirror service %s on error: %v", endpointMirrorInfo, err)
		rcsw.localAPIClient.Client.CoreV1().Services(endpointMirrorService.Namespace).Delete(ctx, endpointMirrorName, metav1.DeleteOptions{})
		// and retry
		return createdService, RetryableError{[]error{err}}
	}

	return createdService, nil
}

func isExportedEndpoints(obj interface{}, log *logging.Entry) bool {
	ep, ok := obj.(*corev1.Endpoints)
	if !ok {
		log.Errorf("error processing endpoints object: got %#v, expected *corev1.Endpoints", ep)
		return false
	}

	if _, found := ep.Labels[consts.DefaultExportedServiceSelector]; !found {
		log.Debugf("skipped processing endpoints object %s/%s: missing %s label", ep.Namespace, ep.Name, consts.DefaultExportedServiceSelector)
		return false
	}

	return true
}
