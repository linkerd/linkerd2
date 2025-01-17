package servicemirror

import (
	"context"
	"fmt"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

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
		return fmt.Errorf("error retrieving exported service %s/%s: %w", exportedEndpoints.Namespace, exportedEndpoints.Name, err)
	}

	// Check whether the endpoints should be processed for a headless exported
	// service. If the exported service does not have any ports exposed, then
	// neither will its corresponding endpoint mirrors, it should not be created
	// as a headless mirror. If the service does not have any named addresses in
	// its Endpoints object, then the endpoints should not be processed.
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
		mirrorService, err = rcsw.createRemoteHeadlessService(ctx, exportedService, exportedEndpoints)
		if err != nil {
			return err
		}
	}

	headlessMirrorEpName := rcsw.mirrorServiceName(exportedEndpoints.Name)
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

			endpointMirrorName := rcsw.mirrorServiceName(address.Hostname)
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

	headlessMirrorName := rcsw.mirrorServiceName(exportedService.Name)
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

			endpointMirrorName := rcsw.mirrorServiceName(addr.Hostname)
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
	_, err := rcsw.localAPIClient.Client.CoreV1().Endpoints(exportedService.Namespace).Create(ctx, headlessMirrorEndpoints, metav1.CreateOptions{})
	if err != nil {
		if svcErr := rcsw.localAPIClient.Client.CoreV1().Services(exportedService.Namespace).Delete(ctx, headlessMirrorServiceName, metav1.DeleteOptions{}); svcErr != nil {
			rcsw.log.Errorf("failed to delete Service %s after Endpoints creation failed: %s", headlessMirrorServiceName, svcErr)
		}
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
		consts.RemoteResourceVersionAnnotation: resourceVersion, // needed to detect real changes
		consts.RemoteServiceFqName:             fmt.Sprintf("%s.%s.%s.svc.%s", endpointHostname, exportedService.Name, exportedService.Namespace, rcsw.link.Spec.TargetClusterDomain),
	}

	endpointMirrorLabels := rcsw.getMirrorServiceLabels(exportedService)
	mirrorServiceName := rcsw.mirrorServiceName(exportedService.Name)
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
	ports, err := rcsw.getEndpointsPorts(exportedService)
	if err != nil {
		return nil, err
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
				Ports:     ports,
			},
		},
	}

	if rcsw.link.Spec.GatewayIdentity != "" {
		endpointMirrorEndpoints.Annotations[consts.RemoteGatewayIdentity] = rcsw.link.Spec.GatewayIdentity
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
	err = rcsw.createMirrorEndpoints(ctx, endpointMirrorEndpoints)
	if err != nil {
		if svcErr := rcsw.localAPIClient.Client.CoreV1().Services(endpointMirrorService.Namespace).Delete(ctx, endpointMirrorName, metav1.DeleteOptions{}); svcErr != nil {
			rcsw.log.Errorf("Failed to delete service %s after endpoints creation failed: %s", endpointMirrorName, svcErr)
		}
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

// isHeadlessEndpoints checks if an endpoints object belongs to a
// headless service.
func isHeadlessEndpoints(ep *corev1.Endpoints, log *logging.Entry) bool {
	if _, found := ep.Labels[corev1.IsHeadlessService]; !found {
		// Not an Endpoints object for a headless service? Then we likely don't want
		// to update anything.
		log.Debugf("skipped processing endpoints object %s/%s: missing %s label", ep.Namespace, ep.Name, corev1.IsHeadlessService)
		return false
	}

	return true
}
