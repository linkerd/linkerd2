package issuercerts

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"
)

func rsaSelfSignedCert(t *testing.T, bits int) *x509.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}
	return cert
}

func TestCheckTrustAnchorAlgoRequirementsRSA(t *testing.T) {
	t.Run("rejects unsupported RSA key size with correct error message", func(t *testing.T) {
		cert := rsaSelfSignedCert(t, 1024)
		err := CheckTrustAnchorAlgoRequirements(cert)
		if err == nil {
			t.Fatal("expected error for 1024-bit RSA key, got nil")
		}
		if !strings.Contains(err.Error(), "2048 or 4096") {
			t.Errorf("error message should mention '2048 or 4096', got: %s", err.Error())
		}
	})

	t.Run("accepts 2048-bit RSA key", func(t *testing.T) {
		cert := rsaSelfSignedCert(t, 2048)
		if err := CheckTrustAnchorAlgoRequirements(cert); err != nil {
			t.Errorf("unexpected error for 2048-bit RSA key: %v", err)
		}
	})
}
