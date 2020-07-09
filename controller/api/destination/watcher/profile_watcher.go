package watcher

import (
	"fmt"
	"sync"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	splisters "github.com/linkerd/linkerd2/controller/gen/client/listers/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

type (
	// ProfileWatcher watches all service profiles in the Kubernetes cluster.
	// Listeners can subscribe to a particular profile and profileWatcher will
	// publish the service profile and all future changes for that profile.
	ProfileWatcher struct {
		profileLister splisters.ServiceProfileLister
		profiles      map[ProfileID]*profilePublisher // <-- intentional formatting error to test CI

		log          *logging.Entry
		sync.RWMutex // This mutex protects modification of the map itself.
	}

	profilePublisher struct {
		profile   *sp.ServiceProfile
		listeners []ProfileUpdateListener

		log            *logging.Entry
		profileMetrics metrics
		// All access to the profilePublisher is explicitly synchronized by this mutex.
		sync.Mutex
	}

	// ProfileUpdateListener is the interface that subscribers must implement.
	ProfileUpdateListener interface {
		Update(profile *sp.ServiceProfile)
	}
)

var profileVecs = newMetricsVecs("profile", []string{"namespace", "profile"})

// NewProfileWatcher creates a ProfileWatcher and begins watching the k8sAPI for
// service profile changes.
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

// Subscribe to an authority.
// The provided listener will be updated each time the service profile for the
// given authority is changed.
func (pw *ProfileWatcher) Subscribe(id ProfileID, listener ProfileUpdateListener) error {
	pw.log.Infof("Establishing watch on profile %s", id)

	publisher := pw.getOrNewProfilePublisher(id, nil)

	publisher.subscribe(listener)
	return nil
}

// Unsubscribe removes a listener from the subscribers list for this authority.
func (pw *ProfileWatcher) Unsubscribe(id ProfileID, listener ProfileUpdateListener) error {
	pw.log.Infof("Stopping watch on profile %s", id)

	publisher, ok := pw.getProfilePublisher(id)
	if !ok {
		return fmt.Errorf("cannot unsubscribe from unknown service [%s] ", id)
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

	publisher := pw.getOrNewProfilePublisher(id, profile)

	publisher.update(profile)
}

func (pw *ProfileWatcher) updateProfile(old interface{}, new interface{}) {
	pw.addProfile(new)
}

func (pw *ProfileWatcher) deleteProfile(obj interface{}) {
	profile, ok := obj.(*sp.ServiceProfile)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			pw.log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		profile, ok = tombstone.Obj.(*sp.ServiceProfile)
		if !ok {
			pw.log.Errorf("DeletedFinalStateUnknown contained object that is not a ServiceProfile %#v", obj)
			return
		}
	}

	id := ProfileID{
		Namespace: profile.Namespace,
		Name:      profile.Name,
	}

	publisher, ok := pw.getProfilePublisher(id)
	if ok {
		publisher.update(nil)
	}
}

func (pw *ProfileWatcher) getOrNewProfilePublisher(id ProfileID, profile *sp.ServiceProfile) *profilePublisher {
	pw.Lock()
	defer pw.Unlock()

	publisher, ok := pw.profiles[id]
	if !ok {
		if profile == nil {
			var err error
			profile, err = pw.profileLister.ServiceProfiles(id.Namespace).Get(id.Name)
			if err != nil && !apierrors.IsNotFound(err) {
				pw.log.Errorf("error getting service profile: %s", err)
			}
			if err != nil {
				profile = nil
			}
		}

		publisher = &profilePublisher{
			profile:   profile,
			listeners: make([]ProfileUpdateListener, 0),
			log: pw.log.WithFields(logging.Fields{
				"component": "profile-publisher",
				"ns":        id.Namespace,
				"profile":   id.Name,
			}),
			profileMetrics: profileVecs.newMetrics(prometheus.Labels{
				"namespace": id.Namespace,
				"profile":   id.Name,
			}),
		}
		pw.profiles[id] = publisher
	}

	return publisher
}

func (pw *ProfileWatcher) getProfilePublisher(id ProfileID) (publisher *profilePublisher, ok bool) {
	pw.RLock()
	defer pw.RUnlock()
	publisher, ok = pw.profiles[id]
	return
}

////////////////////////
/// profilePublisher ///
////////////////////////

func (pp *profilePublisher) subscribe(listener ProfileUpdateListener) {
	pp.Lock()
	defer pp.Unlock()

	pp.listeners = append(pp.listeners, listener)
	listener.Update(pp.profile)

	pp.profileMetrics.setSubscribers(len(pp.listeners))
}

// unsubscribe returns true if and only if the listener was found and removed.
// it also returns the number of listeners remaining after unsubscribing.
func (pp *profilePublisher) unsubscribe(listener ProfileUpdateListener) {
	pp.Lock()
	defer pp.Unlock()

	for i, item := range pp.listeners {
		if item == listener {
			// delete the item from the slice
			n := len(pp.listeners)
			pp.listeners[i] = pp.listeners[n-1]
			pp.listeners[n-1] = nil
			pp.listeners = pp.listeners[:n-1]
			break
		}
	}

	pp.profileMetrics.setSubscribers(len(pp.listeners))
}

func (pp *profilePublisher) update(profile *sp.ServiceProfile) {
	pp.Lock()
	defer pp.Unlock()
	pp.log.Debug("Updating profile")

	pp.profile = profile
	for _, listener := range pp.listeners {
		listener.Update(profile)
	}

	pp.profileMetrics.incUpdates()
}
