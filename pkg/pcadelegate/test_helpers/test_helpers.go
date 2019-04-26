package test_helpers

// borrowed from https://stackoverflow.com/questions/26043321/create-a-certificate-signing-request-csr-with-an-email-address-in-go

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"

	"github.com/aws/aws-sdk-go/service/acmpca"
)

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

type MockACMClient struct {
	GetCertOutput   *acmpca.GetCertificateOutput
	GetCertError    error
	IssueCertOutput *acmpca.IssueCertificateOutput
	IssueCertError  error
}

func (m MockACMClient) GetCertificate(input *acmpca.GetCertificateInput) (*acmpca.GetCertificateOutput, error) {
	return m.GetCertOutput, m.GetCertError
}

func (m MockACMClient) IssueCertificate(input *acmpca.IssueCertificateInput) (*acmpca.IssueCertificateOutput, error) {
	return m.IssueCertOutput, m.IssueCertError
}
