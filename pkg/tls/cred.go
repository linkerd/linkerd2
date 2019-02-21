package tls

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
)

// Cred is a container for a certificate, trust chain, and private key.
type Cred struct {
	PrivateKey *ecdsa.PrivateKey
	*Crt
}

// Crt is a container for a certificate and trust chain.
//
// The trust chain stores all issuer certificates from root at the head direct
// issuer at the tail.
type Crt struct {
	Certificate *x509.Certificate
	TrustChain  []*x509.Certificate
}

// Verify the validity of the provided certificate
func (crt *Crt) Verify(roots *x509.CertPool, name string) error {
	i := x509.NewCertPool()
	for _, c := range crt.TrustChain {
		i.AddCert(c)
	}
	vo := x509.VerifyOptions{Roots: roots, Intermediates: i, DNSName: name}
	_, err := crt.Certificate.Verify(vo)
	return err
}

// ExtractRaw extracts the DER-encoded certificates in the Crt from leaf to root.
func (crt *Crt) ExtractRaw() [][]byte {
	chain := make([][]byte, len(crt.TrustChain)+1)
	chain[0] = crt.Certificate.Raw
	for i, c := range crt.TrustChain {
		chain[len(crt.TrustChain)-i] = c.Raw
	}
	return chain
}

// EncodeCertificateAndTrustChainPEM emits a certificate and trust chain as a
// series of PEM-encoded certificates from leaf to root.
func (crt *Crt) EncodeCertificateAndTrustChainPEM() string {
	buf := bytes.Buffer{}
	n := len(crt.TrustChain)

	// Serialize certificates from leaf to root.
	encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: crt.Certificate.Raw})
	for i := n - 1; i >= 0; i-- {
		encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: crt.TrustChain[i].Raw})
	}

	return buf.String()
}

// EncodeCertificatePEM emits the Crt's leaf certificate as PEM-encoded text.
func (crt *Crt) EncodeCertificatePEM() string {
	return EncodeCertificatesPEM(crt.Certificate)
}

// EncodePrivateKeyPEM emits the private key as PEM-encoded text.
func (cred *Cred) EncodePrivateKeyPEM() (string, error) {
	der, err := x509.MarshalECPrivateKey(cred.PrivateKey)
	if err != nil {
		return "", err
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})), nil
}

// EncodePrivateKeyP8 encodes the provided key to the PKCS#8 binary form.
func (cred *Cred) EncodePrivateKeyP8() ([]byte, error) {
	return x509.MarshalPKCS8PrivateKey(cred.PrivateKey)
}

// CreateCrt uses this Cred to sign a new certificate.
//
// This may fail if the Cred contains an end-entity certificate.
func (cred *Cred) CreateCrt(template *x509.Certificate) (*Crt, error) {
	crtb, err := x509.CreateCertificate(
		rand.Reader,
		template,
		cred.Crt.Certificate,
		cred.PrivateKey.Public(),
		cred.PrivateKey,
	)
	if err != nil {
		return nil, err
	}

	c, err := x509.ParseCertificate(crtb)
	if err != nil {
		return nil, err
	}

	crt := Crt{
		Certificate: c,
		TrustChain:  append(cred.Crt.TrustChain, cred.Crt.Certificate),
	}
	return &crt, nil
}

// ReadPEMCreds reads PEM-encoded credentials from the named files.
func ReadPEMCreds(keyPath, crtPath string) (*Cred, error) {
	keyb, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	key, err := DecodePEMKey(string(keyb))
	if err != nil {
		return nil, err
	}

	crtb, err := ioutil.ReadFile(crtPath)
	if err != nil {
		return nil, err
	}
	crt, err := DecodePEMCrt(string(crtb))
	if err != nil {
		return nil, err
	}

	return &Cred{PrivateKey: key, Crt: crt}, nil
}

// DecodePEMCrt decodes PEM-encoded certificates from leaf to root.
func DecodePEMCrt(txt string) (*Crt, error) {
	certs, err := DecodePEMCertificates(txt)
	if err != nil {
		return nil, err
	}

	crt := Crt{Certificate: certs[0]}
	certs = certs[1:]

	// The chain is read from Leaf to Root, but we store it from Root to Leaf.
	crt.TrustChain = make([]*x509.Certificate, len(certs))
	for i, c := range certs {
		crt.TrustChain[len(certs)-i-1] = c
	}

	return &crt, nil
}
