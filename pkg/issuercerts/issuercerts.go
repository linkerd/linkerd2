package issuercerts

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const keyMissingError = "key %s containing the %s needs to exist in secret %s if --identity-external-issuer=%v"
const expirationWarningThresholdInDays = 60

// IssuerCertData holds the trust anchors cert data used by the CA
type IssuerCertData struct {
	TrustAnchors string
	IssuerCrt    string
	IssuerKey    string
	Expiry       *time.Time
}

// FetchIssuerData fetches the issuer data from the linkerd-identity-issuer secrets (used for linkerd.io/tls schemed secrets)
func FetchIssuerData(ctx context.Context, api kubernetes.Interface, trustAnchors, controlPlaneNamespace string) (*IssuerCertData, error) {
	secret, err := api.CoreV1().Secrets(controlPlaneNamespace).Get(ctx, k8s.IdentityIssuerSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	crt, ok := secret.Data[k8s.IdentityIssuerCrtName]
	if !ok {
		return nil, fmt.Errorf(keyMissingError, k8s.IdentityIssuerCrtName, "issuer certificate", k8s.IdentityIssuerSecretName, false)
	}

	key, ok := secret.Data[k8s.IdentityIssuerKeyName]
	if !ok {
		return nil, fmt.Errorf(keyMissingError, k8s.IdentityIssuerKeyName, "issuer key", k8s.IdentityIssuerSecretName, true)
	}

	cert, err := tls.DecodePEMCrt(string(crt))
	if err != nil {
		return nil, fmt.Errorf("could not parse issuer certificate: %w", err)
	}

	return &IssuerCertData{trustAnchors, string(crt), string(key), &cert.Certificate.NotAfter}, nil
}

// FetchExternalIssuerData fetches the issuer data from the linkerd-identity-issuer secrets (used for kubernetes.io/tls schemed secrets)
func FetchExternalIssuerData(ctx context.Context, api kubernetes.Interface, controlPlaneNamespace string) (*IssuerCertData, error) {
	secret, err := api.CoreV1().Secrets(controlPlaneNamespace).Get(ctx, k8s.IdentityIssuerSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	anchors, ok := secret.Data[k8s.IdentityIssuerTrustAnchorsNameExternal]
	if !ok {
		return nil, fmt.Errorf(keyMissingError, k8s.IdentityIssuerTrustAnchorsNameExternal, "trust anchors", k8s.IdentityIssuerSecretName, true)
	}

	crt, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return nil, fmt.Errorf(keyMissingError, corev1.TLSCertKey, "issuer certificate", k8s.IdentityIssuerSecretName, true)
	}

	key, ok := secret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf(keyMissingError, corev1.TLSPrivateKeyKey, "issuer key", k8s.IdentityIssuerSecretName, true)
	}

	cert, err := tls.DecodePEMCrt(string(crt))
	if err != nil {
		return nil, fmt.Errorf("could not parse issuer certificate: %w", err)
	}

	return &IssuerCertData{string(anchors), string(crt), string(key), &cert.Certificate.NotAfter}, nil
}

// LoadIssuerCrtAndKeyFromFiles loads the issuer certificate and key from files
func LoadIssuerCrtAndKeyFromFiles(keyPEMFile, crtPEMFile string) (string, string, error) {
	key, err := ioutil.ReadFile(keyPEMFile)
	if err != nil {
		return "", "", err
	}

	crt, err := ioutil.ReadFile(crtPEMFile)
	if err != nil {
		return "", "", err
	}

	return string(key), string(crt), nil
}

// LoadIssuerDataFromFiles loads the issuer data from file stored on disk
func LoadIssuerDataFromFiles(keyPEMFile, crtPEMFile, trustPEMFile string) (*IssuerCertData, error) {
	key, crt, err := LoadIssuerCrtAndKeyFromFiles(keyPEMFile, crtPEMFile)
	if err != nil {
		return nil, err
	}

	anchors, err := ioutil.ReadFile(trustPEMFile)
	if err != nil {
		return nil, err
	}

	return &IssuerCertData{string(anchors), crt, key, nil}, nil
}

// CheckCertValidityPeriod ensures the certificate is valid time - wise
func CheckCertValidityPeriod(cert *x509.Certificate) error {
	if cert.NotBefore.After(time.Now()) {
		return fmt.Errorf("not valid before: %s", cert.NotBefore.Format(time.RFC3339))
	}

	if cert.NotAfter.Before(time.Now()) {
		return fmt.Errorf("not valid anymore. Expired on %s", cert.NotAfter.Format(time.RFC3339))
	}
	return nil
}

// CheckExpiringSoon returns an error if a certificate is expiring soon
func CheckExpiringSoon(cert *x509.Certificate) error {
	if time.Now().AddDate(0, 0, expirationWarningThresholdInDays).After(cert.NotAfter) {
		return fmt.Errorf("will expire on %s", cert.NotAfter.Format(time.RFC3339))
	}
	return nil
}

// CheckCertAlgoRequirements ensures the certificate respects with the constraints
// we have posed on the public key and signature algorithms
func CheckCertAlgoRequirements(cert *x509.Certificate) error {
	if cert.PublicKeyAlgorithm == x509.ECDSA {
		// this is a safe cast here as we know we are using ECDSA
		k, ok := cert.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("expected ecdsa.PublicKey but got something %v", cert.PublicKey)
		}
		if k.Params().BitSize != 256 {
			return fmt.Errorf("must use P-256 curve for public key, instead P-%d was used", k.Params().BitSize)
		}
	} else {
		return fmt.Errorf("must use ECDSA for public key algorithm, instead %s was used", cert.PublicKeyAlgorithm)
	}

	if cert.SignatureAlgorithm != x509.ECDSAWithSHA256 {
		return fmt.Errorf("must be signed by an ECDSA P-256 key, instead %s was used", cert.SignatureAlgorithm)
	}
	return nil
}

// VerifyAndBuildCreds builds and validates the creds out of the data in IssuerCertData
func (ic *IssuerCertData) VerifyAndBuildCreds() (*tls.Cred, error) {
	creds, err := tls.ValidateAndCreateCreds(ic.IssuerCrt, ic.IssuerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA: %s", err)
	}

	// we check the time validity of the issuer cert
	if err := CheckCertValidityPeriod(creds.Certificate); err != nil {
		return nil, err
	}

	// we check the algo requirements of the issuer cert
	if err := CheckCertAlgoRequirements(creds.Certificate); err != nil {
		return nil, err
	}

	if !creds.Certificate.IsCA {
		return nil, fmt.Errorf("issuer cert is not a CA")
	}

	anchors, err := tls.DecodePEMCertPool(ic.TrustAnchors)
	if err != nil {
		return nil, err
	}

	if err := creds.Verify(anchors, "", time.Time{}); err != nil {
		return nil, err
	}

	return creds, nil
}
