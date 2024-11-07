package destination

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
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
	metadataAPI    *k8s.MetadataAPI
	config         *Config
	clusterStore   *watcher.ClusterStore
	localEndpoints *watcher.EndpointsWatcher

	log *logging.Entry
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

	metadataAPI    *k8s.MetadataAPI
	config         *Config
	localEndpoints *watcher.EndpointsWatcher
	clusterStore   *watcher.ClusterStore
	log            *logging.Entry

	sync.Mutex
}

// FederatedServiceSubscriber holds all the state for an individual subscriber
// stream to a federated service.
type federatedServiceSubscriber struct {
	port       uint32
	nodeName   string
	instanceID string

	localTranslators  map[string]*endpointTranslator
	remoteTranslators map[remoteDiscoveryID]*endpointTranslator

	stream    *synchronizedGetStream
	endStream chan struct{}
}

func newFederatedServiceWatcher(
	k8sAPI *k8s.API,
	metadataAPI *k8s.MetadataAPI,
	config *Config,
	clusterStore *watcher.ClusterStore,
	localEndpoints *watcher.EndpointsWatcher,
	log *logging.Entry,
) (*federatedServiceWatcher, error) {
	fsw := &federatedServiceWatcher{
		services:       make(map[watcher.ServiceID]*federatedService),
		k8sAPI:         k8sAPI,
		metadataAPI:    metadataAPI,
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
	instanceID string,
	stream pb.Destination_GetServer,
	endStream chan struct{},
) error {
	id := watcher.ServiceID{Namespace: namespace, Name: service}
	if federatedService, ok := fsw.services[id]; ok {
		fsw.log.Debugf("Subscribing to federated service %s/%s", namespace, service)
		federatedService.subscribe(port, nodeName, instanceID, stream, endStream)
		return nil
	}
	return fmt.Errorf("service %s/%s is not a federated service", namespace, service)
}

func (fsw *federatedServiceWatcher) Unsubscribe(
	service string,
	namespace string,
	stream pb.Destination_GetServer,
) {
	id := watcher.ServiceID{Namespace: namespace, Name: service}
	if federatedService, ok := fsw.services[id]; ok {
		fsw.log.Debugf("Unsubscribing from federated service %s/%s", namespace, service)
		federatedService.unsubscribe(stream)
	}
}

func (fsw *federatedServiceWatcher) addService(obj interface{}) {
	service := obj.(*corev1.Service)
	id := watcher.ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	if isFederatedService(service) {
		if federatedService, ok := fsw.services[id]; ok {
			fsw.log.Debugf("Updating federated service %s/%s", service.Namespace, service.Name)
			federatedService.update(service)
		} else {
			fsw.log.Debugf("Adding federated service %s/%s", service.Namespace, service.Name)
			federatedService = fsw.newFederatedService(service)
			fsw.services[id] = federatedService
			federatedService.update(service)
		}
	} else {
		if federatedService, ok := fsw.services[id]; ok {
			fsw.log.Debugf("Service %s/%s is no longer a federated service", service.Namespace, service.Name)
			federatedService.delete()
			delete(fsw.services, id)
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
	if federatedService, ok := fsw.services[id]; ok {
		federatedService.delete()
		delete(fsw.services, id)
	}
}

func (fsw *federatedServiceWatcher) newFederatedService(service *corev1.Service) *federatedService {
	return &federatedService{
		namespace: service.Namespace,

		localDiscovery:  service.Annotations[labels.LocalDiscoveryAnnotation],
		remoteDiscovery: remoteDiscoveryIDs(service, fsw.log),
		subscribers:     []federatedServiceSubscriber{},

		metadataAPI:    fsw.metadataAPI,
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

	for _, subscriber := range fs.subscribers {
		for id, translator := range subscriber.remoteTranslators {
			remoteWatcher, _, found := fs.clusterStore.Get(id.cluster)
			if !found {
				fs.log.Errorf("Failed to get remote cluster %s", id.cluster)
				continue
			}
			remoteWatcher.Unsubscribe(id.service, subscriber.port, subscriber.instanceID, translator)
			translator.Stop()
		}
		for localDiscovery, translator := range subscriber.localTranslators {
			fs.localEndpoints.Unsubscribe(watcher.ServiceID{Namespace: fs.namespace, Name: localDiscovery}, subscriber.port, subscriber.instanceID, translator)
			translator.Stop()
		}
		close(subscriber.endStream)
	}
}

func (fs *federatedService) subscribe(
	port uint32,
	nodeName string,
	instanceID string,
	stream pb.Destination_GetServer,
	endStream chan struct{},
) {
	syncStream := newSyncronizedGetStream(stream, fs.log)
	syncStream.Start()

	subscriber := federatedServiceSubscriber{
		stream:            syncStream,
		endStream:         endStream,
		remoteTranslators: make(map[remoteDiscoveryID]*endpointTranslator, 0),
		localTranslators:  make(map[string]*endpointTranslator, 0),
		port:              port,
		nodeName:          nodeName,
		instanceID:        instanceID,
	}
	for _, id := range fs.remoteDiscovery {
		fs.remoteDiscoverySubscribe(&subscriber, id)
	}
	if fs.localDiscovery != "" {
		fs.localDiscoverySubscribe(&subscriber, fs.localDiscovery)
	}

	fs.Lock()
	defer fs.Unlock()
	fs.subscribers = append(fs.subscribers, subscriber)
}

func (fs *federatedService) unsubscribe(
	stream pb.Destination_GetServer,
) {
	fs.Lock()
	defer fs.Unlock()

	subscribers := make([]federatedServiceSubscriber, 0)
	for i, subscriber := range fs.subscribers {
		if subscriber.stream.inner == stream {
			for id := range subscriber.remoteTranslators {
				fs.remoteDiscoveryUnsubscribe(&fs.subscribers[i], id)
			}
			for localDiscovery := range subscriber.localTranslators {
				fs.localDiscoveryUnsubscribe(&fs.subscribers[i], localDiscovery)
			}
			subscriber.stream.Stop()
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
	}

	translator := newEndpointTranslator(
		fs.config.ControllerNS,
		remoteConfig.TrustDomain,
		fs.config.EnableH2Upgrade,
		false, // Disable endpoint filtering for remote discovery.
		fs.config.EnableIPv6,
		fs.config.ExtEndpointZoneWeights,
		fs.config.MeshedHttp2ClientParams,
		fmt.Sprintf("%s.%s.svc.%s:%d", id.service, fs.namespace, remoteConfig.ClusterDomain, subscriber.port),
		subscriber.nodeName,
		fs.config.DefaultOpaquePorts,
		fs.metadataAPI,
		subscriber.stream,
		subscriber.endStream,
		fs.log,
	)
	translator.Start()
	subscriber.remoteTranslators[id] = translator

	fs.log.Debugf("Subscribing to remote discovery service %s in cluster %s", id.service, id.cluster)
	err := remoteWatcher.Subscribe(watcher.ServiceID{Namespace: id.service.Namespace, Name: id.service.Name}, subscriber.port, subscriber.instanceID, translator)
	if err != nil {
		fs.log.Errorf("Failed to subscribe to remote disocvery service %q in cluster %s: %s", id.service.Name, id.cluster, err)
	}
}

func (fs *federatedService) remoteDiscoveryUnsubscribe(
	subscriber *federatedServiceSubscriber,
	id remoteDiscoveryID,
) {
	remoteWatcher, _, found := fs.clusterStore.Get(id.cluster)
	if !found {
		fs.log.Errorf("Failed to get remote cluster %s", id.cluster)
		return
	}

	translator := subscriber.remoteTranslators[id]
	fs.log.Debugf("Unsubscribing from remote discovery service %s in cluster %s", id.service, id.cluster)
	remoteWatcher.Unsubscribe(id.service, subscriber.port, subscriber.instanceID, translator)
	translator.NoEndpoints(true)
	translator.DrainAndStop()
	delete(subscriber.remoteTranslators, id)
}

func (fs *federatedService) localDiscoverySubscribe(
	subscriber *federatedServiceSubscriber,
	localDiscovery string,
) {
	translator := newEndpointTranslator(
		fs.config.ControllerNS,
		fs.config.IdentityTrustDomain,
		fs.config.EnableH2Upgrade,
		true,
		fs.config.EnableIPv6,
		fs.config.ExtEndpointZoneWeights,
		fs.config.MeshedHttp2ClientParams,
		localDiscovery,
		subscriber.nodeName,
		fs.config.DefaultOpaquePorts,
		fs.metadataAPI,
		subscriber.stream,
		subscriber.endStream,
		fs.log,
	)
	translator.Start()
	subscriber.localTranslators[localDiscovery] = translator

	fs.log.Debugf("Subscribing to local discovery service %s", localDiscovery)
	err := fs.localEndpoints.Subscribe(watcher.ServiceID{Namespace: fs.namespace, Name: localDiscovery}, subscriber.port, subscriber.instanceID, translator)
	if err != nil {
		fs.log.Errorf("Failed to subscribe to %s: %s", localDiscovery, err)
	}
}

func (fs *federatedService) localDiscoveryUnsubscribe(
	subscriber *federatedServiceSubscriber,
	localDiscovery string,
) {
	translator, found := subscriber.localTranslators[localDiscovery]
	if found {
		fs.log.Debugf("Unsubscribing to local discovery service %s", localDiscovery)
		fs.localEndpoints.Unsubscribe(watcher.ServiceID{Namespace: fs.namespace, Name: localDiscovery}, subscriber.port, subscriber.instanceID, translator)
		translator.NoEndpoints(true)
		translator.DrainAndStop()
		delete(subscriber.localTranslators, localDiscovery)
	}
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
