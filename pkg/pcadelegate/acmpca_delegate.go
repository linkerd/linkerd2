// Package pcadelegate is used to delegate Certificate Signing Requests to AWS Private Certificate Authority.
// IssueCertificate requests sent to the Identity service may use this instead of the local ca.
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
	// ACMPCAClient is an interface that replicates the aws acmpca.Client.
	ACMPCAClient interface {
		// GetCertificate maps to the acmpca.Client's GetCertificate method.
		GetCertificate(input *acmpca.GetCertificateInput) (*acmpca.GetCertificateOutput, error)

		// IssueCertificate maps to the acmpca.Client's IssueCertificate method.
		IssueCertificate(input *acmpca.IssueCertificateInput) (*acmpca.IssueCertificateOutput, error)
	}

	// ACMPCADelegate implements the Issuer Interface.
	ACMPCADelegate struct {
		// acmClient abstracts away the AWS acmpca.Client.
		acmClient ACMPCAClient

		// params represents the configuration information we need to to issue certificates.
		CADelegateParams
	}

	// CADelegateParams holds the required parameters for creating a new CADelegate.
	CADelegateParams struct {
		// Region describes which AWS region the ACM-PCA resides in.
		Region string

		// CaARN describes the full ARN that represents the ACM-PCA we are using.
		CaARN string

		// ValidityPeriod describes how long certificates issued by this CA are expected to be valid.
		// This is in nanoseconds. We will convert nanoseconds in to days since ACMPCA only supports days at the moment.
		ValidityPeriod time.Duration

		// SigAlgorithm describes which Signature Algorithm to use when requesting a certficate
		SigAlgorithm SigAlgorithmType

		// Retryer is the implementation of the AWS-GO-SDK retry interface.
		// This allows us to inject different retry strategies.
		Retryer ACMPCARetryer
	}

	// SigAlgorithmType represents the different signature acm signature algorithms
	SigAlgorithmType string
)

const (
	// Sha256withecdsa is a SigAlgorithmType enum value
	Sha256withecdsa SigAlgorithmType = acmpca.SigningAlgorithmSha256withecdsa

	// Sha384withecdsa is a SigAlgorithmType enum value
	Sha384withecdsa SigAlgorithmType = acmpca.SigningAlgorithmSha384withecdsa

	// Sha512withecdsa is a SigAlgorithmType enum value
	Sha512withecdsa SigAlgorithmType = acmpca.SigningAlgorithmSha512withecdsa

	// Sha256withrsa is a SigAlgorithmType enum value
	Sha256withrsa SigAlgorithmType = acmpca.SigningAlgorithmSha256withrsa

	// Sha384withrsa is a SigAlgorithmType enum value
	Sha384withrsa SigAlgorithmType = acmpca.SigningAlgorithmSha384withrsa

	// Sha512withrsa is a SigAlgorithmType enum value
	Sha512withrsa SigAlgorithmType = acmpca.SigningAlgorithmSha512withrsa
)

// NewCADelegate is a factory method that returns a new ACMPCADelegate.
func NewCADelegate(params CADelegateParams) (*ACMPCADelegate, error) {
	sess := session.Must(
		session.NewSession(&aws.Config{
			Retryer: params.Retryer,
			Region:  aws.String(params.Region),
		}),
	)

	config := aws.NewConfig()
	acmClient := acmpca.New(sess, config)

	return &ACMPCADelegate{
		acmClient:        acmClient,
		CADelegateParams: params,
	}, nil
}

// ConvertNanoSecondsToDays is used to convert a duration in nanosecnods (time.Duration) to a number of days (int).
func ConvertNanoSecondsToDays(validityPeriod time.Duration) int64 {
	// validityPeriod is duration in nanoseconds.
	// time.Hour is an hour in nanoseconds.
	const hoursInDay int64 = 24
	hours := (validityPeriod / time.Hour)
	days := int64(hours) / hoursInDay
	return days
}

// IssueEndEntityCrt reaches out to the AWS ACM PCA, retrieves, and validates returned certificates.
func (c ACMPCADelegate) IssueEndEntityCrt(csr *x509.CertificateRequest) (tls.Crt, error) {
	certificateARN, issueCertError := c.issueCertificate(c.acmClient, csr)
	if issueCertError != nil {
		log.Errorf("Unable to issue a certificate on the aws client: %v\n", issueCertError)
		return tls.Crt{}, issueCertError
	}

	// The certificate is eventually consistent so you may get several 400 responses from AWS since the cert isn't ready yet.
	// There is also rate limiting per account/region in aws which results in a 400 response as well.
	certificateOutput, getCertificateErr := c.getCertificate(c.acmClient, *certificateARN)
	if getCertificateErr != nil {
		log.Errorf("Unable to execute get certificate on the aws client: %v\n", getCertificateErr)
		return tls.Crt{}, getCertificateErr
	}

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

	return crt, nil
}

func extractEndCertificate(endCertificate string) (*x509.Certificate, error) {
	// Convert the raw certOutput to a pem decoded block.
	byteCertificate := []byte(endCertificate)
	pemBlock, _ := pem.Decode(byteCertificate)
	if pemBlock == nil {
		return &x509.Certificate{}, errors.New("Unable to pemDecode the certificate returned from the aws client")
	}
	// Parse the pem decoded block into an x509.
	cert, certParseError := x509.ParseCertificate(pemBlock.Bytes)
	if certParseError != nil {
		log.Errorf("Unable to parse certificate: %v\n", certParseError)
		return &x509.Certificate{}, certParseError
	}

	return cert, nil
}

func extractTrustChain(certificateChain string) ([]*x509.Certificate, error) {
	// We normalize the chained PEM certificates because the AWS PrivateCA sends chained PEMS but it does not have a newline between each PEM.
	normalizedCertChain := normalizeChainedPEMCertificates(certificateChain)

	// Parse the cert chain.
	byteTrustChain := []byte(normalizedCertChain)
	var pemTrustBytes []byte
	var tempTrust *pem.Block
	var nextBytes []byte

	// If we received an empty CertChain.
	if len(byteTrustChain) == 0 {
		return []*x509.Certificate{}, errors.New("Unable to decode CertificateChain from the aws client, empty CertificateChain received")
	}

	// Walk through each PEM file and append the results without any newline.
	nextBytes = byteTrustChain
	for ok := true; ok; ok = (tempTrust != nil) && len(nextBytes) != 0 {
		tempTrust, nextBytes = pem.Decode(nextBytes)
		if tempTrust != nil {
			tempTrustBytes := tempTrust.Bytes
			pemTrustBytes = append(tempTrustBytes, pemTrustBytes...)
		}
	}

	// If there was a failure marshalling the pems from the cert chain.
	if len(nextBytes) != 0 {
		return []*x509.Certificate{}, errors.New("Unable to decode CertificateChain from the aws client, could not find pem while decoding")
	}

	// If there was a failure marshalling the pems from the cert chain.
	if pemTrustBytes == nil {
		return []*x509.Certificate{}, errors.New("Unable to decode CertificateChain from the aws client, could not find pem while decoding")
	}

	// Convert the chained certificates into a x509 format.
	trustChain, certChainParseError := x509.ParseCertificates(pemTrustBytes)
	if certChainParseError != nil {
		log.Errorf("Unable to parse trust chain certificates received from the aws client: %v\n", certChainParseError)
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
		CertificateAuthorityArn: &c.CaARN,
	}

	getCertOutput, getCertError := acmClient.GetCertificate(&getCertificateInput)
	if getCertError != nil {
		return nil, getCertError
	}
	return getCertOutput, nil
}

func (c ACMPCADelegate) issueCertificate(acmClient ACMPCAClient, csr *x509.CertificateRequest) (*string, error) {
	signingAlgo := string(c.SigAlgorithm)
	validityPeriodType := acmpca.ValidityPeriodTypeDays
	duration := ConvertNanoSecondsToDays(c.ValidityPeriod)
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
		log.Error("Unable to PEM encode the block based on the input certificate signing request\n")
		return nil, errors.New("Unable to PEM encode the input Certificate Signing Request")
	}

	issueCertificateInput := acmpca.IssueCertificateInput{
		CertificateAuthorityArn: &c.CaARN,
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
