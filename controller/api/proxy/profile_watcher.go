package proxy

import (
	"fmt"
	"sync"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	splisters "github.com/linkerd/linkerd2/controller/gen/client/listers/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

type profileID struct {
	namespace string
	name      string
}

func (p profileID) String() string {
	return fmt.Sprintf("%s/%s", p.namespace, p.name)
}

// profileWatcher watches all service profiles in the Kubernetes cluster.
// Listeners can subscribe to a particular profile and profileWatcher will
// publish the service profile and all future changes for that profile.
type profileWatcher struct {
	profileLister splisters.ServiceProfileLister
	profiles      map[profileID]*profileEntry
	profilesLock  sync.RWMutex
}

func newProfileWatcher(k8sAPI *k8s.API) *profileWatcher {
	watcher := &profileWatcher{
		profileLister: k8sAPI.SP().Lister(),
		profiles:      make(map[profileID]*profileEntry),
		profilesLock:  sync.RWMutex{},
	}

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

func (p *profileWatcher) subscribeToProfile(name profileID, listener profileUpdateListener) error {
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

func (p *profileWatcher) unsubscribeToProfile(profile profileID, listener profileUpdateListener) error {
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

func (p *profileWatcher) getProfile(profile profileID) (*sp.ServiceProfile, error) {
	return p.profileLister.ServiceProfiles(profile.namespace).Get(profile.name)
}

func (p *profileWatcher) addProfile(obj interface{}) {
	profile := obj.(*sp.ServiceProfile)
	id := profileID{
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
	id := profileID{
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
