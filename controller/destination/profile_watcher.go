package destination

import (
	"fmt"
	"sync"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	splisters "github.com/linkerd/linkerd2/controller/gen/client/listers/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const profileAnnotation = "linkerd.io/service-profile"

type profileId struct {
	namespace string
	name      string
}

func (p profileId) String() string {
	return fmt.Sprintf("%s/%s", p.namespace, p.name)
}

// profileWatcher watches all services and service profiles in the Kubernetes
// cluster.  Listeners can subscribe to a particular service and profileWatcher
// will publish the service profile and all future changes for that service.
type profileWatcher struct {
	serviceLister corelisters.ServiceLister
	profileLister splisters.ServiceProfileLister
	services      map[serviceId]*serviceEntry
	servicesLock  sync.RWMutex
	profiles      map[profileId]*profileEntry
	profilesLock  sync.RWMutex
}

func newProfileWatcher(k8sAPI *k8s.API) *profileWatcher {
	watcher := &profileWatcher{
		serviceLister: k8sAPI.Svc().Lister(),
		profileLister: k8sAPI.SP().Lister(),
		services:      make(map[serviceId]*serviceEntry),
		servicesLock:  sync.RWMutex{},
		profiles:      make(map[profileId]*profileEntry),
		profilesLock:  sync.RWMutex{},
	}

	k8sAPI.Svc().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    watcher.addService,
			UpdateFunc: watcher.updateService,
			DeleteFunc: watcher.deleteService,
		},
	)

	k8sAPI.SP().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    watcher.addProfile,
			UpdateFunc: watcher.updateProfile,
			DeleteFunc: watcher.deleteProfile,
		},
	)

	return watcher
}

// Close all open streams on shutdown
func (p *profileWatcher) stop() {
	p.profilesLock.Lock()
	defer p.profilesLock.Unlock()

	for _, profile := range p.profiles {
		profile.unsubscribeAll()
	}
}

// Subscribe to a service.
// The provided listener will be updated each time the service changes which
// service profile it uses and each time the service's service profile is
// updated.
func (p *profileWatcher) subscribeToSvc(service serviceId, listener profileUpdateListener) error {
	log.Infof("Establishing profile watch on service %s", service)

	svc, err := p.getService(service)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Errorf("Error getting service: %s", err)
		return err
	}

	p.servicesLock.Lock()
	defer p.servicesLock.Unlock()

	svcEntry, ok := p.services[service]
	if !ok {
		svcEntry = newServiceEntry(svc)
		p.services[service] = svcEntry
	}
	svcEntry.subscribe(listener)
	p.subscribeToProfile(svcEntry.profile, listener)
	return nil
}

func (p *profileWatcher) subscribeToProfile(name profileId, listener profileUpdateListener) error {
	p.profilesLock.Lock()
	defer p.profilesLock.Unlock()

	profileEntry, ok := p.profiles[name]
	if !ok {
		profile, err := p.getProfile(name)
		if err != nil && !apierrors.IsNotFound(err) {
			log.Errorf("Error getting profile: %s", err)
			return err
		}

		profileEntry = newProfileEntry(profile)
		p.profiles[name] = profileEntry
	}
	profileEntry.subscribe(listener)
	return nil
}

func (p *profileWatcher) unsubscribeToSvc(service serviceId, listener profileUpdateListener) error {
	log.Infof("Stopping profile watch on service %s", service)

	p.servicesLock.Lock()
	defer p.servicesLock.Unlock()

	svcEntry, ok := p.services[service]
	if !ok {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", service)
	}

	unsubscribed, numListeners := svcEntry.unsubscribe(listener)
	if !unsubscribed {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", service)
	}
	if numListeners == 0 {
		delete(p.services, service)
	}
	return p.unsubscribeToProfile(svcEntry.profile, listener)
}

func (p *profileWatcher) unsubscribeToProfile(profile profileId, listener profileUpdateListener) error {
	p.profilesLock.Lock()
	defer p.profilesLock.Unlock()

	profileEntry, ok := p.profiles[profile]
	if !ok {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", profile)
	}

	unsubscribed, numListeners := profileEntry.unsubscribe(listener)
	if !unsubscribed {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", profile)
	}
	if numListeners == 0 {
		delete(p.profiles, profile)
	}
	return nil
}

func (p *profileWatcher) getService(service serviceId) (*v1.Service, error) {
	return p.serviceLister.Services(service.namespace).Get(service.name)
}

func (p *profileWatcher) getProfile(profile profileId) (*sp.ServiceProfile, error) {
	return p.profileLister.ServiceProfiles(profile.namespace).Get(profile.name)
}

func (p *profileWatcher) addService(obj interface{}) {
	service := obj.(*v1.Service)
	if service.Namespace == kubeSystem {
		return
	}
	id := serviceId{
		namespace: service.Namespace,
		name:      service.Name,
	}

	p.servicesLock.Lock()
	defer p.servicesLock.Unlock()
	entry, ok := p.services[id]
	if ok {
		newId := profileId{
			name:      service.Annotations[profileAnnotation],
			namespace: service.Namespace,
		}
		if newId != entry.profile {
			for _, listener := range entry.listeners {
				p.unsubscribeToProfile(entry.profile, listener)
				p.subscribeToProfile(newId, listener)
			}
			entry.profile = newId
		}
	}
}

func (p *profileWatcher) updateService(old interface{}, new interface{}) {
	p.addService(new)
}

func (p *profileWatcher) deleteService(obj interface{}) {
	service := obj.(*v1.Service)
	if service.Namespace == kubeSystem {
		return
	}
	id := serviceId{
		namespace: service.Namespace,
		name:      service.Name,
	}

	p.servicesLock.RLock()
	defer p.servicesLock.RUnlock()
	entry, ok := p.services[id]
	if ok {
		for _, listener := range entry.listeners {
			p.unsubscribeToProfile(entry.profile, listener)
			listener.Update(&sp.ServiceProfile{})
		}
	}
}

func (p *profileWatcher) addProfile(obj interface{}) {
	profile := obj.(*sp.ServiceProfile)
	id := profileId{
		namespace: profile.Namespace,
		name:      profile.Name,
	}

	p.profilesLock.RLock()
	defer p.profilesLock.RUnlock()
	entry, ok := p.profiles[id]
	if ok {
		entry.update(profile)
	}
}

func (p *profileWatcher) updateProfile(old interface{}, new interface{}) {
	p.addProfile(new)
}

func (p *profileWatcher) deleteProfile(obj interface{}) {
	profile := obj.(*sp.ServiceProfile)
	id := profileId{
		namespace: profile.Namespace,
		name:      profile.Name,
	}

	p.profilesLock.RLock()
	defer p.profilesLock.RUnlock()
	entry, ok := p.profiles[id]
	if ok {
		entry.update(&sp.ServiceProfile{})
	}
}

type serviceEntry struct {
	profile   profileId
	listeners []profileUpdateListener
	mutex     sync.Mutex
}

func newServiceEntry(service *v1.Service) *serviceEntry {
	id := profileId{}
	if service != nil {
		id.name = service.Annotations[profileAnnotation]
		id.namespace = service.Namespace
	}
	return &serviceEntry{
		profile:   id,
		listeners: make([]profileUpdateListener, 0),
		mutex:     sync.Mutex{},
	}
}

func (e *serviceEntry) subscribe(listener profileUpdateListener) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.listeners = append(e.listeners, listener)
}

// unsubscribe returns true iff the listener was found and removed.
// it also returns the number of listeners remaining after unsubscribing.
func (e *serviceEntry) unsubscribe(listener profileUpdateListener) (bool, int) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	for i, item := range e.listeners {
		if item == listener {
			// delete the item from the slice
			e.listeners[i] = e.listeners[len(e.listeners)-1]
			e.listeners[len(e.listeners)-1] = nil
			e.listeners = e.listeners[:len(e.listeners)-1]
			return true, len(e.listeners)
		}
	}
	return false, len(e.listeners)
}

type profileEntry struct {
	profile   *sp.ServiceProfile
	listeners []profileUpdateListener
	mutex     sync.Mutex
}

func newProfileEntry(profile *sp.ServiceProfile) *profileEntry {
	return &profileEntry{
		profile:   profile,
		listeners: make([]profileUpdateListener, 0),
		mutex:     sync.Mutex{},
	}
}

func (e *profileEntry) subscribe(listener profileUpdateListener) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.listeners = append(e.listeners, listener)
	listener.Update(e.profile)
}

// unsubscribe returns true iff the listener was found and removed.
// it also returns the number of listeners remaining after unsubscribing.
func (e *profileEntry) unsubscribe(listener profileUpdateListener) (bool, int) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	for i, item := range e.listeners {
		if item == listener {
			// delete the item from the slice
			e.listeners[i] = e.listeners[len(e.listeners)-1]
			e.listeners[len(e.listeners)-1] = nil
			e.listeners = e.listeners[:len(e.listeners)-1]
			return true, len(e.listeners)
		}
	}
	return false, len(e.listeners)
}

func (e *profileEntry) update(profile *sp.ServiceProfile) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.profile = profile
	for _, listener := range e.listeners {
		listener.Update(profile)
	}
}

func (e *profileEntry) unsubscribeAll() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	for _, listener := range e.listeners {
		listener.Stop()
	}
	e.listeners = make([]profileUpdateListener, 0)
}
