package main

import (
	"crypto"
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

	keyPath, keyExists, csrPath, csrExists, err := checkEndEntityDir(*dir)
	if err != nil {
		log.Fatalf("Invalid end-entity directory: %s", err)
	}

	if _, err := loadVerifier(os.Getenv(envTrustAnchors)); err != nil {
		log.Fatalf("Failed to load trust anchors: %s", err)
	}

	var key crypto.Signer
	if keyExists {
		keyb, err := ioutil.ReadFile(keyPath)
		if err != nil {
			log.Fatalf("failed to read key file: %s", err)
		}
		k, err := tls.DecodeDERKey(keyb)
		if err != nil {
			log.Fatalf("failed to decode key file: %s", err)
		}
		key = k.Signer()
	} else {
		key, err = generateAndStoreKey(keyPath)
		if err != nil {
			log.Fatal(err.Error())
		}
	}

	if !csrExists {
		if _, err := generateAndStoreCSR(csrPath, *name, key); err != nil {
			log.Fatal(err.Error())
		}
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
// suitable to write key material to, returning the Key, CSR, and Crt paths.
//
// If the directory does not exist, we assume that the wrong directory was
// specified incorrectly, instead of trying to
// create or repair the directory. In practice, this directory should be tmpfs
// so that credentials are not written to disk, so we want to be extra sensitive
// to an incorrectly specified path.
//
// If the key, CSR, and/or Crt paths refer to existing files, it is assumed that
// the proxy has been restarted and these credentials are NOT recreated.
func checkEndEntityDir(dir string) (string, bool, string, bool, error) {
	if dir == "" {
		return "", false, "", false, errors.New("no end entity directory specified")
	}

	s, err := os.Stat(dir)
	if err != nil {
		return "", false, "", false, err
	}
	if !s.IsDir() {
		return "", false, "", false, fmt.Errorf("not a directory: %s", dir)
	}

	keyPath := filepath.Join(dir, "key.p8")
	keyExists := false
	if err = checkNotExists(keyPath); err != nil {
		log.Infof("Using with pre-existing key: %s", keyPath)
		keyExists = true
	}

	csrPath := filepath.Join(dir, "csr.der")
	csrExists := false
	if err = checkNotExists(csrPath); err != nil {
		log.Infof("Using with pre-existing CSR: %s", csrPath)
		csrExists = true
	}

	return keyPath, keyExists, csrPath, csrExists, nil
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
	// Generate a private key and store it read-only (i.e. mostly for debugging). Because the file is read-only
	key, err = tls.GenerateKey()
	if err != nil {
		return
	}

	pemb := tls.EncodePrivateKeyP8(key)
	err = ioutil.WriteFile(p, pemb, 0600)
	return
}

func generateAndStoreCSR(p, id string, key crypto.Signer) ([]byte, error) {
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
