package tls

import (
	"testing"
)

func newRoot(t *testing.T) CA {
	root, err := GenerateRootCAWithDefaults(t.Name())
	if err != nil {
		t.Fatalf("failed to create CA: %s", err)
	}
	return *root
}

func TestCrtRoundtrip(t *testing.T) {
	root := newRoot(t)
	rootTrust := root.Cred.Crt.CertPool()

	cred, err := root.GenerateEndEntityCred("endentity.test")
	if err != nil {
		t.Fatalf("failed to create end entity cred: %s", err)
	}

	crt, err := DecodePEMCrt(cred.Crt.EncodePEM())
	if err != nil {
		t.Fatalf("Failed to decode PEM Crt: %s", err)
	}

	if err := crt.Verify(rootTrust, "endentity.test"); err != nil {
		t.Fatal("Failed to verify round-tripped certificate")
	}
}

func TestCredEncodeCertificateAndTrustChain(t *testing.T) {
	root, err := GenerateRootCAWithDefaults("Test Root CA")
	if err != nil {
		t.Fatalf("failed to create CA: %s", err)
	}

	cred, err := root.GenerateEndEntityCred("test end entity")
	if err != nil {
		t.Fatalf("failed to create end entity cred")
	}

	expected := EncodeCertificatesPEM(cred.Crt.Certificate, root.Cred.Crt.Certificate)
	if cred.EncodePEM() != expected {
		t.Errorf("Encoded Certificate And TrustChain does not match expected output")
	}
}
