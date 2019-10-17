package identity

import (
	"context"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
)

const dataDirectoryLnName = "..data"

// FsCredsWatcher is used to monitor tls credentials on the filesystem
type FsCredsWatcher struct {
	issuerPath string
	EventChan  chan<- struct{}
	ErrorChan  chan<- error
}

// NewFsCredsWatcher constructs a FsCredsWatcher instance
func NewFsCredsWatcher(issuerPath string, issuerEvent chan<- struct{}, issuerError chan<- error) *FsCredsWatcher {
	return &FsCredsWatcher{issuerPath, issuerEvent, issuerError}
}

// StartWatching starts watching the filesystem for cert updates
func (fscw *FsCredsWatcher) StartWatching(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	// no point of proceeding if we fail to watch this
	if err := watcher.Add(fscw.issuerPath); err != nil {
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
				event.Name == filepath.Join(fscw.issuerPath, dataDirectoryLnName) {
				fscw.EventChan <- struct{}{}
			}
		case err := <-watcher.Errors:
			fscw.ErrorChan <- err
			log.Warnf("Error while watching %s: %s", fscw.issuerPath, err)
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
