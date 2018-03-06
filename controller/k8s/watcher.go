package k8s

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
)

const maxInt = int(^uint(0) >> 1)

var (
	initializationTimeout  = 60 * time.Second
	sleepBetweenChecks     = 500 * time.Millisecond
	durationBetweenBackoff = 1 * time.Second
	watchBackoffFactor     = 1.5
	backoffSteps           = maxInt // Try looping for the maximum number of steps possible
)

type watchInitializer func(stopCh <-chan struct{}) error
type resourceToWatch interface {
	LastSyncResourceVersion() string
}

type watcher struct {
	resource     resourceToWatch
	resourceType string
	timeout      time.Duration
	watchInit    watchInitializer
	stopCh       chan struct{}
}

func newWatcher(resource resourceToWatch, resourceType string, watchInit watchInitializer, stop chan struct{}) *watcher {
	return &watcher{
		resource:     resource,
		resourceType: resourceType,
		timeout:      initializationTimeout,
		watchInit:    watchInit,
		stopCh:       stop,
	}
}

func (w *watcher) run() error {
	timedOut := make(chan struct{}, 1)
	defer close(timedOut)
	initialized := make(chan struct{}, 1)
	defer close(initialized)

	go wait.ExponentialBackoff(wait.Backoff{
		Duration: durationBetweenBackoff,
		Factor:   watchBackoffFactor,
		Steps:    backoffSteps,
	}, func() (bool, error) {
		err := w.watchInit(w.stopCh)
		if err != nil {
			log.Errorf("Error establishing watch in [%s watcher]. Retrying", w.resourceType)
			return false, nil
		}
		return true, err
	})

	go func() {
		for {
			select {
			case <-timedOut:
				log.Warnf("[%s watcher] timed out", w.resourceType)
				return
			case <-time.Tick(sleepBetweenChecks):
				if w.resource.LastSyncResourceVersion() != "" {
					log.Infof("[%s watcher] initialized", w.resourceType)
					initialized <- struct{}{}
					return
				}
				log.Debugf("[%s watcher] waiting for initialization", w.resourceType)
			}
		}
	}()

	select {
	case <-initialized:
		return nil
	case <-time.After(w.timeout):
		timedOut <- struct{}{}
		w.stopCh <- struct{}{}
		return fmt.Errorf("[%s watcher] timed out", w.resourceType)
	}
}
