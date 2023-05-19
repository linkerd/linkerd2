package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
)

const (
	envDir          = "LINKERD2_PROXY_IDENTITY_DIR"
	envLocalName    = "LINKERD2_PROXY_IDENTITY_LOCAL_NAME"
	envTrustAnchors = "LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS"
)

func main() {
	dir := os.Getenv(envDir)
	keyPath, csrPath, err := checkEndEntityDir(dir)
	if err != nil {
		log.Fatalf("Invalid end-entity directory: %s", err)
	}

	if _, err := loadVerifier(os.Getenv(envTrustAnchors)); err != nil {
		log.Fatalf("Failed to load trust anchors: %s", err)
	}

	key, err := generateAndStoreKey(keyPath)
	if err != nil {
		log.Fatal(err.Error())
	}

	name := os.Getenv(envLocalName)
	if _, err := generateAndStoreCSR(csrPath, name, key); err != nil {
		log.Fatal(err.Error())
	}

	runProxy()
}

func loadVerifier(pem string) (verify x509.VerifyOptions, err error) {
	if pem == "" {
		err = fmt.Errorf("'%s' must be set", envTrustAnchors)
		return
	}

	verify.Roots, err = tls.DecodePEMCertPool(pem)
	return
}

// checkEndEntityDir checks that the provided directory path exists and is
// suitable to write key material to, returning the key and CSR paths.
//
// If the directory does not exist, we assume that the directory was specified
// incorrectly and return an error. In practice this directory should be tmpfs
// so that credentials are not written to disk, so we do not want to create new
// directories here.
//
// If the key and/or CSR paths refer to existing files, it will be logged and
// the credentials will be recreated.
func checkEndEntityDir(dir string) (string, string, error) {
	if dir == "" {
		return "", "", errors.New("no end entity directory specified")
	}

	s, err := os.Stat(dir)
	if err != nil {
		return "", "", err
	}
	if !s.IsDir() {
		return "", "", fmt.Errorf("not a directory: %s", dir)
	}

	keyPath := filepath.Join(dir, "key.p8")
	if err = checkNotExists(keyPath); err != nil {
		log.Infof("Found pre-existing key: %s", keyPath)
	}

	csrPath := filepath.Join(dir, "csr.der")
	if err = checkNotExists(csrPath); err != nil {
		log.Infof("Found pre-existing CSR: %s", csrPath)
	}

	return keyPath, csrPath, nil
}

func checkNotExists(p string) (err error) {
	_, err = os.Stat(p)
	if err == nil {
		err = fmt.Errorf("already exists: %s", p)
	} else if os.IsNotExist(err) {
		err = nil
	}
	return
}

func generateAndStoreKey(p string) (key *ecdsa.PrivateKey, err error) {
	// Generate a private key and store it read-only. This is written to the
	// file-system so that the proxy may read this key at startup. The
	// destination path should generally be tmpfs so that the key material is
	// not written to disk.
	key, err = tls.GenerateKey()
	if err != nil {
		return
	}

	pemb := tls.EncodePrivateKeyP8(key)
	err = os.WriteFile(p, pemb, 0600)
	return
}

func generateAndStoreCSR(p, id string, key *ecdsa.PrivateKey) ([]byte, error) {
	if id == "" {
		return nil, errors.New("a non-empty identity is required")
	}

	if err := validation.IsFullyQualifiedDomainName(field.NewPath(""), id).ToAggregate(); err != nil {
		return nil, fmt.Errorf("%s a fully qualified DNS name is required", id)
	}

	csr := x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: id},
		DNSNames: []string{id},
	}
	csrb, err := x509.CreateCertificateRequest(rand.Reader, &csr, key)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR: %w", err)
	}

	if err = os.WriteFile(p, csrb, 0600); err != nil {
		return nil, fmt.Errorf("failed to write CSR: %w", err)
	}

	return csrb, nil
}

func runProxy() {
	// The input arguments are static.
	//nolint:gosec
	err := syscall.Exec("/usr/lib/linkerd/linkerd2-proxy", []string{}, os.Environ())
	if err != nil {
		log.Fatalf("Failed to run proxy: %s", err)
	}
}
