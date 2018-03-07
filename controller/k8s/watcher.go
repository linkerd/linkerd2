package k8s

import (
	"fmt"
	"math"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	initializationTimeout = 30 * time.Second
	sleepBetweenChecks    = 500 * time.Millisecond
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
		Duration: 100 * time.Millisecond,
		Factor:   1.5,
		Steps:    int(math.MaxInt32),
	}, func() (bool, error) {
		err := w.watchInit(w.stopCh)
		if err != nil {
			log.Errorf("[%s watcher] error establishing watch: %s", w.resourceType, err)
		}
		log.Debugf("[%s watcher] retrying", w.resourceType)
		return false, nil
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
