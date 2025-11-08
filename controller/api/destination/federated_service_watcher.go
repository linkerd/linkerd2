package destination

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/k8s"
	labels "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

// FederatedServiceWatcher watches federated services for the local discovery
// and remote discovery annotations and subscribes to the approprite local and
// remote services.
type federatedServiceWatcher struct {
	services       map[watcher.ServiceID]*federatedService
	k8sAPI         *k8s.API
	config         *Config
	clusterStore   *watcher.ClusterStore
	localEndpoints *watcher.EndpointsWatcher

	log *logging.Entry

	sync.RWMutex
}

type remoteDiscoveryID struct {
	cluster string
	service watcher.ServiceID
}

// FederatedService represents a federated service and it may have a local
// discovery target and remote discovery targets. This struct holds a list of
// subsribers that are subscribed to the federated service.
type federatedService struct {
	namespace string

	localDiscovery  string
	remoteDiscovery []remoteDiscoveryID
	subscribers     []federatedServiceSubscriber

	config         *Config
	localEndpoints *watcher.EndpointsWatcher
	clusterStore   *watcher.ClusterStore
	log            *logging.Entry

	sync.Mutex
}

// FederatedServiceSubscriber holds all the state for an individual subscriber
// stream to a federated service.
type federatedServiceSubscriber struct {
	port             uint32
	nodeName         string
	nodeTopologyZone string
	instanceID       string

	localViews  map[string]*endpointView
	remoteViews map[remoteDiscoveryID]*endpointView

	dispatcher *endpointStreamDispatcher
}

func newFederatedServiceWatcher(
	k8sAPI *k8s.API,
	config *Config,
	clusterStore *watcher.ClusterStore,
	localEndpoints *watcher.EndpointsWatcher,
	log *logging.Entry,
) (*federatedServiceWatcher, error) {
	fsw := &federatedServiceWatcher{
		services:       make(map[watcher.ServiceID]*federatedService),
		k8sAPI:         k8sAPI,
		config:         config,
		clusterStore:   clusterStore,
		localEndpoints: localEndpoints,
		log: log.WithFields(logging.Fields{
			"component": "federated-service-watcher",
		}),
	}

	var err error
	_, err = k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    fsw.addService,
		DeleteFunc: fsw.deleteService,
		UpdateFunc: fsw.updateService,
	})
	if err != nil {
		return nil, err
	}
	return fsw, nil
}

func (fsw *federatedServiceWatcher) Subscribe(
	service string,
	namespace string,
	port uint32,
	nodeName string,
	nodeTopologyZone string,
	instanceID string,
	dispatcher *endpointStreamDispatcher,
) error {
	id := watcher.ServiceID{Namespace: namespace, Name: service}
	fsw.RLock()
	if federatedService, ok := fsw.services[id]; ok {
		fsw.RUnlock()
		fsw.log.Debugf("Subscribing to federated service %s/%s", namespace, service)
		federatedService.subscribe(port, nodeName, nodeTopologyZone, instanceID, dispatcher)
		return nil
	} else {
		fsw.RUnlock()
	}
	return fmt.Errorf("service %s/%s is not a federated service", namespace, service)
}

func (fsw *federatedServiceWatcher) Unsubscribe(
	service string,
	namespace string,
	dispatcher *endpointStreamDispatcher,
) {
	id := watcher.ServiceID{Namespace: namespace, Name: service}
	fsw.RLock()
	if federatedService, ok := fsw.services[id]; ok {
		fsw.RUnlock()
		fsw.log.Debugf("Unsubscribing from federated service %s/%s", namespace, service)
		federatedService.unsubscribe(dispatcher)
	} else {
		fsw.RUnlock()
	}
}

func (fsw *federatedServiceWatcher) addService(obj interface{}) {
	service := obj.(*corev1.Service)
	id := watcher.ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	if isFederatedService(service) {
		fsw.Lock()
		if federatedService, ok := fsw.services[id]; ok {
			fsw.Unlock()
			fsw.log.Debugf("Updating federated service %s/%s", service.Namespace, service.Name)
			federatedService.update(service)
		} else {
			fsw.log.Debugf("Adding federated service %s/%s", service.Namespace, service.Name)
			federatedService = fsw.newFederatedService(service)
			fsw.services[id] = federatedService
			fsw.Unlock()
			federatedService.update(service)
		}
	} else {
		fsw.Lock()
		if federatedService, ok := fsw.services[id]; ok {
			delete(fsw.services, id)
			fsw.Unlock()
			fsw.log.Debugf("Service %s/%s is no longer a federated service", service.Namespace, service.Name)
			federatedService.delete()
		} else {
			fsw.Unlock()
		}
	}
}

func (fsw *federatedServiceWatcher) updateService(oldObj interface{}, newObj interface{}) {
	fsw.addService(newObj)
}

func (fsw *federatedServiceWatcher) deleteService(obj interface{}) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			fsw.log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		service, ok = tombstone.Obj.(*corev1.Service)
		if !ok {
			fsw.log.Errorf("DeletedFinalStateUnknown contained object that is not a Service %#v", obj)
			return
		}
	}

	id := watcher.ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}
	fsw.Lock()
	if federatedService, ok := fsw.services[id]; ok {
		delete(fsw.services, id)
		fsw.Unlock()
		federatedService.delete()
	} else {
		fsw.Unlock()
	}

}

func (fsw *federatedServiceWatcher) newFederatedService(service *corev1.Service) *federatedService {
	return &federatedService{
		namespace: service.Namespace,

		localDiscovery:  service.Annotations[labels.LocalDiscoveryAnnotation],
		remoteDiscovery: remoteDiscoveryIDs(service, fsw.log),
		subscribers:     []federatedServiceSubscriber{},

		config:         fsw.config,
		localEndpoints: fsw.localEndpoints,
		clusterStore:   fsw.clusterStore,
		log:            fsw.log.WithFields(logging.Fields{"service": service.Name, "namespace": service.Namespace}),
	}
}

func (fs *federatedService) update(service *corev1.Service) {
	fs.Lock()
	defer fs.Unlock()

	newRemoteDiscovery := remoteDiscoveryIDs(service, fs.log)
	for _, id := range newRemoteDiscovery {
		if !slices.Contains(fs.remoteDiscovery, id) {
			for i := range fs.subscribers {
				fs.remoteDiscoverySubscribe(&fs.subscribers[i], id)
			}
		}
	}
	for _, id := range fs.remoteDiscovery {
		if !slices.Contains(newRemoteDiscovery, id) {
			for i := range fs.subscribers {
				fs.remoteDiscoveryUnsubscribe(&fs.subscribers[i], id)
			}
		}
	}
	fs.remoteDiscovery = newRemoteDiscovery

	newLocalDiscovery := service.Annotations[labels.LocalDiscoveryAnnotation]
	if fs.localDiscovery != service.Annotations[labels.LocalDiscoveryAnnotation] {
		if newLocalDiscovery != "" {
			for i := range fs.subscribers {
				if fs.localDiscovery != "" {
					fs.localDiscoveryUnsubscribe(&fs.subscribers[i], fs.localDiscovery)
				}
				fs.localDiscoverySubscribe(&fs.subscribers[i], newLocalDiscovery)
			}
		} else {
			for i := range fs.subscribers {
				fs.localDiscoveryUnsubscribe(&fs.subscribers[i], fs.localDiscovery)
			}
		}
	}
	fs.localDiscovery = newLocalDiscovery
}

func (fs *federatedService) delete() {
	fs.Lock()
	defer fs.Unlock()

	for i := range fs.subscribers {
		subscriber := &fs.subscribers[i]
		for id := range subscriber.remoteViews {
			fs.remoteDiscoveryUnsubscribe(subscriber, id)
		}
		for localDiscovery := range subscriber.localViews {
			fs.localDiscoveryUnsubscribe(subscriber, localDiscovery)
		}
	}

	fs.subscribers = nil
}

func (fs *federatedService) subscribe(
	port uint32,
	nodeName string,
	nodeTopologyZone string,
	instanceID string,
	dispatcher *endpointStreamDispatcher,
) {
	fs.Lock()
	defer fs.Unlock()

	subscriber := federatedServiceSubscriber{
		dispatcher:       dispatcher,
		remoteViews:      make(map[remoteDiscoveryID]*endpointView, 0),
		localViews:       make(map[string]*endpointView, 0),
		port:             port,
		nodeName:         nodeName,
		nodeTopologyZone: nodeTopologyZone,
		instanceID:       instanceID,
	}
	for _, id := range fs.remoteDiscovery {
		fs.remoteDiscoverySubscribe(&subscriber, id)
	}
	if fs.localDiscovery != "" {
		fs.localDiscoverySubscribe(&subscriber, fs.localDiscovery)
	}

	fs.subscribers = append(fs.subscribers, subscriber)
}

func (fs *federatedService) unsubscribe(dispatcher *endpointStreamDispatcher) {
	fs.Lock()
	defer fs.Unlock()

	subscribers := make([]federatedServiceSubscriber, 0)
	for i, subscriber := range fs.subscribers {
		if subscriber.dispatcher == dispatcher {
			for id := range subscriber.remoteViews {
				fs.remoteDiscoveryUnsubscribe(&fs.subscribers[i], id)
			}
			for localDiscovery := range subscriber.localViews {
				fs.localDiscoveryUnsubscribe(&fs.subscribers[i], localDiscovery)
			}
		} else {
			subscribers = append(subscribers, subscriber)
		}
	}
	fs.subscribers = subscribers
}

func (fs *federatedService) remoteDiscoverySubscribe(
	subscriber *federatedServiceSubscriber,
	id remoteDiscoveryID,
) {
	remoteWatcher, remoteConfig, found := fs.clusterStore.Get(id.cluster)
	if !found {
		fs.log.Errorf("Failed to get remote cluster %s", id.cluster)
		return
	}

	if subscriber.dispatcher == nil {
		fs.log.Errorf("Dispatcher is nil for subscriber to remote discovery service %s", id.service)
		return
	}

	serviceName := fmt.Sprintf("%s.%s.svc.%s:%d", id.service, fs.namespace, remoteConfig.ClusterDomain, subscriber.port)
	cfg := endpointTranslatorConfig{
		controllerNS:            fs.config.ControllerNS,
		identityTrustDomain:     remoteConfig.TrustDomain,
		nodeName:                subscriber.nodeName,
		nodeTopologyZone:        subscriber.nodeTopologyZone,
		defaultOpaquePorts:      fs.config.DefaultOpaquePorts,
		forceOpaqueTransport:    fs.config.ForceOpaqueTransport,
		enableH2Upgrade:         fs.config.EnableH2Upgrade,
		enableEndpointFiltering: false,
		enableIPv6:              fs.config.EnableIPv6,
		extEndpointZoneWeights:  fs.config.ExtEndpointZoneWeights,
		meshedHTTP2ClientParams: fs.config.MeshedHttp2ClientParams,
		service:                 serviceName,
	}

	topic, err := remoteWatcher.Topic(watcher.ServiceID{Namespace: id.service.Namespace, Name: id.service.Name}, subscriber.port, subscriber.instanceID)
	if err != nil {
		fs.log.Errorf("Failed to resolve topic for remote discovery service %q in cluster %s: %s", id.service.Name, id.cluster, err)
		return
	}

	view, err := subscriber.dispatcher.newEndpointView(context.Background(), topic, &cfg, fs.log)
	if err != nil {
		fs.log.Errorf("Failed to create endpoint view for remote discovery service %q in cluster %s: %s", id.service.Name, id.cluster, err)
		return
	}

	fs.log.Debugf("Subscribing to remote discovery service %s in cluster %s", id.service, id.cluster)
	subscriber.remoteViews[id] = view
}

func (fs *federatedService) remoteDiscoveryUnsubscribe(
	subscriber *federatedServiceSubscriber,
	id remoteDiscoveryID,
) {
	view, ok := subscriber.remoteViews[id]
	if !ok {
		return
	}
	fs.log.Debugf("Unsubscribing from remote discovery service %s in cluster %s", id.service, id.cluster)
	view.NoEndpoints(true)
	delete(subscriber.remoteViews, id)
	view.Close()
}

func (fs *federatedService) localDiscoverySubscribe(
	subscriber *federatedServiceSubscriber,
	localDiscovery string,
) {
	if subscriber.dispatcher == nil {
		fs.log.Errorf("Dispatcher is nil for subscriber to local discovery service %s", localDiscovery)
		return
	}

	cfg := endpointTranslatorConfig{
		controllerNS:            fs.config.ControllerNS,
		identityTrustDomain:     fs.config.IdentityTrustDomain,
		nodeName:                subscriber.nodeName,
		nodeTopologyZone:        subscriber.nodeTopologyZone,
		defaultOpaquePorts:      fs.config.DefaultOpaquePorts,
		forceOpaqueTransport:    fs.config.ForceOpaqueTransport,
		enableH2Upgrade:         fs.config.EnableH2Upgrade,
		enableEndpointFiltering: true,
		enableIPv6:              fs.config.EnableIPv6,
		extEndpointZoneWeights:  fs.config.ExtEndpointZoneWeights,
		meshedHTTP2ClientParams: fs.config.MeshedHttp2ClientParams,
		service:                 localDiscovery,
	}

	topic, err := fs.localEndpoints.Topic(watcher.ServiceID{Namespace: fs.namespace, Name: localDiscovery}, subscriber.port, subscriber.instanceID)
	if err != nil {
		fs.log.Errorf("Failed to resolve topic for %s: %s", localDiscovery, err)
		return
	}

	view, err := subscriber.dispatcher.newEndpointView(context.Background(), topic, &cfg, fs.log)
	if err != nil {
		fs.log.Errorf("Failed to create endpoint view for %s: %s", localDiscovery, err)
		return
	}

	fs.log.Debugf("Subscribing to local discovery service %s", localDiscovery)
	subscriber.localViews[localDiscovery] = view
}

func (fs *federatedService) localDiscoveryUnsubscribe(
	subscriber *federatedServiceSubscriber,
	localDiscovery string,
) {
	view, found := subscriber.localViews[localDiscovery]
	if !found {
		return
	}
	fs.log.Debugf("Unsubscribing to local discovery service %s", localDiscovery)
	view.NoEndpoints(true)
	delete(subscriber.localViews, localDiscovery)
	view.Close()
}

func remoteDiscoveryIDs(service *corev1.Service, log *logging.Entry) []remoteDiscoveryID {
	remoteDiscovery, remoteDiscoveryFound := service.Annotations[labels.RemoteDiscoveryAnnotation]
	if !remoteDiscoveryFound {
		return nil
	}

	remotes := strings.Split(remoteDiscovery, ",")
	ids := make([]remoteDiscoveryID, 0)
	for _, remote := range remotes {
		parts := strings.Split(remote, "@")
		if len(parts) != 2 {
			log.Errorf("Invalid remote discovery service '%s'", remote)
			continue
		}
		remoteSvc := parts[0]
		cluster := parts[1]
		ids = append(ids, remoteDiscoveryID{
			cluster: cluster,
			service: watcher.ServiceID{
				Namespace: service.Namespace,
				Name:      remoteSvc,
			},
		})
	}
	return ids
}

func isFederatedService(service *corev1.Service) bool {
	_, localDiscoveryFound := service.Annotations[labels.LocalDiscoveryAnnotation]
	_, remoteDiscoveryFound := service.Annotations[labels.RemoteDiscoveryAnnotation]
	return localDiscoveryFound || remoteDiscoveryFound
}
