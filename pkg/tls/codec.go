package tls

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// === ENCODE ===

// EncodeCertificatesPEM encodes the collection of provided certificates as
// a text blob of PEM-encoded certificates.
func EncodeCertificatesPEM(crts ...*x509.Certificate) string {
	buf := bytes.Buffer{}
	for _, c := range crts {
		encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	}
	return buf.String()
}

// EncodePrivateKeyPEM encodes the provided key as PEM-encoded text
func EncodePrivateKeyPEM(k *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		return nil, err
	}

	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), nil
}

// EncodePrivateKeyP8 encodes the provided key as PEM-encoded text
func EncodePrivateKeyP8(k *ecdsa.PrivateKey) []byte {
	p8, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		panic("ECDSA keys must be encodeable as PKCS8")
	}
	return p8
}

func encode(buf *bytes.Buffer, blk *pem.Block) {
	if err := pem.Encode(buf, blk); err != nil {
		panic("encoding to memory must not fail")
	}
}

// === DECODE ===

// DecodePEMKey parses a PEM-encoded private key from the named path.
func DecodePEMKey(txt string) (GenericPrivateKey, error) {
	block, _ := pem.Decode([]byte(txt))
	if block == nil {
		return nil, errors.New("not PEM-encoded")
	}
	switch block.Type {
	case "EC PRIVATE KEY":
		k, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return privateKeyEC{k}, nil
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return privateKeyRSA{k}, nil
	default:
		return nil, fmt.Errorf("unsupported block type: '%s'", block.Type)
	}
}

// DecodePEMCertificates parses a string containing PEM-encoded certificates.
func DecodePEMCertificates(txt string) (certs []*x509.Certificate, err error) {
	buf := []byte(txt)
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

// DecodePEMCertPool parses a string containing PE-encoded certificates into a CertPool.
func DecodePEMCertPool(txt string) (pool *x509.CertPool, err error) {
	certs, err := DecodePEMCertificates(txt)
	if err != nil {
		return
	}
	if len(certs) == 0 {
		err = errors.New("no certificates found")
		return
	}

	pool = x509.NewCertPool()
	for _, c := range certs {
		pool.AddCert(c)
	}

	return
}

func decodeCertificatePEM(crtb []byte) (*x509.Certificate, []byte, error) {
	block, crtb := pem.Decode(crtb)
	if block == nil {
		return nil, crtb, errors.New("not a PEM certificate")
	}
	if block.Type != "CERTIFICATE" {
		return nil, nil, nil
	}
	c, err := x509.ParseCertificate(block.Bytes)
	return c, crtb, err
}
