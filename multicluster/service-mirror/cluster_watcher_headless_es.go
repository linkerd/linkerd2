package servicemirror

import (
	"context"
	"fmt"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// handleCreateOrUpdateEndpointSlice processes EndpointSlice objects for exported
// services. When an EndpointSlice object is created or updated in the remote
// cluster, it will be processed here in order to reconcile the local cluster
// state with the remote cluster state.
func (rcsw *RemoteClusterServiceWatcher) handleCreateOrUpdateEndpointSlice(
	ctx context.Context,
	exportedEndpointSlice *discoveryv1.EndpointSlice,
) error {
	ns, svcName, err := getEndpointSliceServiceID(exportedEndpointSlice)
	if err != nil {
		return err
	}

	exportedService, err := rcsw.remoteAPIClient.Svc().Lister().Services(ns).Get(svcName)
	if err != nil {
		rcsw.log.Debugf("failed to retrieve exported service %s/%s when updating its mirror endpoints: %v", ns, svcName, err)
		return fmt.Errorf("error retrieving exported service %s/%s: %w", ns, svcName, err)
	}

	if isHeadlessEndpointSlice(exportedEndpointSlice, rcsw.log) {
		if rcsw.headlessServicesEnabled {
			return rcsw.createOrUpdateHeadlessEndpointsFromSlice(ctx, exportedService, exportedEndpointSlice)
		}
		return nil
	}

	// For non-headless services, handle emptiness check
	return rcsw.handleEndpointSliceEmptiness(ctx, exportedService, exportedEndpointSlice)
}

// handleEndpointSliceEmptiness handles updates to EndpointSlices to check if they've
// become empty/filled since their creation, in order to empty/fill the
// mirrored endpoints as well
func (rcsw *RemoteClusterServiceWatcher) handleEndpointSliceEmptiness(
	ctx context.Context,
	exportedService *corev1.Service,
	exportedEndpointSlice *discoveryv1.EndpointSlice,
) error {
	localServiceName := rcsw.mirrorServiceName(exportedService.Name)
	ep, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(exportedService.Namespace).Get(localServiceName)
	if err != nil {
		return RetryableError{[]error{err}}
	}

	// Check if the service is empty using all EndpointSlices
	serviceEmpty, err := rcsw.isEmptyServiceES(exportedService)
	if err != nil {
		return RetryableError{[]error{err}}
	}

	localEmpty := rcsw.isEmptyEndpoints(ep)

	if (localEmpty && serviceEmpty) || (!localEmpty && !serviceEmpty) {
		return nil
	}

	rcsw.log.Infof("Updating subsets for mirror endpoint %s/%s", exportedService.Namespace, exportedService.Name)
	if serviceEmpty {
		ep.Subsets = []corev1.EndpointSubset{}
	} else {
		gatewayAddresses, err := rcsw.resolveGatewayAddress()
		if err != nil {
			return err
		}
		ports, err := rcsw.getEndpointsPorts(exportedService)
		if err != nil {
			return err
		}
		ep.Subsets = []corev1.EndpointSubset{
			{
				Addresses: gatewayAddresses,
				Ports:     ports,
			},
		}
	}
	return rcsw.updateMirrorEndpoints(ctx, ep)
}

// createOrUpdateHeadlessEndpointsFromSlice processes EndpointSlice objects for
// exported headless services. This is the EndpointSlice equivalent of
// createOrUpdateHeadlessEndpoints.
func (rcsw *RemoteClusterServiceWatcher) createOrUpdateHeadlessEndpointsFromSlice(
	ctx context.Context,
	exportedService *corev1.Service,
	exportedEndpointSlice *discoveryv1.EndpointSlice,
) error {
	// Check whether the endpoints should be processed for a headless exported
	// service. If the exported service does not have any ports exposed, then
	// neither will its corresponding endpoint mirrors, it should not be created
	// as a headless mirror.
	if len(exportedService.Spec.Ports) == 0 {
		rcsw.recorder.Event(exportedService, corev1.EventTypeNormal, eventTypeSkipped, "Skipped mirroring service: object spec has no exposed ports")
		rcsw.log.Infof("Skipped creating headless mirror for %s/%s: service object spec has no exposed ports", exportedService.Namespace, exportedService.Name)
		return nil
	}

	mirrorServiceName := rcsw.mirrorServiceName(exportedService.Name)
	mirrorService, err := rcsw.localAPIClient.Svc().Lister().Services(exportedService.Namespace).Get(mirrorServiceName)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		// If the mirror service does not exist, create it, either as clusterIP
		// or as headless.
		mirrorService, err = rcsw.createRemoteHeadlessServiceFromSlice(ctx, exportedService, exportedEndpointSlice)
		if err != nil {
			return err
		}
	}

	headlessMirrorEpName := rcsw.mirrorServiceName(exportedService.Name)
	headlessMirrorEndpoints, err := rcsw.localAPIClient.Endpoint().Lister().Endpoints(exportedService.Namespace).Get(headlessMirrorEpName)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		if mirrorService.Spec.ClusterIP != corev1.ClusterIPNone {
			return rcsw.createGatewayEndpoints(ctx, exportedService)
		}

		// Create endpoint mirrors for headless mirror
		if err := rcsw.createHeadlessMirrorEndpointsFromSlice(ctx, exportedService, exportedEndpointSlice); err != nil {
			rcsw.log.Debugf("failed to create headless mirrors for EndpointSlice %s/%s: %v", exportedEndpointSlice.Namespace, exportedEndpointSlice.Name, err)
			return err
		}

		return nil
	}

	// Past this point, we do not want to process a mirror service that is not
	// headless. We want to process only endpoints for headless mirrors.
	if mirrorService.Spec.ClusterIP != corev1.ClusterIPNone {
		return nil
	}

	mirrorEndpoints := headlessMirrorEndpoints.DeepCopy()
	endpointMirrors := make(map[string]struct{})
	newSubsets := make([]corev1.EndpointSubset, 0)

	// Get all EndpointSlices for this service to aggregate
	matchLabels := map[string]string{discoveryv1.LabelServiceName: exportedService.Name}
	selector := labels.Set(matchLabels).AsSelector()
	allSlices, err := rcsw.remoteAPIClient.ES().Lister().EndpointSlices(exportedService.Namespace).List(selector)
	if err != nil {
		return RetryableError{[]error{err}}
	}

	// Build ports from the service
	ports := make([]corev1.EndpointPort, 0, len(exportedService.Spec.Ports))
	for _, port := range exportedService.Spec.Ports {
		ports = append(ports, corev1.EndpointPort{
			Name:     port.Name,
			Protocol: port.Protocol,
			Port:     port.Port,
		})
	}

	// Process all EndpointSlices for this service
	newAddresses := make([]corev1.EndpointAddress, 0)
	for _, slice := range allSlices {
		for _, endpoint := range slice.Endpoints {
			hostname := ""
			if endpoint.Hostname != nil {
				hostname = *endpoint.Hostname
			}
			if hostname == "" {
				continue
			}

			// Skip endpoints that are not ready
			if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
				continue
			}

			endpointMirrorName := rcsw.mirrorServiceName(hostname)
			endpointMirrorService, err := rcsw.localAPIClient.Svc().Lister().Services(exportedService.Namespace).Get(endpointMirrorName)
			if err != nil {
				if !kerrors.IsNotFound(err) {
					return err
				}
				// If the error is 'NotFound' then the Endpoint Mirror service
				// does not exist, so create it.
				endpointMirrorService, err = rcsw.createEndpointMirrorService(ctx, hostname, slice.ResourceVersion, endpointMirrorName, exportedService)
				if err != nil {
					return err
				}
			}

			endpointMirrors[endpointMirrorName] = struct{}{}
			newAddresses = append(newAddresses, corev1.EndpointAddress{
				Hostname: hostname,
				IP:       endpointMirrorService.Spec.ClusterIP,
			})
		}
	}

	if len(newAddresses) > 0 {
		newSubsets = append(newSubsets, corev1.EndpointSubset{
			Addresses: newAddresses,
			Ports:     ports,
		})
	}

	headlessMirrorName := rcsw.mirrorServiceName(exportedService.Name)
	matchLabels = map[string]string{
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
				errors = append(errors, fmt.Errorf("error deleting Endpoint Mirror service %s/%s: %w", service.Namespace, service.Name, err))
			}
		}
	}

	if len(errors) > 0 {
		return RetryableError{errors}
	}

	// Update endpoints
	mirrorEndpoints.Subsets = newSubsets
	err = rcsw.updateMirrorEndpoints(ctx, mirrorEndpoints)
	if err != nil {
		return RetryableError{[]error{err}}
	}

	return nil
}

// createRemoteHeadlessServiceFromSlice creates a mirror service for an exported
// headless service using EndpointSlice data. Whether the mirror will be created
// as a headless or clusterIP service depends on the EndpointSlice.
func (rcsw *RemoteClusterServiceWatcher) createRemoteHeadlessServiceFromSlice(
	ctx context.Context,
	exportedService *corev1.Service,
	exportedEndpointSlice *discoveryv1.EndpointSlice,
) (*corev1.Service, error) {
	// If we don't have any endpoints to process then avoid creating the service.
	if len(exportedEndpointSlice.Endpoints) == 0 {
		return &corev1.Service{}, nil
	}

	remoteService := exportedService.DeepCopy()
	serviceInfo := fmt.Sprintf("%s/%s", remoteService.Namespace, remoteService.Name)
	localServiceName := rcsw.mirrorServiceName(remoteService.Name)

	if rcsw.namespaceCreationEnabled {
		if err := rcsw.mirrorNamespaceIfNecessary(ctx, remoteService.Namespace); err != nil {
			return &corev1.Service{}, err
		}
	} else {
		// Ensure the namespace exists, and skip mirroring if it doesn't
		if _, err := rcsw.localAPIClient.NS().Lister().Get(remoteService.Namespace); err != nil {
			if kerrors.IsNotFound(err) {
				rcsw.log.Warnf("Skipping mirroring of service %s: namespace %s does not exist", serviceInfo, remoteService.Namespace)
				return &corev1.Service{}, nil
			}
			// something else went wrong, so we can just retry
			return nil, RetryableError{[]error{err}}
		}
	}

	serviceToCreate := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        localServiceName,
			Namespace:   remoteService.Namespace,
			Annotations: rcsw.getMirrorServiceAnnotations(remoteService),
			Labels:      rcsw.getMirrorServiceLabels(remoteService),
		},
		Spec: corev1.ServiceSpec{
			Ports: remapRemoteServicePorts(remoteService.Spec.Ports),
		},
	}

	if shouldExportAsHeadlessServiceFromSlice(exportedEndpointSlice, rcsw.log) {
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

// createHeadlessMirrorEndpointsFromSlice creates an endpoints object for a
// Headless Mirror service using EndpointSlice data.
func (rcsw *RemoteClusterServiceWatcher) createHeadlessMirrorEndpointsFromSlice(
	ctx context.Context,
	exportedService *corev1.Service,
	exportedEndpointSlice *discoveryv1.EndpointSlice,
) error {
	exportedServiceInfo := fmt.Sprintf("%s/%s", exportedService.Namespace, exportedService.Name)

	// Get all EndpointSlices for this service to aggregate
	matchLabels := map[string]string{discoveryv1.LabelServiceName: exportedService.Name}
	selector := labels.Set(matchLabels).AsSelector()
	allSlices, err := rcsw.remoteAPIClient.ES().Lister().EndpointSlices(exportedService.Namespace).List(selector)
	if err != nil {
		return RetryableError{[]error{err}}
	}

	endpointsHostnames := make(map[string]struct{})
	newAddresses := make([]corev1.EndpointAddress, 0)

	for _, slice := range allSlices {
		for _, endpoint := range slice.Endpoints {
			hostname := ""
			if endpoint.Hostname != nil {
				hostname = *endpoint.Hostname
			}
			if hostname == "" {
				continue
			}

			// Skip endpoints that are not ready
			if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
				continue
			}

			endpointMirrorName := rcsw.mirrorServiceName(hostname)
			createdService, err := rcsw.createEndpointMirrorService(ctx, hostname, slice.ResourceVersion, endpointMirrorName, exportedService)
			if err != nil {
				rcsw.log.Errorf("error creating endpoint mirror service %s/%s for exported headless service %s: %v", endpointMirrorName, exportedService.Namespace, exportedServiceInfo, err)
				continue
			}

			endpointsHostnames[hostname] = struct{}{}
			// Use the hostname as the address hostname (for DNS)
			targetRefName := hostname
			if endpoint.TargetRef != nil {
				targetRefName = endpoint.TargetRef.Name
			}
			newAddresses = append(newAddresses, corev1.EndpointAddress{
				Hostname: targetRefName,
				IP:       createdService.Spec.ClusterIP,
			})
		}
	}

	// Build ports from the service
	ports := make([]corev1.EndpointPort, 0, len(exportedService.Spec.Ports))
	for _, port := range exportedService.Spec.Ports {
		ports = append(ports, corev1.EndpointPort{
			Name:     port.Name,
			Protocol: port.Protocol,
			Port:     port.Port,
		})
	}

	subsetsToCreate := make([]corev1.EndpointSubset, 0)
	if len(newAddresses) > 0 {
		subsetsToCreate = append(subsetsToCreate, corev1.EndpointSubset{
			Addresses: newAddresses,
			Ports:     ports,
		})
	}

	headlessMirrorServiceName := rcsw.mirrorServiceName(exportedService.Name)
	headlessMirrorEndpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headlessMirrorServiceName,
			Namespace: exportedService.Namespace,
			Labels:    rcsw.getMirrorEndpointLabels(exportedService),
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.%s", exportedService.Name, exportedService.Namespace, rcsw.link.Spec.TargetClusterDomain),
			},
		},
		Subsets: subsetsToCreate,
	}

	if rcsw.link.Spec.GatewayIdentity != "" {
		headlessMirrorEndpoints.Annotations[consts.RemoteGatewayIdentity] = rcsw.link.Spec.GatewayIdentity
	}

	rcsw.log.Infof("Creating a new headless mirror endpoints object for headless mirror %s/%s", headlessMirrorServiceName, exportedService.Namespace)
	// The addresses for the headless mirror service point to the Cluster IPs
	// of auxiliary services that are tied to gateway liveness. Therefore,
	// these addresses should always be considered ready.
	_, err = rcsw.localAPIClient.Client.CoreV1().Endpoints(exportedService.Namespace).Create(ctx, headlessMirrorEndpoints, metav1.CreateOptions{})
	if err != nil {
		if svcErr := rcsw.localAPIClient.Client.CoreV1().Services(exportedService.Namespace).Delete(ctx, headlessMirrorServiceName, metav1.DeleteOptions{}); svcErr != nil {
			rcsw.log.Errorf("failed to delete Service %s after Endpoints creation failed: %s", headlessMirrorServiceName, svcErr)
		}
		return RetryableError{[]error{err}}
	}

	return nil
}

// shouldExportAsHeadlessServiceFromSlice checks if an exported service should be
// mirrored as a headless service or as a clusterIP service, based on its
// EndpointSlice. For an exported service to be a headless mirror, it needs
// to have at least one named address (hostname) in its EndpointSlice.
func shouldExportAsHeadlessServiceFromSlice(es *discoveryv1.EndpointSlice, log *logging.Entry) bool {
	for _, endpoint := range es.Endpoints {
		if endpoint.Hostname != nil && *endpoint.Hostname != "" {
			return true
		}
	}
	ns, svcName, _ := getEndpointSliceServiceID(es)
	log.Infof("Service %s/%s should not be exported as headless: no named addresses in its EndpointSlice", ns, svcName)
	return false
}
