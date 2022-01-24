package servicemirror

import (
	"context"
	"fmt"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"

	// Target stable (1.21?) or beta (1.17+? until 1.25?)
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TODO: Update documentation
// createOrUpdateGlobalEndpointSlices processes endpoints objects for exported
// headless services. When an endpoints object is created or updated in the
// remote cluster, it will be processed here in order to reconcile the local
// cluster state with the remote cluster state.
//
// createOrUpdateGlobalEndpointSlices is also responsible for creating the service
// mirror in the source cluster. In order for an exported headless service to be
// mirrored as headless, it must have at least one port defined and at least one
// named address in its endpoints object (e.g a deployment would not work since
// pods may not have arbitrary hostnames). As such, when an endpoints object is
// first processed, if there is no mirror service, we create one, by looking at
// the endpoints object itself. If the exported service is deemed to be valid
// for global mirroring, then the function will create the global mirror and
// then create an endpoints object for it in the source cluster. If it is not
// valid, the exported service will be mirrored as clusterIP and its endpoints
// will point to the gateway.
//
// When creating endpoints for a global mirror, we also create an endpoint
// mirror (clusterIP) service for each of the endpoints' named addresses. If the
// global mirror exists and has an endpoints object, we simply update by
// either creating or deleting endpoint mirror services.
func (rcsw *RemoteClusterServiceWatcher) createOrUpdateGlobalEndpointSlices(ctx context.Context, exportedEndpoints *corev1.Endpoints) error {
	exportedService, err := rcsw.remoteAPIClient.Svc().Lister().Services(exportedEndpoints.Namespace).Get(exportedEndpoints.Name)
	if err != nil {
		rcsw.log.Debugf("failed to retrieve exported service %s/%s when updating its global mirror endpoints: %v", exportedEndpoints.Namespace, exportedEndpoints.Name, err)
		return fmt.Errorf("error retrieving exported service %s/%s: %v", exportedEndpoints.Namespace, exportedEndpoints.Name, err)
	}

	globalName, found := exportedService.Labels[consts.GlobalServiceNameLabel]
	if !found {
		return nil
	}

	// Past this point, we do not want to process a mirror service that is not
	// headless. We want to process only endpoints for global mirrors; before
	// this point it would have been possible to have a clusterIP mirror, since
	// we are creating the mirror service in the scope of the function.
	if exportedService.Spec.ClusterIP != corev1.ClusterIPNone {
		rcsw.log.Warnf("Invalid configuration for creating a global mirror of service %s/%s, global mirrors must be headless", exportedService.Namespace, exportedService.Name)
		return nil
	}

	// Check whether the endpoints should be processed for a headless exported
	// service. If the exported service does not have any ports exposed, then
	// neither will its corresponding endpoint mirrors, it should not be created
	// as a global mirror. If the service does not have any named addresses in
	// its Endpoints object, then the endpoints should not be processed.
	if len(exportedService.Spec.Ports) == 0 {
		rcsw.recorder.Event(exportedService, v1.EventTypeNormal, eventTypeSkipped, "Skipped mirroring service: object spec has no exposed ports")
		rcsw.log.Infof("Skipped creating global mirror for %s/%s: service object spec has no exposed ports", exportedService.Namespace, exportedService.Name)
		return nil
	}

	mirrorServiceName := globalName
	_, err = rcsw.localAPIClient.Svc().Lister().Services(exportedService.Namespace).Get(mirrorServiceName)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		// If the global mirror doesn't exist, create it. This function promises to handle if we are raced.
		_, err = rcsw.createRemoteGlobalService(ctx, globalName, exportedService, exportedEndpoints)
		if err != nil {
			return err
		}
	}

	endpointSliceName := rcsw.globalEndpointSliceName(globalName, exportedService)
	endpointSlice, err := rcsw.localAPIClient.ES().Lister().EndpointSlices(exportedEndpoints.Namespace).Get(endpointSliceName)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		err := rcsw.createGlobalMirrorEndpointSlices(ctx, globalName, exportedService, exportedEndpoints)
		if err != nil {
			rcsw.log.Debugf("failed to create global mirrors for endpoints %s/%s: %v", exportedEndpoints.Namespace, exportedEndpoints.Name, err)
			return err
		}

		return nil
	}

	mirrorEndpoints := endpointSlice.DeepCopy()
	endpoints, nil := rcsw.computeEndpointsForEndpointSlice(ctx, exportedService, exportedEndpoints)
	if err != nil {
		return err
	}
	mirrorEndpoints.Endpoints = endpoints
	_, err = rcsw.localAPIClient.Client.DiscoveryV1beta1().EndpointSlices(mirrorEndpoints.Namespace).Update(ctx, mirrorEndpoints, metav1.UpdateOptions{})
	if err != nil {
		return err
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
func (rcsw *RemoteClusterServiceWatcher) createRemoteGlobalService(ctx context.Context, globalName string, exportedService *corev1.Service, exportedEndpoints *corev1.Endpoints) (*corev1.Service, error) {
	// If we don't have any subsets to process then avoid creating the service.
	// We need at least one address to be make a decision (whether we should
	// create as clusterIP or headless), rely on the endpoints being eventually
	// consistent, will likely receive an update with subsets.
	if len(exportedEndpoints.Subsets) == 0 {
		return &corev1.Service{}, nil
	}

	remoteService := exportedService.DeepCopy()
	serviceInfo := fmt.Sprintf("%s/%s", remoteService.Namespace, remoteService.Name)
	localServiceName := globalName

	// Ensure the namespace exists, and skip mirroring if it doesn't
	if _, err := rcsw.localAPIClient.NS().Lister().Get(remoteService.Namespace); err != nil {
		if kerrors.IsNotFound(err) {
			rcsw.log.Warnf("Skipping mirroring of global service %s: namespace %s does not exist", serviceInfo, remoteService.Namespace)
			return &corev1.Service{}, nil
		}
		// something else went wrong, so we can just retry
		return nil, RetryableError{[]error{err}}
	}

	serviceToCreate := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        localServiceName,
			Namespace:   remoteService.Namespace,
			Annotations: rcsw.getMirroredServiceAnnotations(remoteService),
			Labels:      rcsw.getMirroredServiceLabels(remoteService),
		},
		Spec: corev1.ServiceSpec{
			Ports:     remapRemoteServicePorts(remoteService.Spec.Ports),
			ClusterIP: corev1.ClusterIPNone,
		},
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

// createGlobalMirrorEndpointSlices creates an endpoints object for a Headless
// Mirror service. The endpoints object will contain the same subsets and hosts
// as the endpoints object of the exported headless service. Each host in the
// Global mirror's endpoints object will point to an Endpoint Mirror service.
func (rcsw *RemoteClusterServiceWatcher) createGlobalMirrorEndpointSlices(ctx context.Context, globalName string, exportedService *corev1.Service, exportedEndpoints *corev1.Endpoints) error {
	endpointsToCreate, err := rcsw.computeEndpointsForEndpointSlice(ctx, exportedService, exportedEndpoints)
	if err != nil {
		return err
	}

	endpointSliceName := rcsw.globalEndpointSliceName(globalName, exportedService)
	headlessMirrorEndpoints := &discoveryv1beta1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      endpointSliceName,
			Namespace: exportedService.Namespace,
			Labels: map[string]string{
				"kubernetes.io/service-name":             globalName,
				"endpointslice.kubernetes.io/managed-by": "linkerd",
				consts.MirroredResourceLabel:             "true",
				consts.RemoteClusterNameLabel:            rcsw.link.TargetClusterName,
			},
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.%s", exportedService.Name, exportedService.Namespace, rcsw.link.TargetClusterDomain),
			},
		},
		Endpoints:   endpointsToCreate,
		AddressType: discoveryv1beta1.AddressTypeIPv4,
		Ports:       []discoveryv1beta1.EndpointPort{},
	}

	if rcsw.link.GatewayIdentity != "" {
		headlessMirrorEndpoints.Annotations[consts.RemoteGatewayIdentity] = rcsw.link.GatewayIdentity
	}

	rcsw.log.Infof("Creating a new global mirror endpoint slice object for global mirror %s/%s: %s", exportedService.Namespace, globalName, endpointSliceName)
	if _, err := rcsw.localAPIClient.Client.DiscoveryV1beta1().EndpointSlices(exportedService.Namespace).Create(ctx, headlessMirrorEndpoints, metav1.CreateOptions{}); err != nil {
		if !kerrors.IsAlreadyExists(err) {
			// Another service mirror may have raced us to creating the service, retry.
			return RetryableError{[]error{err}}
		}
	}

	return nil
}

func BoolRef(b bool) *bool { return &b }

func (rcsw *RemoteClusterServiceWatcher) computeEndpointsForEndpointSlice(ctx context.Context, exportedService *v1.Service, exportedEndpoints *v1.Endpoints) ([]discoveryv1beta1.Endpoint, error) {
	endpointsToCreate := make([]discoveryv1beta1.Endpoint, 0, len(exportedEndpoints.Subsets))
	for _, subset := range exportedEndpoints.Subsets {
		for _, addr := range subset.Addresses {
			if addr.Hostname == "" {
				continue
			}

			endpointMirrorName := rcsw.mirroredResourceName(addr.Hostname)

			endpointMirrorServiceId := fmt.Sprintf("%s/%s", exportedService.Namespace, endpointMirrorName)
			entry, found, err := rcsw.endpointMirrorServiceCache.GetByKey(endpointMirrorServiceId)
			if err != nil {
				rcsw.log.Warnf("error reading endpoint mirror service %s from cache: %v", endpointMirrorServiceId, err)
			}
			var endpointMirrorService *corev1.Service
			if found {
				endpointMirrorService = entry.(*corev1.Service)
			} else {
				svc, err := rcsw.localAPIClient.Svc().Lister().Services(exportedService.Namespace).Get(endpointMirrorName)
				if err != nil {
					exportedServiceId := fmt.Sprintf("%s/%s", exportedService.Namespace, exportedService.Name)
					rcsw.log.Errorf("error getting endpoint mirror service %s/%s for remote exported headless service %s: %v", exportedService.Namespace, endpointMirrorName, exportedServiceId, err)
					return nil, err
				}
				endpointMirrorService = svc
			}

			hostname := rcsw.mirroredResourceName(addr.TargetRef.Name)
			endpointsToCreate = append(endpointsToCreate, discoveryv1beta1.Endpoint{
				Hostname:  &hostname,
				Addresses: []string{endpointMirrorService.Spec.ClusterIP},
			})
		}
	}

	return endpointsToCreate, nil
}

func (rcsw *RemoteClusterServiceWatcher) globalEndpointSliceName(globalName string, exportedService *v1.Service) string {
	return fmt.Sprintf("%s-%s-%s", globalName, rcsw.link.TargetClusterName, exportedService.Name)
}
