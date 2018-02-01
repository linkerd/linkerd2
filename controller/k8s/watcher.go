package k8s

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
)

var (
	initializationTimeout = 30 * time.Second
	sleepBetweenChecks    = 500 * time.Millisecond
)

type resourceToWatch interface {
	LastSyncResourceVersion() string
}

type watcher struct {
	resource     resourceToWatch
	resourceType string
	timeout      time.Duration
}

func newWatcher(resource resourceToWatch, resourceType string) *watcher {
	return &watcher{
		resource:     resource,
		resourceType: resourceType,
		timeout:      initializationTimeout,
	}
}

func (w *watcher) run() error {
	timedOut := make(chan struct{}, 1)
	defer close(timedOut)
	initialized := make(chan struct{}, 1)
	defer close(initialized)

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
		return fmt.Errorf("[%s watcher] timed out", w.resourceType)
	}
}
