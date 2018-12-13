package tls

import (
	"bytes"
	"encoding/pem"
	"fmt"
)

const (
	KeyTypeRSA   = "rsa"
	KeyTypeECDSA = "ecdsa"
)

// PEMEncodeCert returns the PEM encoding of cert.
func PEMEncodeCert(cert []byte) ([]byte, error) {
	buf := &bytes.Buffer{}
	err := pem.Encode(buf, &pem.Block{Type: "CERTIFICATE", Bytes: cert})
	return buf.Bytes(), err
}

// PEMEncodeKey returns the PEM encoding of key.
func PEMEncodeKey(key []byte, keyType string) ([]byte, error) {
	pemBlock := &pem.Block{Bytes: key}
	switch keyType {
	case KeyTypeRSA:
		pemBlock.Type = "RSA PRIVATE KEY"
	case KeyTypeECDSA:
		pemBlock.Type = "EC PRIVATE KEY"
	default:
		return nil, fmt.Errorf("Unknown key type")
	}

	buf := &bytes.Buffer{}
	err := pem.Encode(buf, pemBlock)
	return buf.Bytes(), err
}
