package servicemirror

import (
	"errors"
	"fmt"
	"strconv"
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
		clusterName            string
		clusterDomain          string
		remoteAPIClient        *k8s.API
		localAPIClient         *k8s.API
		stopper                chan struct{}
		log                    *logging.Entry
		eventsQueue            workqueue.RateLimitingInterface
		requeueLimit           int
	}

	// ProbeConfig describes the configured probe on particular gateway (if presents)
	ProbeConfig struct {
		path            string
		port            uint32
		periodInSeconds uint32
	}

	// GatewaySpec contains essential data about the gateway
	GatewaySpec struct {
		gatewayName      string
		gatewayNamespace string
		clusterName      string
		addresses        []corev1.EndpointAddress
		incomingPort     uint32
		resourceVersion  string
		identity         string
		*ProbeConfig
	}

	// RemoteServiceCreated is generated whenever a remote service is created Observing
	// this event means that the service in question is not mirrored atm
	RemoteServiceCreated struct {
		service     *corev1.Service
		gatewayData gatewayMetadata
	}

	// RemoteServiceUpdated is generated when we see something about an already
	// mirrored service change on the remote cluster. In that case we need to
	// reconcile. Most importantly we need to keep track of exposed ports
	// and gateway association changes.
	RemoteServiceUpdated struct {
		localService   *corev1.Service
		localEndpoints *corev1.Endpoints
		remoteUpdate   *corev1.Service
		gatewayData    gatewayMetadata
	}

	// RemoteServiceDeleted when a remote service is going away or it is not
	// considered mirrored anymore
	RemoteServiceDeleted struct {
		Name      string
		Namespace string
	}

	// RemoteGatewayDeleted is observed when a service that is a gateway is deleted
	RemoteGatewayDeleted struct {
		gatewayData gatewayMetadata
	}

	// RemoteGatewayCreated is observed when a gateway service is created on the remote cluster
	RemoteGatewayCreated struct {
		gatewaySpec GatewaySpec
	}

	// RemoteGatewayUpdated happens when a service that is updated.
	RemoteGatewayUpdated struct {
		gatewaySpec      GatewaySpec
		affectedServices []*corev1.Service
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

	gatewayMetadata struct {
		Name      string
		Namespace string
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

// When the gateway is resolved we need to produce a set of endpoint addresses that that
// contain the external IPs that this gateway exposes. Therefore we return the IP addresses
// as well as a single port on which the gateway is accessible.
func (rcsw *RemoteClusterServiceWatcher) resolveGateway(metadata *gatewayMetadata) (*GatewaySpec, error) {
	gateway, err := rcsw.remoteAPIClient.Svc().Lister().Services(metadata.Namespace).Get(metadata.Name)
	if err != nil {
		return nil, err
	}
	return rcsw.extractGatewaySpec(gateway)
}

// NewRemoteClusterServiceWatcher constructs a new cluster watcher
func NewRemoteClusterServiceWatcher(
	serviceMirrorNamespace string,
	localAPI *k8s.API,
	cfg *rest.Config,
	clusterName string,
	requeueLimit int,
	clusterDomain string,
) (*RemoteClusterServiceWatcher, error) {
	remoteAPI, err := k8s.InitializeAPIForConfig(cfg, false, k8s.Svc)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize api for target cluster %s: %s", clusterName, err)
	}
	stopper := make(chan struct{})
	return &RemoteClusterServiceWatcher{
		serviceMirrorNamespace: serviceMirrorNamespace,
		clusterName:            clusterName,
		clusterDomain:          clusterDomain,
		remoteAPIClient:        remoteAPI,
		localAPIClient:         localAPI,
		stopper:                stopper,
		log: logging.WithFields(logging.Fields{
			"cluster":    clusterName,
			"apiAddress": cfg.Host,
		}),
		eventsQueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		requeueLimit: requeueLimit,
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
				if err := rcsw.localAPIClient.Client.CoreV1().Services(srv.Namespace).Delete(srv.Name, &metav1.DeleteOptions{}); err != nil {
					// something went wrong with deletion, we need to retry
					errors = append(errors, err)
				} else {
					rcsw.log.Debugf("Deleted service %s/%s as part of GC process", srv.Namespace, srv.Name)
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
func (rcsw *RemoteClusterServiceWatcher) cleanupMirroredResources() error {
	matchLabels := map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteClusterNameLabel: rcsw.clusterName,
	}

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
		if err := rcsw.localAPIClient.Client.CoreV1().Services(svc.Namespace).Delete(svc.Name, &metav1.DeleteOptions{}); err != nil {
			if kerrors.IsNotFound(err) {
				continue
			}
			errors = append(errors, fmt.Errorf("Could not delete  service %s/%s: %s", svc.Namespace, svc.Name, err))
		} else {
			rcsw.log.Debugf("Deleted service %s/%s", svc.Namespace, svc.Name)
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

	for _, endpt := range endpoints {
		if err := rcsw.localAPIClient.Client.CoreV1().Endpoints(endpt.Namespace).Delete(endpt.Name, &metav1.DeleteOptions{}); err != nil {
			if kerrors.IsNotFound(err) {
				continue
			}
			errors = append(errors, fmt.Errorf("Could not delete  Endpoints %s/%s: %s", endpt.Namespace, endpt.Name, err))
		} else {
			rcsw.log.Debugf("Deleted Endpoints %s/%s", endpt.Namespace, endpt.Name)
		}
	}

	if len(errors) > 0 {
		return RetryableError{errors}
	}
	return nil
}

// Deletes a locally mirrored service as it is not present on the remote cluster anymore
func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceDeleted(ev *RemoteServiceDeleted) error {
	localServiceName := rcsw.mirroredResourceName(ev.Name)
	rcsw.log.Debugf("Deleting mirrored service %s/%s and its corresponding Endpoints", ev.Namespace, localServiceName)
	var errors []error
	if err := rcsw.localAPIClient.Client.CoreV1().Services(ev.Namespace).Delete(localServiceName, &metav1.DeleteOptions{}); err != nil {
		if !kerrors.IsNotFound(err) {
			errors = append(errors, fmt.Errorf("could not delete Service: %s/%s: %s", ev.Namespace, localServiceName, err))
		}
	}

	if err := rcsw.localAPIClient.Client.CoreV1().Endpoints(ev.Namespace).Delete(localServiceName, &metav1.DeleteOptions{}); err != nil {
		if !kerrors.IsNotFound(err) {
			errors = append(errors, fmt.Errorf("could not delete Endpoints: %s/%s: %s", ev.Namespace, localServiceName, err))
		}
	}

	if len(errors) > 0 {
		return RetryableError{errors}
	}

	rcsw.log.Debugf("Successfully deleted Service: %s/%s", ev.Namespace, localServiceName)
	return nil
}

// Updates a locally mirrored service. There might have been some pretty fundamental changes such as
// new gateway being assigned or additional ports exposed. This method takes care of that.
func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceUpdated(ev *RemoteServiceUpdated) error {
	serviceInfo := fmt.Sprintf("%s/%s", ev.remoteUpdate.Namespace, ev.remoteUpdate.Name)
	rcsw.log.Debugf("Updating mirror service %s/%s", ev.localService.Namespace, ev.localService.Name)

	gatewaySpec, err := rcsw.resolveGateway(&ev.gatewayData)
	copiedEndpoints := ev.localEndpoints.DeepCopy()
	if err == nil {
		copiedEndpoints.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewaySpec.addresses,
				Ports:     rcsw.getEndpointsPorts(ev.remoteUpdate, int32(gatewaySpec.incomingPort)),
			},
		}

		if gatewaySpec.identity != "" {
			copiedEndpoints.Annotations[consts.RemoteGatewayIdentity] = gatewaySpec.identity
		} else {
			delete(copiedEndpoints.Annotations, consts.RemoteGatewayIdentity)
		}

	} else {
		rcsw.log.Warnf("Could not resolve gateway for %s: %s, nulling endpoints", serviceInfo, err)
		copiedEndpoints.Subsets = nil
	}
	// we need to set the new name and ns data no matter whether they are valid or not
	copiedEndpoints.Labels[consts.RemoteGatewayNameLabel] = ev.gatewayData.Name
	copiedEndpoints.Labels[consts.RemoteGatewayNsLabel] = ev.gatewayData.Namespace

	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(copiedEndpoints.Namespace).Update(copiedEndpoints); err != nil {
		return RetryableError{[]error{err}}
	}

	ev.localService.Labels = rcsw.getMirroredServiceLabels(&ev.gatewayData)
	ev.localService.Annotations = rcsw.getMirroredServiceAnnotations(ev.remoteUpdate)
	ev.localService.Spec.Ports = remapRemoteServicePorts(ev.remoteUpdate.Spec.Ports)

	if gatewaySpec != nil {
		ev.localService.Annotations[consts.RemoteGatewayResourceVersionAnnotation] = gatewaySpec.resourceVersion
	}

	if _, err := rcsw.localAPIClient.Client.CoreV1().Services(ev.localService.Namespace).Update(ev.localService); err != nil {
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

func (rcsw *RemoteClusterServiceWatcher) handleRemoteServiceCreated(ev *RemoteServiceCreated) error {
	remoteService := ev.service.DeepCopy()
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
			Labels:      rcsw.getMirroredServiceLabels(&ev.gatewayData),
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
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.%s", remoteService.Name, remoteService.Namespace, rcsw.clusterDomain),
			},
		},
	}

	// Now we try to resolve the remote gateway
	gatewaySpec, err := rcsw.resolveGateway(&ev.gatewayData)
	if err == nil {
		// only if we resolve it, we are updating the endpoints addresses and ports
		rcsw.log.Debugf("Resolved gateway [%v:%d] for %s", gatewaySpec.addresses, gatewaySpec.incomingPort, serviceInfo)

		if len(gatewaySpec.addresses) > 0 {
			endpointsToCreate.Subsets = []corev1.EndpointSubset{
				{
					Addresses: gatewaySpec.addresses,
					Ports:     rcsw.getEndpointsPorts(ev.service, int32(gatewaySpec.incomingPort)),
				},
			}
		} else {
			rcsw.log.Warnf("gateway for %s: %s does not have ready addresses, skipping subsets", serviceInfo, err)
		}
		serviceToCreate.Annotations[consts.RemoteGatewayResourceVersionAnnotation] = gatewaySpec.resourceVersion
		if gatewaySpec.identity != "" {
			endpointsToCreate.Annotations[consts.RemoteGatewayIdentity] = gatewaySpec.identity
		}

	} else {
		rcsw.log.Warnf("Could not resolve gateway for %s: %s, skipping subsets", serviceInfo, err)
		endpointsToCreate.Subsets = nil
	}

	rcsw.log.Debugf("Creating a new service mirror for %s", serviceInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Services(remoteService.Namespace).Create(serviceToCreate); err != nil {
		if !kerrors.IsAlreadyExists(err) {
			// we might have created it during earlier attempt, if that is not the case, we retry
			return RetryableError{[]error{err}}
		}
	}

	rcsw.log.Debugf("Creating a new Endpoints for %s", serviceInfo)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(ev.service.Namespace).Create(endpointsToCreate); err != nil {
		// we clean up after ourselves
		rcsw.localAPIClient.Client.CoreV1().Services(ev.service.Namespace).Delete(localServiceName, &metav1.DeleteOptions{})
		// and retry
		return RetryableError{[]error{err}}
	}
	return nil
}

func (rcsw *RemoteClusterServiceWatcher) handleRemoteGatewayDeleted(ev *RemoteGatewayDeleted) error {

	if err := rcsw.localAPIClient.Client.CoreV1().Services(rcsw.serviceMirrorNamespace).Delete(rcsw.mirroredResourceName(ev.gatewayData.Name), &metav1.DeleteOptions{}); err != nil {
		rcsw.log.Errorf("Could not delete gateway mirror %s", err)
	}

	affectedEndpoints, err := rcsw.endpointsForGateway(&ev.gatewayData)
	if err != nil {
		// if we cannot find the endpoints, we can give up
		if kerrors.IsNotFound(err) {
			return err
		}
		// if it is another error, just retry
		return RetryableError{[]error{err}}
	}

	var errors []error
	if len(affectedEndpoints) > 0 {
		rcsw.log.Debugf("Nulling %d endpoints due to gateway [%s/%s] deletion", len(affectedEndpoints), ev.gatewayData.Namespace, ev.gatewayData.Name)
		for _, ep := range affectedEndpoints {
			updated := ep.DeepCopy()
			updated.Subsets = nil
			if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(ep.Namespace).Update(updated); err != nil {
				errors = append(errors, err)
			}
		}
	}
	if len(errors) > 0 {
		// if we have encountered any errors, we can retry the whole operation
		return RetryableError{errors}
	}
	return nil
}

// the logic here creates a mirror service for the gateway. The only port exposed there is the
// probes port. This enables us to discover the gateways probe endpoints through the dst service
// and apply proper identity
func (rcsw *RemoteClusterServiceWatcher) handleRemoteGatewayCreated(event *RemoteGatewayCreated) error {
	localServiceName := rcsw.mirroredResourceName(event.gatewaySpec.gatewayName)
	if event.gatewaySpec.ProbeConfig == nil {
		rcsw.log.Debugf("Skipping creation of gateway mirror as gateway does not specify probe config")
		return nil
	}
	serviceToCreate := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      localServiceName,
			Namespace: rcsw.serviceMirrorNamespace,
			Annotations: map[string]string{
				consts.RemoteGatewayResourceVersionAnnotation: event.gatewaySpec.resourceVersion,
				consts.MirroredGatewayRemoteName:              event.gatewaySpec.gatewayName,
				consts.MirroredGatewayRemoteNameSpace:         event.gatewaySpec.gatewayNamespace,
				consts.MirroredGatewayProbePath:               event.gatewaySpec.ProbeConfig.path,
				consts.MirroredGatewayProbePeriod:             fmt.Sprint(event.gatewaySpec.ProbeConfig.periodInSeconds),
			},
			Labels: map[string]string{
				consts.MirroredResourceLabel:  "true",
				consts.RemoteClusterNameLabel: rcsw.clusterName,
				consts.MirroredGatewayLabel:   "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     consts.ProbePortName,
					Protocol: "TCP",
					Port:     int32(event.gatewaySpec.ProbeConfig.port),
				},
			},
		},
	}

	endpointsToCreate := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      localServiceName,
			Namespace: rcsw.serviceMirrorNamespace,
			Labels: map[string]string{
				consts.MirroredResourceLabel:  "true",
				consts.RemoteClusterNameLabel: rcsw.clusterName,
			},
			Annotations: map[string]string{
				consts.RemoteGatewayIdentity: event.gatewaySpec.identity,
			},
		},
	}

	if len(event.gatewaySpec.addresses) > 0 {
		endpointsToCreate.Subsets = []corev1.EndpointSubset{
			{
				Addresses: event.gatewaySpec.addresses,
				Ports: []corev1.EndpointPort{
					{
						Name:     consts.ProbePortName,
						Protocol: "TCP",
						Port:     int32(event.gatewaySpec.ProbeConfig.port),
					},
				},
			},
		}
	}

	rcsw.log.Debugf("Creating a new gateway mirror Service for %s", localServiceName)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Services(rcsw.serviceMirrorNamespace).Create(serviceToCreate); err != nil {
		if !kerrors.IsAlreadyExists(err) {
			// we might have created it during earlier attempt, if that is not the case, we retry
			return RetryableError{[]error{err}}
		}
	}

	rcsw.log.Debugf("Creating a new gateway mirror Endpoints for %s", localServiceName)
	if _, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(rcsw.serviceMirrorNamespace).Create(endpointsToCreate); err != nil {
		// we clean up after ourselves
		rcsw.localAPIClient.Client.CoreV1().Services(rcsw.serviceMirrorNamespace).Delete(event.gatewaySpec.gatewayName, &metav1.DeleteOptions{})
		// and retry
		return RetryableError{[]error{err}}
	}

	return nil
}

func (rcsw *RemoteClusterServiceWatcher) updateAffectedServices(gatewaySpec GatewaySpec, affectedServices []*corev1.Service) error {
	rcsw.log.Debugf("Updating %d services due to gateway [%s/%s] update", len(affectedServices), gatewaySpec.gatewayNamespace, gatewaySpec.gatewayName)
	var errors []error
	for _, svc := range affectedServices {
		updatedService := svc.DeepCopy()
		if updatedService.Annotations != nil {
			updatedService.Annotations[consts.RemoteGatewayResourceVersionAnnotation] = gatewaySpec.resourceVersion
		}
		endpoints, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(svc.Namespace).Get(svc.Name)
		if err != nil {
			errors = append(errors, fmt.Errorf("Could not get endpoints: %s", err))
			continue
		}

		updatedEndpoints := endpoints.DeepCopy()
		if len(gatewaySpec.addresses) > 0 {
			updatedEndpoints.Subsets = []corev1.EndpointSubset{
				{
					Addresses: gatewaySpec.addresses,
					Ports:     rcsw.getEndpointsPorts(updatedService, int32(gatewaySpec.incomingPort)),
				},
			}
		} else {
			updatedEndpoints.Subsets = nil
		}

		if gatewaySpec.identity != "" {
			updatedEndpoints.Annotations[consts.RemoteGatewayIdentity] = gatewaySpec.identity
		} else {
			delete(updatedEndpoints.Annotations, consts.RemoteGatewayIdentity)
		}

		_, err = rcsw.localAPIClient.Client.CoreV1().Services(updatedService.Namespace).Update(updatedService)
		if err != nil {
			errors = append(errors, err)
			continue
		}

		_, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(updatedService.Namespace).Update(updatedEndpoints)
		if err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return RetryableError{errors}
	}
	return nil
}

func (rcsw *RemoteClusterServiceWatcher) updateGatewayMirrorService(spec *GatewaySpec) error {
	localServiceName := rcsw.mirroredResourceName(spec.gatewayName)
	service, err := rcsw.localAPIClient.Svc().Lister().Services(rcsw.serviceMirrorNamespace).Get(localServiceName)
	if err != nil {
		return err
	}

	if service.Annotations != nil && service.Annotations[consts.RemoteGatewayResourceVersionAnnotation] != spec.resourceVersion {
		updatedService := service.DeepCopy()
		if updatedService.Annotations != nil {
			updatedService.Annotations[consts.RemoteGatewayResourceVersionAnnotation] = spec.resourceVersion
			updatedService.Annotations[consts.MirroredGatewayProbePath] = spec.ProbeConfig.path
			updatedService.Annotations[consts.MirroredGatewayProbePeriod] = fmt.Sprint(spec.ProbeConfig.periodInSeconds)
		}

		endpoints, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(rcsw.serviceMirrorNamespace).Get(localServiceName)
		if err != nil {
			return err
		}

		updatedEndpoints := endpoints.DeepCopy()
		if spec.addresses == nil {
			updatedEndpoints.Subsets = nil
		} else {
			updatedEndpoints.Subsets = []corev1.EndpointSubset{
				{
					Addresses: spec.addresses,
					Ports: []corev1.EndpointPort{
						{
							Name:     consts.ProbePortName,
							Protocol: "TCP",
							Port:     int32(spec.ProbeConfig.port),
						},
					},
				},
			}
		}

		endpoints.Annotations[consts.RemoteGatewayIdentity] = spec.identity

		_, err = rcsw.localAPIClient.Client.CoreV1().Services(rcsw.serviceMirrorNamespace).Update(updatedService)
		if err != nil {
			return err
		}

		_, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(rcsw.serviceMirrorNamespace).Update(updatedEndpoints)
		if err != nil {
			return err
		}
		rcsw.log.Debugf("%s gateway mirror updated", localServiceName)
	}

	return nil
}

func (rcsw *RemoteClusterServiceWatcher) handleRemoteGatewayUpdated(ev *RemoteGatewayUpdated) error {
	if err := rcsw.updateAffectedServices(ev.gatewaySpec, ev.affectedServices); err != nil {
		return err
	}

	if err := rcsw.updateGatewayMirrorService(&ev.gatewaySpec); err != nil {
		return err
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

func isGateway(annotations map[string]string) bool {
	if annotations != nil {
		_, hasAnnotation := annotations[consts.MulticlusterGatewayAnnotation]
		return hasAnnotation
	}
	return false
}

func isMirroredService(annotations map[string]string) bool {
	if annotations != nil {
		_, hasGtwName := annotations[consts.GatewayNameAnnotation]
		_, hasGtwNs := annotations[consts.GatewayNsAnnotation]
		return hasGtwName && hasGtwNs
	}
	return false
}

// this method is common to both CREATE and UPDATE because if we have been
// offline for some time due to a crash a CREATE for a service that we have
// observed before is simply a case of UPDATE
func (rcsw *RemoteClusterServiceWatcher) createOrUpdateService(service *corev1.Service) error {
	localName := rcsw.mirroredResourceName(service.Name)

	if isGateway(service.Annotations) {
		gatewaySpec, err := rcsw.extractGatewaySpec(service)
		if err != nil {
			return RetryableError{[]error{err}}
		}

		_, err = rcsw.localAPIClient.Svc().Lister().Services(rcsw.serviceMirrorNamespace).Get(localName)
		if err != nil {
			if kerrors.IsNotFound(err) {
				rcsw.eventsQueue.Add(&RemoteGatewayCreated{
					gatewaySpec: *gatewaySpec,
				})
				return nil
			}
			return RetryableError{[]error{err}}
		}

		affectedServices, err := rcsw.affectedMirroredServicesForGatewayUpdate(&gatewayMetadata{
			Name:      service.Name,
			Namespace: service.Namespace,
		}, service.ResourceVersion)
		if err != nil {
			return RetryableError{[]error{err}}
		}

		rcsw.eventsQueue.Add(&RemoteGatewayUpdated{
			affectedServices: affectedServices,
			gatewaySpec:      *gatewaySpec,
		})
		return nil

	} else if isMirroredService(service.Annotations) {
		gatewayData := getGatewayMetadata(service.Annotations)
		if gatewayData == nil {
			return fmt.Errorf("got service in invalid state, no gateway metadata %s", service)
		}
		localService, err := rcsw.localAPIClient.Svc().Lister().Services(service.Namespace).Get(localName)
		if err != nil {
			if kerrors.IsNotFound(err) {
				rcsw.eventsQueue.Add(&RemoteServiceCreated{
					service:     service,
					gatewayData: *gatewayData,
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
					gatewayData:    *gatewayData,
				})
				return nil
			}
			return RetryableError{[]error{err}}
		}
		return nil
	} else {
		localSvc, err := rcsw.localAPIClient.Svc().Lister().Services(service.Namespace).Get(localName)
		if err == nil {
			if localSvc.Labels != nil {
				_, isMirroredRes := localSvc.Labels[consts.MirroredResourceLabel]
				clusterName := localSvc.Labels[consts.RemoteClusterNameLabel]
				if isMirroredRes && (clusterName == rcsw.clusterName) {
					rcsw.eventsQueue.Add(&RemoteServiceDeleted{
						Name:      service.Name,
						Namespace: service.Namespace,
					})
				}
			}
		}
		return nil
	}
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
		consts.RemoteClusterNameLabel: rcsw.clusterName,
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
		consts.RemoteClusterNameLabel: rcsw.clusterName,
	}

	endpoints, err := rcsw.localAPIClient.Endpoint().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		return nil, err
	}
	return endpoints, nil
}

func (rcsw *RemoteClusterServiceWatcher) handleOnDelete(service *corev1.Service) {
	if isMirroredService(service.Annotations) {
		rcsw.eventsQueue.Add(&RemoteServiceDeleted{
			Name:      service.Name,
			Namespace: service.Namespace,
		})
	} else if isGateway(service.Annotations) {
		rcsw.eventsQueue.Add(&RemoteGatewayDeleted{
			gatewayData: gatewayMetadata{
				Name:      service.Name,
				Namespace: service.Namespace,
			}})
	} else {
		rcsw.log.Debugf("Skipping OnDelete for service %s", service)
	}
}

func (rcsw *RemoteClusterServiceWatcher) processNextEvent() (bool, interface{}, error) {
	event, done := rcsw.eventsQueue.Get()
	if event != nil {
		rcsw.log.Debugf("Received: %s", event)
	} else {
		if done {
			rcsw.log.Debug("Received: Stop")
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
		err = rcsw.handleRemoteServiceCreated(ev)
	case *RemoteServiceUpdated:
		err = rcsw.handleRemoteServiceUpdated(ev)
	case *RemoteServiceDeleted:
		err = rcsw.handleRemoteServiceDeleted(ev)
	case *RemoteGatewayUpdated:
		err = rcsw.handleRemoteGatewayUpdated(ev)
	case *RemoteGatewayDeleted:
		err = rcsw.handleRemoteGatewayDeleted(ev)
	case *RemoteGatewayCreated:
		err = rcsw.handleRemoteGatewayCreated(ev)
	case *ClusterUnregistered:
		err = rcsw.cleanupMirroredResources()
	case *OprhanedServicesGcTriggered:
		err = rcsw.cleanupOrphanedServices()
	default:
		if ev != nil || !done { // we get a nil in case we are shutting down...
			rcsw.log.Warnf("Received unknown event: %v", ev)
		}
	}

	return done, event, err

}

// the main processing loop in which we handle more domain specific events
// and deal with retries
func (rcsw *RemoteClusterServiceWatcher) processEvents() {
	for {
		done, event, err := rcsw.processNextEvent()
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
					if (rcsw.eventsQueue.NumRequeues(event) < rcsw.requeueLimit) && !done {
						rcsw.log.Errorf("Error processing %s (will retry): %s", event, e)
						rcsw.eventsQueue.Add(event)
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
			rcsw.log.Debug("Shutting down events processor")
			return
		}
	}
}

// Start starts watching the remote cluster
func (rcsw *RemoteClusterServiceWatcher) Start() error {
	rcsw.remoteAPIClient.Sync(rcsw.stopper)
	rcsw.eventsQueue.Add(&OprhanedServicesGcTriggered{})
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
	go rcsw.processEvents()
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

func extractPort(port []corev1.ServicePort, portName string) (uint32, error) {
	for _, p := range port {
		if p.Name == portName {
			return uint32(p.Port), nil
		}
	}
	return 0, fmt.Errorf("could not find port with name %s", portName)
}

func extractProbeConfig(gateway *corev1.Service) (*ProbeConfig, error) {
	probePath := gateway.Annotations[consts.GatewayProbePath]

	probePort, err := extractPort(gateway.Spec.Ports, consts.ProbePortName)

	if err != nil {
		return nil, err
	}

	probePeriod, err := strconv.ParseUint(gateway.Annotations[consts.GatewayProbePeriod], 10, 32)
	if err != nil {
		return nil, err
	}

	if probePath == "" {
		return nil, errors.New("probe path is empty")
	}

	return &ProbeConfig{
		path:            probePath,
		port:            probePort,
		periodInSeconds: uint32(probePeriod),
	}, nil
}

func (rcsw *RemoteClusterServiceWatcher) extractGatewaySpec(gateway *corev1.Service) (*GatewaySpec, error) {
	incomingPort, err := extractPort(gateway.Spec.Ports, consts.GatewayPortName)

	if err != nil {
		return nil, err
	}

	var gatewayEndpoints []corev1.EndpointAddress
	for _, ingress := range gateway.Status.LoadBalancer.Ingress {
		gatewayEndpoints = append(gatewayEndpoints, corev1.EndpointAddress{
			IP:       ingress.IP,
			Hostname: ingress.Hostname,
		})
	}

	gatewayIdentity := gateway.Annotations[consts.GatewayIdentity]
	probeConfig, err := extractProbeConfig(gateway)
	if err != nil {
		return nil, fmt.Errorf("could not parse probe config for gateway: %s/%s: %s", gateway.Namespace, gateway.Name, err)
	}

	return &GatewaySpec{
		clusterName:      rcsw.clusterName,
		gatewayName:      gateway.Name,
		gatewayNamespace: gateway.Namespace,
		addresses:        gatewayEndpoints,
		incomingPort:     incomingPort,
		resourceVersion:  gateway.ResourceVersion,
		identity:         gatewayIdentity,
		ProbeConfig:      probeConfig,
	}, nil
}
