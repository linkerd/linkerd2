package k8s

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
)

var (
	initializationTimeout = 5 * time.Second
	sleepBetweenChecks    = 100 * time.Millisecond
)

type watcher interface {
	LastSyncResourceVersion() string
}

func initializeWatcher(w watcher) error {
	timedOut := make(chan struct{}, 1)
	defer close(timedOut)
	initialized := make(chan struct{}, 1)
	defer close(initialized)

	go func() {
		for {
			select {
			case <-timedOut:
				log.Warn("Watcher timed out")
				return
			case <-time.Tick(sleepBetweenChecks):
				if w.LastSyncResourceVersion() != "" {
					log.Info("Watcher initialized")
					initialized <- struct{}{}
					return
				}
				log.Debug("Waiting for watcher to initialize")
			}
		}
	}()

	select {
	case <-initialized:
		return nil
	case <-time.After(initializationTimeout):
		timedOut <- struct{}{}
		return fmt.Errorf("Watcher initialization timed out")
	}
}
