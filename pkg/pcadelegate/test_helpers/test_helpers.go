// Package test_helpers is a collection of structs and functions that are used in unit tests.
package test_helpers

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"

	"github.com/aws/aws-sdk-go/service/acmpca"
	"github.com/prometheus/common/log"
)

// CreateCSR creates a generic certificate signing request.
// This CSR is used in unit testing.
// borrowed from https://stackoverflow.com/questions/26043321/create-a-certificate-signing-request-csr-with-an-email-address-in-go.
func CreateCSR() x509.CertificateRequest {
	subj := pkix.Name{}
	rawSubj := subj.ToRDNSequence()
	rawSubj = append(rawSubj, []pkix.AttributeTypeAndValue{})

	asn1Subj, _ := asn1.Marshal(rawSubj)
	csr := x509.CertificateRequest{
		RawSubject:         asn1Subj,
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}

	return csr
}

// MockACMClient is a struct used for Mocking out the real AWS ACMClient.
// It implements the IssueCertificate and GetCertificate methods.
type MockACMClient struct {
	// IssueCertOutput represents the results returned when calling IssueCertificate on the aws-acm client.
	IssueCertOutput *acmpca.IssueCertificateOutput

	// IssueCertError represents the error returned when calling IssueCertificate on the aws-acm client.
	IssueCertError error

	// GetCertOutput represents the result returned when calling GetCertificate on the aws-acm client.
	GetCertOutput *acmpca.GetCertificateOutput

	// GetCertError represents the error returned when calling GetCertificate on the aws-acm client.
	GetCertError error
}

// GetCertificate stubs out the AWS ACM client and returns a precomputed output and error.
func (m MockACMClient) GetCertificate(input *acmpca.GetCertificateInput) (*acmpca.GetCertificateOutput, error) {
	return m.GetCertOutput, m.GetCertError
}

// IssueCertificate stubs out the AWS ACM client and returns a precomputed output and error.
func (m MockACMClient) IssueCertificate(input *acmpca.IssueCertificateInput) (*acmpca.IssueCertificateOutput, error) {
	return m.IssueCertOutput, m.IssueCertError
}

// VerifyCertificates is used to validate end certificates/chains against a trusted root.
func VerifyCertificates(endCert *x509.Certificate, rootCert *x509.Certificate, intermediateCert *x509.Certificate) {
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
		log.Info("End Entity certificate was validated")
	}
}
