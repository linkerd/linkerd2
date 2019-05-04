package tls

import (
	"reflect"
	"testing"
)

func TestParseRootCA(t *testing.T) {
	commonName := "example.com"
	expected, err := GenerateRootCAWithDefaults(commonName)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	var (
		rootCA = []byte(expected.Cred.EncodePEM())
		key    = []byte(expected.Cred.EncodePrivateKeyPEM())
	)

	actual, err := ParseRootCA(rootCA, key)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	if !certificateMatchesKey(actual.Cred.Crt.Certificate, actual.Cred.PrivateKey) {
		t.Errorf("The generated x509 certificate and private key don't match")
	}

	certPool := actual.Cred.Crt.CertPool()
	if err := actual.Cred.Crt.Verify(certPool, commonName); err != nil {
		t.Errorf("Expected the generated x509 certificate to be valid for %s", commonName)
	}

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("CA mismatch. Expected %+v\n Actual %+v\n", expected, actual)
	}
}
