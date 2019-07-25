package tls

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
)

type (
	// PrivateKeyEC wraps an EC private key
	privateKeyEC struct {
		*ecdsa.PrivateKey
	}

	// PrivateKeyRSA wraps an RSA private key
	privateKeyRSA struct {
		*rsa.PrivateKey
	}

	// GenericPrivateKey represents either an EC or an RSA private key
	GenericPrivateKey interface {
		matchesCertificate(*x509.Certificate) bool
		marshal() ([]byte, error)
	}

	// Cred is a container for a certificate, trust chain, and private key.
	Cred struct {
		PrivateKey GenericPrivateKey
		Crt
	}

	// Crt is a container for a certificate and trust chain.
	//
	// The trust chain stores all issuer certificates from the root at the head to
	// the direct issuer at the tail.
	Crt struct {
		Certificate *x509.Certificate
		TrustChain  []*x509.Certificate
	}
)

func (k privateKeyEC) matchesCertificate(c *x509.Certificate) bool {
	pub, ok := c.PublicKey.(*ecdsa.PublicKey)
	return ok && pub.X.Cmp(k.X) == 0 && pub.Y.Cmp(k.Y) == 0
}

func (k privateKeyEC) marshal() ([]byte, error) {
	return x509.MarshalECPrivateKey(k.PrivateKey)
}

func (k privateKeyRSA) matchesCertificate(c *x509.Certificate) bool {
	pub, ok := c.PublicKey.(*rsa.PublicKey)
	return ok && pub.N.Cmp(k.N) == 0 && pub.E == k.E
}

func (k privateKeyRSA) marshal() ([]byte, error) {
	return x509.MarshalPKCS1PrivateKey(k.PrivateKey), nil
}

// validCredOrPanic creates a  Cred, panicking if the key does not match the certificate.
func validCredOrPanic(ecKey *ecdsa.PrivateKey, crt Crt) Cred {
	k := privateKeyEC{ecKey}
	if !k.matchesCertificate(crt.Certificate) {
		panic("Cert's public key does not match private key")
	}
	return Cred{Crt: crt, PrivateKey: k}
}

// CertPool returns a CertPool containing this Crt.
func (crt *Crt) CertPool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(crt.Certificate)
	for _, c := range crt.TrustChain {
		p.AddCert(c)
	}
	return p
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

// EncodePEM emits a certificate and trust chain as a
// series of PEM-encoded certificates from leaf to root.
func (crt *Crt) EncodePEM() string {
	buf := bytes.Buffer{}
	encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: crt.Certificate.Raw})

	// Serialize certificates from leaf to root.
	n := len(crt.TrustChain)
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
func (cred *Cred) EncodePrivateKeyPEM() string {
	b, err := cred.PrivateKey.marshal()
	if err != nil {
		panic(fmt.Sprintf("Invalid private key: %s", err))
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b}))
}

// EncodePrivateKeyP8 encodes the provided key to the PKCS#8 binary form.
func (cred *Cred) EncodePrivateKeyP8() ([]byte, error) {
	return x509.MarshalPKCS8PrivateKey(cred.PrivateKey)
}

// SignCrt uses this Cred to sign a new certificate.
//
// This may fail if the Cred contains an end-entity certificate.
func (cred *Cred) SignCrt(template *x509.Certificate) (Crt, error) {
	crtb, err := x509.CreateCertificate(
		rand.Reader,
		template,
		cred.Crt.Certificate,
		template.PublicKey,
		cred.PrivateKey,
	)
	if err != nil {
		return Crt{}, err
	}

	c, err := x509.ParseCertificate(crtb)
	if err != nil {
		return Crt{}, err
	}

	crt := Crt{
		Certificate: c,
		TrustChain:  append(cred.Crt.TrustChain, cred.Crt.Certificate),
	}
	return crt, nil
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
	if !key.matchesCertificate(crt.Certificate) {
		return nil, errors.New("tls: Public and private key do not match")
	}
	return &Cred{PrivateKey: key, Crt: *crt}, nil
}

// DecodePEMCrt decodes PEM-encoded certificates from leaf to root.
func DecodePEMCrt(txt string) (*Crt, error) {
	certs, err := DecodePEMCertificates(txt)
	if err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return nil, errors.New("No certificates found")
	}

	crt := Crt{
		Certificate: certs[0],
		TrustChain:  make([]*x509.Certificate, len(certs)-1),
	}

	// The chain is read from Leaf to Root, but we store it from Root to Leaf.
	certs = certs[1:]
	for i, c := range certs {
		crt.TrustChain[len(certs)-i-1] = c
	}

	return &crt, nil
}
