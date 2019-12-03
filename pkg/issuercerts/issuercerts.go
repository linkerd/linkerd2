package issuercerts

import (
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const keyMissingError = "key %s containing the %s needs to exist in secret %s if --identity-external-issuer=%v"

// IssuerCertData holds the root cert data used by the CA
type IssuerCertData struct {
	TrustAnchors string
	IssuerCrt    string
	IssuerKey    string
}

// FetchIssuerData fetches the issuer data from the linkerd-identitiy-issuer secrets (used for linkerd.io/tls schemed secrets)
func FetchIssuerData(api *k8s.KubernetesAPI, trustAnchors, controlPlaneNamespace string) (*IssuerCertData, error) {

	secret, err := api.CoreV1().Secrets(controlPlaneNamespace).Get(k8s.IdentityIssuerSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	crt, ok := secret.Data[k8s.IdentityIssuerCrtName]
	if !ok {
		return nil, fmt.Errorf(keyMissingError, k8s.IdentityIssuerCrtName, "issuer certificate", consts.IdentityIssuerSecretName, false)
	}

	key, ok := secret.Data[k8s.IdentityIssuerKeyName]
	if !ok {
		return nil, fmt.Errorf(keyMissingError, k8s.IdentityIssuerKeyName, "issuer key", consts.IdentityIssuerSecretName, true)
	}

	return &IssuerCertData{trustAnchors, string(crt), string(key)}, nil
}

// FetchExternalIssuerData fetches the issuer data from the linkerd-identitiy-issuer secrets (used for kubernetes.io/tls schemed secrets)
func FetchExternalIssuerData(api *k8s.KubernetesAPI, controlPlaneNamespace string) (*IssuerCertData, error) {
	secret, err := api.CoreV1().Secrets(controlPlaneNamespace).Get(k8s.IdentityIssuerSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	anchors, ok := secret.Data[consts.IdentityIssuerTrustAnchorsNameExternal]
	if !ok {
		return nil, fmt.Errorf(keyMissingError, consts.IdentityIssuerTrustAnchorsNameExternal, "trust anchors", consts.IdentityIssuerSecretName, true)
	}

	crt, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return nil, fmt.Errorf(keyMissingError, corev1.TLSCertKey, "issuer certificate", consts.IdentityIssuerSecretName, true)
	}

	key, ok := secret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf(keyMissingError, corev1.TLSPrivateKeyKey, "issuer key", consts.IdentityIssuerSecretName, true)
	}

	return &IssuerCertData{string(anchors), string(crt), string(key)}, nil
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

	return &IssuerCertData{string(anchors), crt, key}, nil
}

func validateCert(cert *x509.Certificate) error {
	if cert.PublicKeyAlgorithm != x509.ECDSA {
		return fmt.Errorf("the required public key algorithm is %s, instead %s was used", x509.ECDSA, cert.PublicKeyAlgorithm)
	}

	if cert.SignatureAlgorithm != x509.ECDSAWithSHA256 {
		return fmt.Errorf("the required public key algorithm is %s, instead %s was used", x509.ECDSAWithSHA256, cert.SignatureAlgorithm)
	}

	if cert.NotBefore.After(time.Now()) {
		return fmt.Errorf("certificate not valid before: %s", cert.NotBefore.Format(time.RFC3339))
	}

	if cert.NotAfter.Before(time.Now()) {
		return fmt.Errorf("certificate not valid anymore. Expired at: %s", cert.NotAfter.Format(time.RFC3339))
	}

	return nil
}

// VerifyAndBuildCreds builds and validates the creds out of the data in IssuerCertData
func (ic *IssuerCertData) VerifyAndBuildCreds(dnsName string) (*tls.Cred, error) {

	creds, err := tls.ValidateAndCreateCreds(ic.IssuerCrt, ic.IssuerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA: %s", err)
	}

	// we check the validity of the issuer cert
	if err := validateCert(creds.Certificate); err != nil {
		return nil, fmt.Errorf("invalid issuer certificate: %s", err)
	}

	roots, err := tls.DecodePEMCertPool(ic.TrustAnchors)
	if err != nil {
		return nil, err
	}

	if err := creds.Verify(roots, dnsName); err != nil {
		return nil, err
	}

	return creds, nil
}
