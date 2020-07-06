package tls

import (
	"testing"
)

func TestPrivateKeyParsing(t *testing.T) {
	if _, err := DecodePEMKey(""); err == nil {
		t.Fatalf("Empty private key should fail to parse")
	}
	if _, err := DecodePEMKey("BEGIN EC PRIVATE KEY\nafjlakjflaksdjf\nEND EC PRIVATE KEY"); err == nil {
		t.Fatalf("Invalid PKCS#1 ECDSA private key should fail to parse")
	}
	if _, err := DecodePEMKey("BEGIN RSA PRIVATE KEY\nafjlakjflaksdjf\nEND RSA PRIVATE KEY"); err == nil {
		t.Fatalf("Invalid PKCS#1 RSA private key should fail to parse")
	}
	if _, err := DecodePEMKey("BEGIN PRIVATE KEY\nafjlakjflaksdjf\nEND PRIVATE KEY"); err == nil {
		t.Fatalf("Invalid PKCS#8 private key should fail to parse")
	}
	ecPkcs8 := "-----BEGIN PRIVATE KEY-----\nMIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgDZUgDvKixfLi8cK8\n/TFLY97TDmQV3J2ygPpvuI8jSdihRANCAARRN3xgbPIR83dr27UuDaf2OJezpEJx\nUC3v06+FD8MUNcRAboqt4akehaNNSh7MMZI+HdnsM4RXN2y8NePUQsPL\n-----END PRIVATE KEY-----"
	if _, err := DecodePEMKey(ecPkcs8); err != nil {
		t.Fatalf("Failed to parse PKCS#8 encoded ECDSA private key: %s", err)
	}
	rsaPkcs8 := "-----BEGIN PRIVATE KEY-----\nMIIBVgIBADANBgkqhkiG9w0BAQEFAASCAUAwggE8AgEAAkEAq7BFUpkGp3+LQmlQ\nYx2eqzDV+xeG8kx/sQFV18S5JhzGeIJNA72wSeukEPojtqUyX2J0CciPBh7eqclQ\n2zpAswIDAQABAkAgisq4+zRdrzkwH1ITV1vpytnkO/NiHcnePQiOW0VUybPyHoGM\n/jf75C5xET7ZQpBe5kx5VHsPZj0CBb3b+wSRAiEA2mPWCBytosIU/ODRfq6EiV04\nlt6waE7I2uSPqIC20LcCIQDJQYIHQII+3YaPqyhGgqMexuuuGx+lDKD6/Fu/JwPb\n5QIhAKthiYcYKlL9h8bjDsQhZDUACPasjzdsDEdq8inDyLOFAiEAmCr/tZwA3qeA\nZoBzI10DGPIuoKXBd3nk/eBxPkaxlEECIQCNymjsoI7GldtujVnr1qT+3yedLfHK\nsrDVjIT3LsvTqw==\n-----END PRIVATE KEY-----"
	if _, err := DecodePEMKey(rsaPkcs8); err != nil {
		t.Fatalf("Failed to parse PKCS#8 encoded RSA private key: %s", err)
	}
}
