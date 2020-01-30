package tls

import (
	"crypto/x509"
	"fmt"
	"testing"
	"time"
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

	if err := crt.Verify(rootTrust, "endentity.test", time.Time{}); err != nil {
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

func TestCrtExpiry(t *testing.T) {
	root := newRoot(t)
	rootTrust := root.Cred.Crt.CertPool()

	cred, err := root.GenerateEndEntityCred("expired.test")
	if err != nil {
		t.Fatalf("failed to create end entity cred: %s", err)
	}

	crt, err := DecodePEMCrt(cred.Crt.EncodePEM())
	if err != nil {
		t.Fatalf("Failed to decode PEM Crt: %s", err)
	}

	fakeExpiryError := x509.CertificateInvalidError{Reason: x509.Expired}

	//need to remove seconds and nanoseconds for testing returned error
	now := time.Now()

	testCases := []struct {
		currentTime time.Time
		notBefore   time.Time
		notAfter    time.Time
		expected    string
	}{
		//cert not valid yet
		{
			currentTime: now,
			notAfter:    now.AddDate(0, 0, 20),
			notBefore:   now.AddDate(0, 0, 10),
			expected:    fmt.Sprintf("%s - Current Time : %s - Invalid before %s - Invalid After %s", fakeExpiryError.Error(), now, now.AddDate(0, 0, 10), now.AddDate(0, 0, 20)),
		},
		//cert has expired
		{
			currentTime: now,
			notAfter:    now.AddDate(0, 0, -10),
			notBefore:   now.AddDate(0, 0, -20),
			expected:    fmt.Sprintf("%s - Current Time : %s - Invalid before %s - Invalid After %s", fakeExpiryError.Error(), now, now.AddDate(0, 0, -20), now.AddDate(0, 0, -10)),
		},
		// cert is valid
		{
			currentTime: time.Time{},
			notAfter:    crt.Certificate.NotAfter,
			notBefore:   crt.Certificate.NotBefore,
			expected:    "",
		},
	}

	for i, tc := range testCases {
		tc := tc //pin
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			//explicitly kill the certificate
			crt.Certificate.NotBefore = tc.notBefore
			crt.Certificate.NotAfter = tc.notAfter

			if err := crt.Verify(rootTrust, "expired.test", tc.currentTime); err != nil {
				if err.Error() != tc.expected {
					t.Logf("Returned error : %s\n", err)
					t.Logf("Expected error : %s\n", tc.expected)
					t.Fatal("test case failed")
				}
			} else {
				t.Log("no error on verification")
			}
		})
	}
}
