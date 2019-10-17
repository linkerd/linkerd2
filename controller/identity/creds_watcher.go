package identity

import (
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"

	"github.com/linkerd/linkerd2/pkg/k8s"

	"github.com/fsnotify/fsnotify"
	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
)

const dataDirectoryLnName = "..data"

// FsCredsWatcher is used to monitor tls credentials on the filesystem
type FsCredsWatcher struct {
	issuerPath, expectedName string
	roots                    *x509.CertPool
	validity                 tls.Validity
	issuerChan               chan tls.Issuer
}

// NewFsCredsWatcher constructs a FsCredsWatcher instance
func NewFsCredsWatcher(issuerPath, expectedName string, roots *x509.CertPool, validity tls.Validity) *FsCredsWatcher {
	ch := make(chan tls.Issuer, 100)
	return &FsCredsWatcher{issuerPath, expectedName, roots, validity, ch}
}

// Creds gives back a chan from which new issuers can be read
func (fscw *FsCredsWatcher) Creds() <-chan tls.Issuer {
	return fscw.issuerChan
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func (fscw *FsCredsWatcher) credPaths() (string, string) {
	externalIssuerPathCrt := filepath.Join(fscw.issuerPath, k8s.IdentityIssuerCrtNameExternal)
	externalIssuerPathKey := filepath.Join(fscw.issuerPath, k8s.IdentityIssuerKeyNameExternal)
	issuerPathCrt := filepath.Join(fscw.issuerPath, k8s.IdentityIssuerCrtNameExternal)
	issuerPathKey := filepath.Join(fscw.issuerPath, k8s.IdentityIssuerKeyNameExternal)

	if fileExists(externalIssuerPathCrt) && fileExists(externalIssuerPathKey) {
		return externalIssuerPathKey, externalIssuerPathCrt
	}
	return issuerPathKey, issuerPathCrt
}

func (fscw *FsCredsWatcher) loadCredentials() (*tls.CA, error) {

	keyPath, crtPath := fscw.credPaths()
	creds, err := tls.ReadPEMCreds(
		keyPath,
		crtPath,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to read CA from %s: %s", fscw.issuerPath, err)
	}

	if err := creds.Crt.Verify(fscw.roots, fscw.expectedName); err != nil {
		return nil, fmt.Errorf("failed to verify issuer credentials for '%s' with trust anchors: %s", fscw.expectedName, err)
	}

	log.Infof("Loaded issuer cert: %s", creds.EncodeCertificatePEM())
	return tls.NewCA(*creds, fscw.validity), nil
}

// StartWatching starts watching the filesystem for cert updates
func (fscw *FsCredsWatcher) StartWatching() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				log.Debugf("Received event: %v", event)
				// Watching the folder for create events as this indicates
				// that the secret has been updated.
				if event.Op&fsnotify.Create == fsnotify.Create &&
					event.Name == filepath.Join(fscw.issuerPath, dataDirectoryLnName) {
					log.Debugf("Reloading issuer certificate")
					newCa, err := fscw.loadCredentials()
					if err != nil {
						log.Fatalf("Problem reloading %s", err)
					}
					fscw.issuerChan <- newCa
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Warnf("Error while watching %s: %s", fscw.issuerPath, err)
			}
		}
	}()

	err = watcher.Add(fscw.issuerPath)
	if err != nil {
		log.Fatal(err)
	}

	initialCredentials, err := fscw.loadCredentials()
	if err != nil {
		return fmt.Errorf("failed to read initial credentials: %s", err)
	}
	fscw.issuerChan <- initialCredentials
	return nil
}
