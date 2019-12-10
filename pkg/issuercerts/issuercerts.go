package issuercerts

import (
	"crypto/ecdsa"
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

// CheckCertTimeValidity ensures the certificate is valid time - wise
func CheckCertTimeValidity(cert *x509.Certificate) error {
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
	lifetime := cert.NotAfter.Unix() - cert.NotBefore.Unix()
	timeLeft := cert.NotAfter.Unix() - time.Now().Unix()
	if (float64(timeLeft) / float64(lifetime)) < 0.1 {
		return fmt.Errorf("will expire on %s", cert.NotAfter.Format(time.RFC3339))
	}
	return nil
}

// CheckCertAlgoRequirements ensures the certificate respects with the constraints
// we have posed on the public key and signature algorithms
func CheckCertAlgoRequirements(cert *x509.Certificate) error {
	if cert.PublicKeyAlgorithm == x509.ECDSA {
		// this si a safe cast here as wel know we are using ECDSA
		k := cert.PublicKey.(*ecdsa.PublicKey)
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
func (ic *IssuerCertData) VerifyAndBuildCreds(dnsName string) (*tls.Cred, error) {

	creds, err := tls.ValidateAndCreateCreds(ic.IssuerCrt, ic.IssuerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA: %s", err)
	}

	// we check the time validity of the issuer cert
	if err := CheckCertTimeValidity(creds.Certificate); err != nil {
		return nil, err
	}

	// we check the algo requirements of the issuer cert
	if err := CheckCertAlgoRequirements(creds.Certificate); err != nil {
		return nil, err
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
