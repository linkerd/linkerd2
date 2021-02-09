package tls

import (
	"context"
	"crypto/tls"
	"fmt"
	"path/filepath"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

const dataDirectoryLnName = "..data"

// FsCredsWatcher is used to monitor tls credentials on the filesystem
type FsCredsWatcher struct {
	certRootPath string
	certFilePath string
	keyFilePath  string
	EventChan    chan<- struct{}
	ErrorChan    chan<- error
}

// NewFsCredsWatcher constructs a FsCredsWatcher instance
func NewFsCredsWatcher(certRootPath string, updateEvent chan<- struct{}, errEvent chan<- error) *FsCredsWatcher {
	return &FsCredsWatcher{certRootPath, "", "", updateEvent, errEvent}
}

// WithFilePaths completes the FsCredsWatcher instance with the cert and key files locations
func (fscw *FsCredsWatcher) WithFilePaths(certFilePath, keyFilePath string) *FsCredsWatcher {
	fscw.certFilePath = certFilePath
	fscw.keyFilePath = keyFilePath
	return fscw
}

// StartWatching starts watching the filesystem for cert updates
func (fscw *FsCredsWatcher) StartWatching(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	// no point of proceeding if we fail to watch this
	if err := watcher.Add(fscw.certRootPath); err != nil {
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
				event.Name == filepath.Join(fscw.certRootPath, dataDirectoryLnName) {
				fscw.EventChan <- struct{}{}
			}
		case err := <-watcher.Errors:
			fscw.ErrorChan <- err
			log.Warnf("Error while watching %s: %s", fscw.certRootPath, err)
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

// UpdateCert reads the cert and key files and stores the key pair in certVal
func (fscw *FsCredsWatcher) UpdateCert(certVal *atomic.Value) error {
	creds, err := ReadPEMCreds(fscw.keyFilePath, fscw.certFilePath)
	if err != nil {
		return fmt.Errorf("failed to read cert from disk: %s", err)
	}

	certPEM := creds.EncodePEM()
	keyPEM := creds.EncodePrivateKeyPEM()
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return err
	}
	certVal.Store(&cert)
	return nil
}

// ProcessEvents reads from the update and error channels and reloads the certs when necessary
func (fscw *FsCredsWatcher) ProcessEvents(
	log *logrus.Entry,
	certVal *atomic.Value,
	updateEvent <-chan struct{},
	errEvent <-chan error,
) {
	for {
		select {
		case <-updateEvent:
			if err := fscw.UpdateCert(certVal); err != nil {
				log.Warnf("Skipping update as cert could not be read from disk: %s", err)
			} else {
				log.Infof("Updated certificate")
			}
		case err := <-errEvent:
			log.Warnf("Received error from fs watcher: %s", err)
		}
	}
}
