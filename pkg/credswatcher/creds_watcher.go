package credswatcher

import (
	"context"
	"fmt"
	"path/filepath"

	v1 "k8s.io/api/core/v1"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
)

const dataDirectoryLnName = "..data"

// FsCredsWatcher is used to monitor tls credentials on the filesystem
type FsCredsWatcher struct {
	watchPath string
	EventChan chan<- struct{}
	ErrorChan chan<- error
}

// NewFsCredsWatcher constructs a FsCredsWatcher instance
func NewFsCredsWatcher(watchPath string, eventCh chan<- struct{}, errCh chan<- error) *FsCredsWatcher {
	return &FsCredsWatcher{watchPath, eventCh, errCh}
}

// StartWatching starts watching the filesystem for cert updates
func (fscw *FsCredsWatcher) StartWatching(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	// no point of proceeding if we fail to watch this
	if err := watcher.Add(fscw.watchPath); err != nil {
		return err
	}

LOOP:
	for {
		select {
		case event := <-watcher.Events:
			log.Debugf("Received event: %v", event)
			// Watching the folder for create events as this indicates
			// that the secret has been updated.
			if event.Op&fsnotify.Create == fsnotify.Create &&
				event.Name == filepath.Join(fscw.watchPath, dataDirectoryLnName) {
				fscw.EventChan <- struct{}{}
			}
		case err := <-watcher.Errors:
			fscw.ErrorChan <- err
			log.Warnf("Error while watching %s: %s", fscw.watchPath, err)
			break LOOP
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				fscw.ErrorChan <- err
			}
			break LOOP
		}
	}

	return nil
}

// WatchCredChanges watches FsCredsWatcher events for changes and calls onChangeFunc and onErrorFunc
func WatchCredChanges(ctx context.Context, path string, onChangeFunc func() (string, string, error), recordEventFunc func(eventType, reason, message string)) {
	eventCh := make(chan struct{})
	errorCh := make(chan error)

	fswatcher := NewFsCredsWatcher(path, eventCh, errorCh)
	go func() {
		if err := fswatcher.StartWatching(ctx); err != nil {
			log.Fatalf("Failed to start creds watcher: %s", err)
		}
	}()

	go func() {
		for {
			select {
			case <-eventCh:
				if message, reason, err := onChangeFunc(); err != nil {
					message := fmt.Sprintf("%s: %s", message, err)
					log.Warn(message)
					recordEventFunc(v1.EventTypeWarning, reason, message)
				} else {
					log.Infof(message)
					recordEventFunc(v1.EventTypeNormal, reason, message)
				}
			case err := <-errorCh:
				log.Warnf("Received error from fs watcher: %s", err)
			}
		}
	}()
}
