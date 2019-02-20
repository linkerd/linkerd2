package tls

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
)

func (crt *Crt) EncodePEM() []byte {
	w := bytes.Buffer{}
	n := len(crt.TrustChain)

	// Serialize certificates from leaf to root.
	_ = pem.Encode(&w, &pem.Block{Type: "CERTIFICATE", Bytes: crt.Certificate.Raw})
	for i := n - 1; i >= 0; i-- {
		_ = pem.Encode(&w, &pem.Block{Type: "CERTIFICATE", Bytes: crt.TrustChain[i].Raw})
	}

	return w.Bytes()
}

func EncodeCertificatePEM(crts ...*x509.Certificate) []byte {
	w := bytes.Buffer{}
	for _, c := range crts {
		_ = pem.Encode(&w, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	}
	return w.Bytes()
}

func (crt *Crt) EncodeTrustChainDER() [][]byte {
	// Serialize certificates from leaf to root.
	chain := make([][]byte, len(crt.TrustChain))
	for i, c := range crt.TrustChain {
		chain[len(crt.TrustChain)-i-1] = c.Raw
	}
	return chain
}

func EncodePrivateKeyP8(k *ecdsa.PrivateKey) ([]byte, error) {
	return x509.MarshalPKCS8PrivateKey(k)
}

func EncodePrivateKeyPEM(k *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		return nil, err
	}

	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), nil
}

// ReadCertificatePEm reads a PEM-encoded certificates from the given path.
func ReadCertificatePEM(path string) (*x509.Certificate, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	crt, _, err := decodeCertificatePEM(b)
	return crt, err
}

// ReadCertificatePEM reads a PEM-encoded certificates from the given path.
func ReadTrustAnchorsPEM(path string) ([]*x509.Certificate, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return DecodeTrustAnchorsPEM(b)
}

// ReadCrtPEM reads PEM-encoded certificates from the named file
func ReadCrtPEM(path string) (Crt, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return Crt{}, err
	}
	return DecodeCrtPEM(b)
}

func ReadKeyPEM(path string) (*ecdsa.PrivateKey, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeKeyPEM(b)
}

func DecodeKeyPEM(b []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("Not PEM-encoded")
	}
	if block.Type != "EC PRIVATE KEY" {
		return nil, fmt.Errorf("Expected 'EC PRIVATE KEY'; found: '%s'", block.Type)
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

func decodeCertificatePEM(crtb []byte) (*x509.Certificate, []byte, error) {
	block, crtb := pem.Decode(crtb)
	if block == nil {
		return nil, nil, errors.New("Failed to decode PEM certificate")
	}
	if block.Type != "CERTIFICATE" {
		return nil, nil, nil
	}
	c, err := x509.ParseCertificate(block.Bytes)
	return c, crtb, err
}

// DecodeCrtPEM decodes PEM-encoded certificates from leaf to root.
func DecodeCrtPEM(buf []byte) (crt Crt, err error) {
	certs, err := decodeCertificatesPEM(buf)
	if err != nil {
		return
	}

	crt.Certificate = certs[0]
	certs = certs[1:]

	// The chain is read from Leaf to Root, but we store it from Root to Leaf.
	crt.TrustChain = make([]*x509.Certificate, len(certs))
	for i, c := range certs {
		crt.TrustChain[len(certs)-i-1] = c
	}
	return
}

// DecodeTrustAnchorsPEM decodes PEM-encoded certificates.
func DecodeTrustAnchorsPEM(buf []byte) ([]*x509.Certificate, error) {
	return decodeCertificatesPEM(buf)
}

func decodeCertificatesPEM(buf []byte) (certs []*x509.Certificate, err error) {
	for len(buf) > 0 {
		var c *x509.Certificate
		c, buf, err = decodeCertificatePEM(buf)
		if err != nil {
			return
		}
		if c == nil {
			continue // not a CERTIFICATE, skip
		}
		certs = append(certs, c)
	}
	return
}
