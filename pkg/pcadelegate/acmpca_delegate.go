package pcadelegate

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/acmpca"
	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
)

type (
	// ACMPCAClient is an interface that replicates the aws acmpca.Client
	ACMPCAClient interface {
		GetCertificate(input *acmpca.GetCertificateInput) (*acmpca.GetCertificateOutput, error)
		IssueCertificate(input *acmpca.IssueCertificateInput) (*acmpca.IssueCertificateOutput, error)
	}

	// ACMPCADelegate implements the Issuer Interface
	ACMPCADelegate struct {
		acmClient ACMPCAClient
		caARN     string
	}
)

// EasyNewCADelegate conveniently creates an ACMPCADelegate configured for our specific environment.
func EasyNewCADelegate() (*ACMPCADelegate, error) {
	region := string("us-west-2")
	caARN := string("arn:aws:acm-pca:us-west-2:536616252769:certificate-authority/70429e36-3d24-4fc1-9308-841e469c5409")
	return NewCADelegate(region, caARN)
}

// NewCADelegate is a factory method that returns a fresh ACMPCADelegate
func NewCADelegate(region, caARN string) (*ACMPCADelegate, error) {
	session, sessionErr := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})

	//config := aws.NewConfig().WithLogLevel(aws.LogDebugWithRequestErrors)
	config := aws.NewConfig()

	if sessionErr != nil {
		log.Error("Unable to create aws session for AWS ACMPCA")
		return nil, sessionErr
	}

	acmClient := acmpca.New(session, config)

	return &ACMPCADelegate{
		acmClient: acmClient,
		caARN:     caARN,
	}, nil
}

// IssueEndEntityCrt reaches out to the AWS ACM PCA, retrieves, and validates returned certificates.
func (c ACMPCADelegate) IssueEndEntityCrt(csr *x509.CertificateRequest) (tls.Crt, error) {
	// ask aws client to create a certificate on our behalf, the return value is the arn of the certificate
	certificateARN, issueCertError := c.issueCertificate(c.acmClient, csr)
	if issueCertError != nil {
		log.Errorf("Unable to issue a certificate on the aws client: %v", issueCertError)
		return tls.Crt{}, issueCertError
	}

	time.Sleep(2 * time.Second)

	// ask aws client to fetch the certificate based on the arn
	certificateOutput, getCertificateErr := c.getCertificate(c.acmClient, *certificateARN)
	if getCertificateErr != nil {
		log.Errorf("Unable to execute get certificate on the aws client: %v", getCertificateErr)
		return tls.Crt{}, getCertificateErr
	}
	log.Infof("Successfully got certficate combo: %v", *certificateOutput.Certificate)

	// parse the cert
	endCert, extractEndCertError := extractEndCertificate(*certificateOutput.Certificate)
	if extractEndCertError != nil {
		return tls.Crt{}, extractEndCertError
	}

	trustChain, extractTrustError := extractTrustChain(*certificateOutput.CertificateChain)
	if extractTrustError != nil {
		return tls.Crt{}, extractTrustError
	}

	crt := tls.Crt{
		Certificate: endCert,
		TrustChain:  trustChain,
	}

	verifyCertificates(endCert, trustChain[1], trustChain[0])

	return crt, nil
}

func verifyCertificates(endCert *x509.Certificate, rootCert *x509.Certificate, intermediateCert *x509.Certificate) {
	roots := x509.NewCertPool()
	interm := x509.NewCertPool()

	roots.AddCert(rootCert)

	interm.AddCert(intermediateCert)

	opts := x509.VerifyOptions{
		Roots:         roots,
		Intermediates: interm,
	}

	if _, verifyErr := endCert.Verify(opts); verifyErr != nil {
		log.Errorf("failed to verify certificate " + verifyErr.Error())
	} else {
		log.Info("GREAT SUCCESSS: Certificate was validated")
	}
}

func extractEndCertificate(endCertificate string) (*x509.Certificate, error) {
	// convert the raw certOutput to a pem decoded block
	byteCertificate := []byte(endCertificate)
	pemBlock, _ := pem.Decode(byteCertificate)
	if pemBlock == nil {
		return &x509.Certificate{}, errors.New("Unable to pemDecode the certificate returned from the aws client")
	}
	// parse the pem decoded block into an x509
	cert, certParseError := x509.ParseCertificate(pemBlock.Bytes)
	if certParseError != nil {
		log.Errorf("Unable to parse certificate: %v", certParseError)
		return &x509.Certificate{}, certParseError
	}

	return cert, nil
}

func extractTrustChain(certificateChain string) ([]*x509.Certificate, error) {
	// we normalize the chained PEM certificates because the AWS PrivateCA sends chained PEMS but it does not have a newline between each PEM
	normalizedCertChain := normalizeChainedPEMCertificates(certificateChain)

	// parse the cert chain
	byteTrustChain := []byte(normalizedCertChain)
	var pemTrustBytes []byte
	var tempTrust *pem.Block
	var nextBytes []byte

	// if we received an empty CertChain
	if len(byteTrustChain) == 0 {
		return []*x509.Certificate{}, errors.New("Unable to decode CertificateChain from the aws client, empty CertificateChain received")
	}

	// walk through each PEM file and append the results without any newline
	nextBytes = byteTrustChain
	for ok := true; ok; ok = (tempTrust != nil) && len(nextBytes) != 0 {
		tempTrust, nextBytes = pem.Decode(nextBytes)
		if tempTrust != nil {
			tempTrustBytes := tempTrust.Bytes
			pemTrustBytes = append(tempTrustBytes, pemTrustBytes...)
		}
	}

	// if there was a failure marshalling the pems from the cert chain
	if len(nextBytes) != 0 {
		return []*x509.Certificate{}, errors.New("Unable to decode CertificateChain from the aws client, could not find pem while decoding")
	}

	// if there was a failure marshalling the pems from the cert chain
	if pemTrustBytes == nil {
		return []*x509.Certificate{}, errors.New("Unable to decode CertificateChain from the aws client, could not find pem while decoding")
	}

	// convert the chained certificates into a x509 format
	trustChain, certChainParseError := x509.ParseCertificates(pemTrustBytes)
	if certChainParseError != nil {
		log.Errorf("Unable to parse trust chain certificates received from the aws client: %v", certChainParseError)
		return []*x509.Certificate{}, certChainParseError
	}

	return trustChain, nil
}

func normalizeChainedPEMCertificates(chainedPEMString string) string {
	const targetString = "-----END CERTIFICATE----------BEGIN CERTIFICATE-----"
	const replacementString = "-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----"
	const unlimitedReplace = -1
	return strings.Replace(chainedPEMString, targetString, replacementString, unlimitedReplace)
}

func (c ACMPCADelegate) getCertificate(acmClient ACMPCAClient, certificateARN string) (*acmpca.GetCertificateOutput, error) {
	getCertificateInput := acmpca.GetCertificateInput{
		CertificateArn:          &certificateARN,
		CertificateAuthorityArn: &c.caARN,
	}

	getCertOutput, getCertError := acmClient.GetCertificate(&getCertificateInput)
	if getCertError != nil {
		return nil, getCertError
	}
	return getCertOutput, nil
}

func (c ACMPCADelegate) issueCertificate(acmClient ACMPCAClient, csr *x509.CertificateRequest) (*string, error) {
	signingAlgo := acmpca.SigningAlgorithmSha256withrsa
	validityPeriodType := acmpca.ValidityPeriodTypeDays
	duration := int64(30)
	validity := acmpca.Validity{
		Type:  &validityPeriodType,
		Value: &duration,
	}

	const certType = "CERTIFICATE REQUEST"
	derBlock := pem.Block{
		Type:  certType,
		Bytes: csr.Raw,
	}

	encodedPem := pem.EncodeToMemory(&derBlock)
	if encodedPem == nil {
		log.Error("Was not able to PEM encode the block based on the input certificate signing request")
		return nil, errors.New("Unable to PEM encode the input Certificate Signing Request")
	}

	issueCertificateInput := acmpca.IssueCertificateInput{
		CertificateAuthorityArn: &c.caARN,
		Csr:                     encodedPem,
		SigningAlgorithm:        &signingAlgo,
		Validity:                &validity,
	}

	arnForCert, certArnErr := acmClient.IssueCertificate(&issueCertificateInput)
	if certArnErr != nil {
		return nil, certArnErr
	}

	return arnForCert.CertificateArn, nil
}
