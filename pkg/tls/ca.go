package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"time"
)

// CA provides a certificate authority for TLS-enabled installs.
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

	// clockSkewAllowance is the maximum supported clock skew. Everything that
	// processes the certificates must have a system clock that is off by no
	// more than this allowance in either direction.
	clockSkewAllowance time.Duration

	// cred contains the CA's credentials.
	cred Cred

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

const (
	// DefaultValidity configures certificate validity.
	//
	// Initially all certificates will be valid for one year.
	//
	// TODO: Shorten the validity duration of CA and end-entity certificates downward.
	DefaultValidity = (24 * 365) * time.Hour

	// DefaultClockSkewAllowance indicates the maximum allowed difference in clocks
	// in the network.
	//
	// Allow an two hours of clock skew.
	//
	// TODO: decrease the default value of this and make it tunable.
	//
	// TODO: Reconsider how this interacts with the similar logic in the webpki
	// verifier; since both are trying to account for clock skew, there is
	// somewhat of an over-correction.
	DefaultClockSkewAllowance = 2 * time.Hour
)

// SetClockSkewAllowance sets the maximum allowable time for a node's clock to skew.
func (ca *CA) SetClockSkewAllowance(csa time.Duration) {
	ca.clockSkewAllowance = csa
}

// SetValidity sets the lifetime for cettificates issued by this CA.
func (ca *CA) SetValidity(v time.Duration) {
	ca.validity = v
}

// NewCA initializes a new CA with default settings.
func NewCA(cred Cred) *CA {
	return &CA{
		nextSerialNumber:   1,
		clockSkewAllowance: DefaultClockSkewAllowance,
		validity:           DefaultValidity,
		cred:               cred,
	}
}

// GenerateRootCAWithDefaults generates a new root CA with default settings.
func GenerateRootCAWithDefaults(name string) (ca *CA, err error) {
	// Generate a new root key.
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}

	return CreateRootCA(key, name, DefaultClockSkewAllowance, DefaultValidity)
}

// CreateRootCA configures a new root CA with the given settings
func CreateRootCA(key *ecdsa.PrivateKey, name string, clockSkewAllowance, validity time.Duration) (*CA, error) {
	// Create a self-signed certificate with the new root key.
	t := createTemplate(1, &key.PublicKey, DefaultClockSkewAllowance, DefaultValidity)
	t.Subject = pkix.Name{CommonName: name}
	t.IsCA = true
	t.MaxPathLen = -1
	t.BasicConstraintsValid = true
	t.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	der, err := x509.CreateCertificate(rand.Reader, t, t, key.Public(), key)
	if err != nil {
		return nil, err
	}

	c, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	// crt has an empty TrustChain because it's at the root.
	crt := Crt{Certificate: c}

	ca := NewCA(Cred{Crt: &crt, PrivateKey: key})
	ca.SetClockSkewAllowance(clockSkewAllowance)
	ca.SetValidity(validity)
	return ca, nil
}

// GenerateCA generates a new intermdiary CA.
func (ca *CA) GenerateCA(name string, maxPathLen int) (*CA, error) {
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}

	t := ca.createTemplate(&key.PublicKey)
	t.Subject = pkix.Name{CommonName: name}
	t.IsCA = true
	t.MaxPathLen = maxPathLen
	t.MaxPathLenZero = true // 0-values are actually 0
	t.BasicConstraintsValid = true
	t.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	crt, err := ca.cred.CreateCrt(t)
	if err != nil {
		return nil, err
	}

	ica := CA{
		validity:           ca.validity,
		clockSkewAllowance: ca.clockSkewAllowance,
		cred:               Cred{Crt: crt, PrivateKey: key},
	}
	return &ica, nil
}

// GenerateEndEntityCred creates a new certificate that is valid for the
// given DNS name, generating a new keypair for it.
func (ca *CA) GenerateEndEntityCred(dnsName string) (*Cred, error) {
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}

	csr := x509.CertificateRequest{
		Subject:   pkix.Name{CommonName: dnsName},
		DNSNames:  []string{dnsName},
		PublicKey: &key.PublicKey,
	}
	crt, err := ca.SignEndEntityCrt(&csr)
	if err != nil {
		return nil, err
	}

	c := Cred{Crt: crt, PrivateKey: key}
	return &c, nil
}

// SignEndEntityCrt creates a new certificate that is valid for the
// given DNS name, generating a new keypair for it.
func (ca *CA) SignEndEntityCrt(csr *x509.CertificateRequest) (*Crt, error) {
	pubkey, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("CSR must contain an ECDSA public key: %+v", csr.PublicKey)
	}

	t := ca.createTemplate(pubkey)
	t.Issuer = ca.cred.Crt.Certificate.Subject
	t.Subject = csr.Subject
	t.Extensions = csr.Extensions
	t.ExtraExtensions = csr.ExtraExtensions
	t.DNSNames = csr.DNSNames
	t.EmailAddresses = csr.EmailAddresses
	t.IPAddresses = csr.IPAddresses
	t.URIs = csr.URIs
	return ca.cred.CreateCrt(t)
}

// Certificate returns this CA's certificate.
func (ca *CA) Certificate() *x509.Certificate {
	return ca.cred.Crt.Certificate
}

// CertPool returns a CertPool containing this CA's certificate and trust chain.
func (ca *CA) CertPool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.cred.Crt.Certificate)
	for _, c := range ca.cred.Crt.TrustChain {
		p.AddCert(c)
	}
	return p
}

// TrustChain returns this CA's trust chain from root to leaf.
func (ca *CA) TrustChain() []*x509.Certificate {
	return ca.cred.Crt.TrustChain
}

// createTemplate returns a certificate t for a non-CA certificate with
// no subject name, no subjectAltNames. The t can then be modified into
// a (root) CA t or an end-entity t by the caller.
func (ca *CA) createTemplate(pubkey *ecdsa.PublicKey) *x509.Certificate {
	c := createTemplate(ca.nextSerialNumber, pubkey, ca.clockSkewAllowance, ca.validity)
	ca.nextSerialNumber++
	return c
}

// createTemplate returns a certificate t for a non-CA certificate with
// no subject name, no subjectAltNames. The t can then be modified into
// a (root) CA t or an end-entity t by the caller.
func createTemplate(
	serialNumber uint64,
	pubkey *ecdsa.PublicKey,
	clockSkewAllowance, validity time.Duration,
) *x509.Certificate {
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

	notBefore := time.Now()

	return &x509.Certificate{
		SerialNumber:       big.NewInt(int64(serialNumber)),
		SignatureAlgorithm: SignatureAlgorithm,
		NotBefore:          notBefore.Add(-clockSkewAllowance),
		NotAfter:           notBefore.Add(validity).Add(clockSkewAllowance),
		PublicKey:          pubkey,
		KeyUsage:           x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
	}
}

// GenerateKey creates a new P-256 ECDSA private key from the default random
// source.
func GenerateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}
