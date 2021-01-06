package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
)

const (
	envDisabled     = "LINKERD2_PROXY_IDENTITY_DISABLED"
	envTrustAnchors = "LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS"
)

func main() {
	cmd := flag.NewFlagSet("public-api", flag.ExitOnError)

	name := cmd.String("name", "", "identity name")
	dir := cmd.String("dir", "", "directory under which credentials are written")

	flags.ConfigureAndParse(cmd, os.Args[1:])

	if os.Getenv(envDisabled) != "" {
		log.Debug("Identity disabled.")
		os.Exit(0)
	}

	keyPath, csrPath, err := checkEndEntityDir(*dir)
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

	if _, err := generateAndStoreCSR(csrPath, *name, key); err != nil {
		log.Fatal(err.Error())
	}
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
	err = ioutil.WriteFile(p, pemb, 0600)
	return
}

func generateAndStoreCSR(p, id string, key *ecdsa.PrivateKey) ([]byte, error) {
	// TODO do proper DNS name validation.
	if id == "" {
		return nil, errors.New("a non-empty identity is required")
	}

	csr := x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: id},
		DNSNames: []string{id},
	}
	csrb, err := x509.CreateCertificateRequest(rand.Reader, &csr, key)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR: %s", err)
	}

	if err = ioutil.WriteFile(p, csrb, 0600); err != nil {
		return nil, fmt.Errorf("failed to write CSR: %s", err)
	}

	return csrb, nil
}
