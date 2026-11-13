package issuercerts

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"
)

func ecdsaKey(t *testing.T, curve elliptic.Curve) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}
	return key
}

func newCert(t *testing.T, pub any, parent *x509.Certificate, parentKey crypto.Signer, sigAlg x509.SignatureAlgorithm) *x509.Certificate {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
		SignatureAlgorithm:    sigAlg,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, pub, parentKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}
	return cert
}

func newSelfSignedCert(t *testing.T, pub any, priv crypto.Signer, sigAlg x509.SignatureAlgorithm) *x509.Certificate {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
		SignatureAlgorithm:    sigAlg,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}
	return cert
}

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

func TestCheckIssuerCertAlgoRequirements(t *testing.T) {
	rsa2048Key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	rsa4096Key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	p256Key := ecdsaKey(t, elliptic.P256())
	p384Key := ecdsaKey(t, elliptic.P384())
	p521Key := ecdsaKey(t, elliptic.P521())

	rsa2048CA := newSelfSignedCert(t, &rsa2048Key.PublicKey, rsa2048Key, x509.SHA256WithRSA)
	rsa4096CA := newSelfSignedCert(t, &rsa4096Key.PublicKey, rsa4096Key, x509.SHA512WithRSA)
	p384CA := newSelfSignedCert(t, &p384Key.PublicKey, p384Key, x509.ECDSAWithSHA384)
	p521CA := newSelfSignedCert(t, &p521Key.PublicKey, p521Key, x509.ECDSAWithSHA512)

	for _, tc := range []struct {
		name    string
		cert    *x509.Certificate
		wantErr bool
		wantMsg string
	}{
		{
			name: "accepts ECDSA P-256 issuer signed with ECDSA SHA-256",
			cert: newSelfSignedCert(t, &p256Key.PublicKey, p256Key, x509.ECDSAWithSHA256),
		},
		{
			name: "accepts ECDSA P-256 issuer signed with ECDSA SHA-384",
			cert: newCert(t, &p256Key.PublicKey, p384CA, p384Key, x509.ECDSAWithSHA384),
		},
		{
			name: "accepts ECDSA P-256 issuer signed with ECDSA SHA-512",
			cert: newCert(t, &p256Key.PublicKey, p521CA, p521Key, x509.ECDSAWithSHA512),
		},
		{
			name: "accepts ECDSA P-256 issuer signed with SHA-256 RSA",
			cert: newCert(t, &p256Key.PublicKey, rsa2048CA, rsa2048Key, x509.SHA256WithRSA),
		},
		{
			name: "accepts ECDSA P-256 issuer signed with SHA-384 RSA",
			cert: newCert(t, &p256Key.PublicKey, rsa2048CA, rsa2048Key, x509.SHA384WithRSA),
		},
		{
			name: "accepts ECDSA P-256 issuer signed with SHA-512 RSA",
			cert: newCert(t, &p256Key.PublicKey, rsa4096CA, rsa4096Key, x509.SHA512WithRSA),
		},
		{
			name:    "rejects ECDSA P-256 issuer signed with SHA-1 RSA",
			cert:    withSigAlg(t, newCert(t, &p256Key.PublicKey, rsa2048CA, rsa2048Key, x509.SHA256WithRSA), x509.SHA1WithRSA),
			wantErr: true,
			wantMsg: "SHA-256, SHA-384 or SHA-512",
		},
		{
			name:    "rejects ECDSA P-256 issuer signed with ECDSA SHA-1",
			cert:    withSigAlg(t, newSelfSignedCert(t, &p256Key.PublicKey, p256Key, x509.ECDSAWithSHA256), x509.ECDSAWithSHA1),
			wantErr: true,
			wantMsg: "SHA-256, SHA-384 or SHA-512",
		},
		{
			name:    "rejects ECDSA P-384 issuer key",
			cert:    newSelfSignedCert(t, &p384Key.PublicKey, p384Key, x509.ECDSAWithSHA384),
			wantErr: true,
		},
		{
			name:    "rejects RSA issuer key",
			cert:    rsaSelfSignedCert(t, 2048),
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckIssuerCertAlgoRequirements(tc.cert)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error message should mention %q, got: %s", tc.wantMsg, err.Error())
			}
		})
	}
}

func TestCheckTrustAnchorAlgoRequirementsSignatureAlgorithms(t *testing.T) {
	rsa2048Key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	rsa4096Key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	for _, tc := range []struct {
		name    string
		cert    *x509.Certificate
		wantErr bool
	}{
		{
			name: "accepts RSA trust anchor signed with SHA-256",
			cert: newSelfSignedCert(t, &rsa2048Key.PublicKey, rsa2048Key, x509.SHA256WithRSA),
		},
		{
			name: "accepts RSA trust anchor signed with SHA-384",
			cert: newSelfSignedCert(t, &rsa2048Key.PublicKey, rsa2048Key, x509.SHA384WithRSA),
		},
		{
			name: "accepts RSA trust anchor signed with SHA-512",
			cert: newSelfSignedCert(t, &rsa4096Key.PublicKey, rsa4096Key, x509.SHA512WithRSA),
		},
		{
			name:    "rejects RSA trust anchor signed with SHA-1",
			cert:    withSigAlg(t, newSelfSignedCert(t, &rsa2048Key.PublicKey, rsa2048Key, x509.SHA256WithRSA), x509.SHA1WithRSA),
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckTrustAnchorAlgoRequirements(tc.cert)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// withSigAlg returns a copy of cert reporting the given signature algorithm.
// The check functions only read the public key and signature algorithm, so
// this covers algorithms Go refuses to sign with (SHA-1, MD5) without
// needing a real signature.
func withSigAlg(t *testing.T, cert *x509.Certificate, alg x509.SignatureAlgorithm) *x509.Certificate {
	t.Helper()
	c := *cert
	c.SignatureAlgorithm = alg
	return &c
}
