package watcher

import (
	"fmt"
	"strings"
	"sync"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	splisters "github.com/linkerd/linkerd2/controller/gen/client/listers/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/cache"
)

type (
	// ProfileWatcher watches all service profiles in the Kubernetes cluster.
	// Listeners can subscribe to a particular profile and profileWatcher will
	// publish the service profile and all future changes for that profile.
	ProfileWatcher struct {
		profileLister splisters.ServiceProfileLister
		profiles      map[ProfileID]*profilePublisher
		profilesMu    sync.RWMutex // This mutex protects modifcation of the map itself.

		log *logging.Entry
	}

	profilePublisher struct {
		profile   *sp.ServiceProfile
		listeners []ProfileUpdateListener
		// All access to the profilePublisher is explicitly synchronized by this mutex.
		mutex sync.Mutex

		log *logging.Entry
	}

	ProfileUpdateListener interface {
		Update(profile *sp.ServiceProfile)
	}
)

func NewProfileWatcher(k8sAPI *k8s.API, log *logging.Entry) *ProfileWatcher {
	watcher := &ProfileWatcher{
		profileLister: k8sAPI.SP().Lister(),
		profiles:      make(map[ProfileID]*profilePublisher),
		log:           log.WithField("component", "profile-watcher"),
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

//////////////////////
/// ProfileWatcher ///
//////////////////////

func (pw *ProfileWatcher) Subscribe(authority string, contextToken string, listener ProfileUpdateListener) error {
	name, err := profileID(authority, contextToken)
	if err != nil {
		return err
	}

	pw.log.Infof("Establishing watch on profile %s", name)

	pw.profilesMu.Lock()
	publisher, ok := pw.profiles[name]
	if !ok {
		profile, err := pw.profileLister.ServiceProfiles(name.Namespace).Get(name.Name)
		if err != nil {
			profile = nil
		}

		publisher = pw.newProfilePublisher(name, profile)
		pw.profiles[name] = publisher
	}
	pw.profilesMu.Unlock()

	publisher.subscribe(listener)
	return nil
}

func (pw *ProfileWatcher) Unsubscribe(authority string, contextToken string, listener ProfileUpdateListener) error {
	name, err := profileID(authority, contextToken)
	if err != nil {
		return err
	}
	pw.log.Infof("Stopping watch on profile %s", name)

	pw.profilesMu.RLock()
	publisher, ok := pw.profiles[name]
	pw.profilesMu.RUnlock()
	if !ok {
		return fmt.Errorf("cannot unsubscribe from unknown service [%s] ", name)
	}
	publisher.unsubscribe(listener)
	return nil
}

func (pw *ProfileWatcher) addProfile(obj interface{}) {
	profile := obj.(*sp.ServiceProfile)
	id := ProfileID{
		Namespace: profile.Namespace,
		Name:      profile.Name,
	}

	pw.profilesMu.Lock()
	publisher, ok := pw.profiles[id]
	if !ok {
		publisher = pw.newProfilePublisher(id, profile)
		pw.profiles[id] = publisher

	}
	pw.profilesMu.Unlock()

	publisher.update(profile)
}

func (pw *ProfileWatcher) updateProfile(old interface{}, new interface{}) {
	pw.addProfile(new)
}

func (pw *ProfileWatcher) deleteProfile(obj interface{}) {
	profile := obj.(*sp.ServiceProfile)
	id := ProfileID{
		Namespace: profile.Namespace,
		Name:      profile.Name,
	}

	pw.profilesMu.RLock()
	publisher, ok := pw.profiles[id]
	pw.profilesMu.RUnlock()
	if ok {
		publisher.update(nil)
	}
}

func (pw *ProfileWatcher) newProfilePublisher(id ProfileID, profile *sp.ServiceProfile) *profilePublisher {
	return &profilePublisher{
		profile:   profile,
		listeners: make([]ProfileUpdateListener, 0),
		log: pw.log.WithFields(logging.Fields{
			"component": "profile-publisher",
			"ns":        id.Namespace,
			"profile":   id.Name,
		}),
	}
}

////////////////////////
/// profilePublisher ///
////////////////////////

func (pp *profilePublisher) subscribe(listener ProfileUpdateListener) {
	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	pp.listeners = append(pp.listeners, listener)
	listener.Update(pp.profile)
}

// unsubscribe returns true iff the listener was found and removed.
// it also returns the number of listeners remaining after unsubscribing.
func (pp *profilePublisher) unsubscribe(listener ProfileUpdateListener) {
	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	for i, item := range pp.listeners {
		if item == listener {
			// delete the item from the slice
			n := len(pp.listeners)
			pp.listeners[i] = pp.listeners[n-1]
			pp.listeners[n-1] = nil
			pp.listeners = pp.listeners[:n-1]
			return
		}
	}
}

func (pp *profilePublisher) update(profile *sp.ServiceProfile) {
	pp.mutex.Lock()
	defer pp.mutex.Unlock()
	pp.log.Debug("Updating profile")

	pp.profile = profile
	for _, listener := range pp.listeners {
		listener.Update(profile)
	}
}

////////////
/// util ///
////////////

func nsFromToken(token string) string {
	// ns:<namespace>
	parts := strings.Split(token, ":")
	if len(parts) == 2 && parts[0] == "ns" {
		return parts[1]
	}

	return ""
}

func profileID(authority string, contextToken string) (ProfileID, error) {
	host, _, err := getHostAndPort(authority)
	if err != nil {
		return ProfileID{}, err
	}
	service, _, err := GetServiceAndPort(authority)
	if err != nil {
		return ProfileID{}, err
	}
	name := ProfileID{
		Name:      host,
		Namespace: service.Name,
	}
	if contextNs := nsFromToken(contextToken); contextNs != "" {
		name.Namespace = contextNs
	}
	return name, nil
}
