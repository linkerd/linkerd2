package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"
)

// Issuing certificates concurrently is not supported.
type CA struct {
	// validity is the duration for which issued certificates are valid. This
	// is approximately cert.NotAfter - cert.NotBefore with some additional
	// allowance for clock skew.
	//
	// Currently this is used for the CA's validity too, but nothing should
	// assume that the CA's validity period is the same as issued certificates'
	// validity.
	validity time.Duration

	// clockSkewAllocance is the maximum supported clock skew. Everything that
	// processes the certificates must have a system clock that is off by no
	// more than this allowance in either direction.
	clockSkewAllocance time.Duration

	// The CA's private key.
	privateKey *ecdsa.PrivateKey

	// The CA's certificate.
	root *x509.Certificate

	// The PEM X.509 encoding of `root`
	rootPEM string

	// nextSerialNumber is the serial number of the next certificate to issue.
	// Serial numbers must not be reused.
	//
	// It is assumed there is only one instance of CA and it is assumed that a
	// given CA object isn't requested to issue certificates concurrently.
	//
	// For now we do not attempt to meet CABForum requirements (e.g. regarding
	// randomness).
	nextSerialNumber uint64
}

type CertificateAndPrivateKey struct {
	// The ASN.1 DER-encoded (binary, not PEM) certificate.
	Certificate []byte

	// The PKCS#8 DER-encoded (binary, not PEM) private key.
	PrivateKey []byte
}

// NewCA is the only way to create a CA.
func NewCA() (*CA, error) {
	// Initially all certificates will be valid for one year. TODO: Shorten the
	// validity duration of CA and end-entity certificates downward.
	validity := (24 * 365) * time.Hour

	// Allow half a day of clock skew. TODO: decrease the default value of this
	// and make it tunable. TODO: Reconsider how this interacts with the
	// similar logic in the webpki verifier; since both are trying to account
	// for clock skew, there is somewhat of an over-correction.
	clockSkewAllocance := 12 * time.Hour

	privateKey, err := generateKeyPair()
	if err != nil {
		return nil, err
	}

	ca := CA{
		validity:           validity,
		clockSkewAllocance: clockSkewAllocance,
		privateKey:         privateKey,
		nextSerialNumber:   1,
	}

	template := ca.createTemplate(&ca.privateKey.PublicKey)

	template.Subject = pkix.Name{CommonName: "Cluster-local Managed Pod CA"}

	// basicConstraints.cA = true
	template.IsCA = true
	template.MaxPathLen = -1
	template.BasicConstraintsValid = true

	// `parent == template` means "self-signed".
	rootDer, err := x509.CreateCertificate(rand.Reader, &template, &template, privateKey.Public(), privateKey)
	if err != nil {
		return nil, err
	}

	ca.root, err = x509.ParseCertificate(rootDer)
	if err != nil {
		return nil, err
	}

	ca.rootPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.root.Raw}))

	return &ca, nil
}

// TrustAnchorDER returns the PEM-encoded X.509 certificate of the trust anchor
// (root CA).
func (ca *CA) TrustAnchorPEM() string {
	return ca.rootPEM
}

// IssueEndEntityCertificate creates a new certificate that is valid for the
// given DNS name, generating a new keypair for it.
func (ca *CA) IssueEndEntityCertificate(dnsName string) (*CertificateAndPrivateKey, error) {
	privateKey, err := generateKeyPair()
	if err != nil {
		return nil, err
	}
	p8, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	template := ca.createTemplate(&privateKey.PublicKey)
	template.DNSNames = []string{dnsName}
	crt, err := x509.CreateCertificate(rand.Reader, &template, ca.root, &privateKey.PublicKey, ca.privateKey)
	if err != nil {
		return nil, err
	}
	return &CertificateAndPrivateKey{
		Certificate: crt,
		PrivateKey:  p8,
	}, nil
}

// createTemplate returns a certificate template for a non-CA certificate with
// no subject name, no subjectAltNames. The template can then be modified into
// a (root) CA template or an end-entity template by the caller.
func (ca *CA) createTemplate(publicKey *ecdsa.PublicKey) x509.Certificate {
	// ECDSA is used instead of RSA because ECDSA key generation is
	// straightforward and fast whereas RSA key generation is extremely slow
	// and error-prone.
	//
	// CA certificates are signed with the same algorithm as end-entity
	// certificates because they are relatively short-lived, because using one
	// algorithm minimizes exposure to implementation flaws, and to speed up
	// signature verification time.
	//
	// SHA-256 is used because any larger digest would be truncated to 256 bits
	// anyway since a P-256 scalar is only 256 bits long.
	const SignatureAlgorithm = x509.ECDSAWithSHA256

	serialNumber := big.NewInt(int64(ca.nextSerialNumber))
	ca.nextSerialNumber++

	notBefore := time.Now()

	return x509.Certificate{
		SerialNumber:       serialNumber,
		SignatureAlgorithm: SignatureAlgorithm,
		NotBefore:          notBefore.Add(-ca.clockSkewAllocance),
		NotAfter:           notBefore.Add(ca.validity).Add(ca.clockSkewAllocance),
		PublicKey:          publicKey,
	}
}

func generateKeyPair() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}
