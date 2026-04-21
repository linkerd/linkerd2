package identity

import (
	"context"
	"crypto/x509"

	"github.com/linkerd/linkerd2/pkg/tls"
)

type fakeValidator struct {
	result string
	err    error
}

func (fk *fakeValidator) Validate(context.Context, []byte) (string, error) {
	return fk.result, fk.err
}

type fakeIssuer struct {
	result tls.Crt
	err    error
}

func (fi *fakeIssuer) IssueEndEntityCrt(*x509.CertificateRequest) (tls.Crt, error) {
	return fi.result, fi.err
}
