package identity

import (
	"context"
	"crypto/x509"
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/identity"
	"github.com/linkerd/linkerd2/pkg/tls"
)

type fakeValidator struct {
	result string
	err    error
}

type fakeIssuer struct {
	result tls.Crt
	err    error
}

func (fi *fakeIssuer) IssueEndEntityCrt(*x509.CertificateRequest) (tls.Crt, error) {
	return fi.result, fi.err
}

func (fk *fakeValidator) Validate(context.Context, []byte) (string, error) {
	return fk.result, fk.err
}

func TestServiceNotReady(t *testing.T) {
	//ch := make(chan tls.Issuer, 1)
	svc := NewService(&fakeValidator{"successful-result", nil}, nil, nil, nil, "", "", "")
	req := &pb.CertifyRequest{
		Identity:                  "some-identity",
		Token:                     []byte{},
		CertificateSigningRequest: []byte{},
	}

	_, err := svc.Certify(context.TODO(), req)

	expectedError := "rpc error: code = Unavailable desc = cert issuer not ready yet"

	if err != nil {
		if err.Error() != expectedError {
			t.Fatalf("Expected error string\"%s\", got \"%s\"", expectedError, err)
		}
	} else {
		t.Fatalf("Expected error but got got nothing")
	}
}

func TestInvalidRequestArguments(t *testing.T) {
	svc := NewService(&fakeValidator{"successful-result", nil}, nil, nil, nil, "", "", "")
	svc.updateIssuer(&fakeIssuer{tls.Crt{}, nil})
	fakeData := "fake-data"
	invalidCsr := func() *pb.CertifyRequest {
		return &pb.CertifyRequest{
			Identity:                  fakeData,
			Token:                     []byte(fakeData),
			CertificateSigningRequest: []byte(fakeData),
		}
	}

	reqNoIdentity := invalidCsr()
	reqNoIdentity.Identity = ""

	reqNoToken := invalidCsr()
	reqNoToken.Token = []byte{}

	reqNoCsr := invalidCsr()
	reqNoCsr.CertificateSigningRequest = []byte{}

	testCases := []struct {
		input         *pb.CertifyRequest
		expectedError string
	}{
		{reqNoIdentity, "rpc error: code = InvalidArgument desc = missing identity"},
		{reqNoToken, "rpc error: code = InvalidArgument desc = missing token"},
		{reqNoCsr, "rpc error: code = InvalidArgument desc = missing certificate signing request"},
		{invalidCsr(), "rpc error: code = InvalidArgument desc = asn1: structure error: tags don't match " +
			"(16 vs {class:1 tag:6 length:97 isCompound:true}) " +
			"{optional:false explicit:false application:false private:false defaultValue:<nil> " +
			"tag:<nil> stringType:0 timeType:0 set:false omitEmpty:false} certificateRequest @2"},
	}

	for _, tc := range testCases {

		_, err := svc.Certify(context.TODO(), tc.input)
		if tc.expectedError != "" {
			if err == nil {
				t.Fatal("Expected error, got nothing")
			}
			if err.Error() != tc.expectedError {
				t.Fatalf("Expected error string\"%s\", got \"%s\"", tc.expectedError, err)
			}
		}
	}

}
