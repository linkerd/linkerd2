package identity

import (
	"context"

	pb "github.com/linkerd/linkerd2-proxy-api/go/identity"
	"github.com/linkerd/linkerd2/pkg/tls"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
)

// FuzzServiceCertify fuzzes the Service.Certify method.
func FuzzServiceCertify(data []byte) int {
	f := fuzz.NewConsumer(data)
	req := &pb.CertifyRequest{}
	err := f.GenerateStruct(req)
	if err != nil {
		return 0
	}

	svc := NewService(&fakeValidator{"successful-result", nil}, nil, nil, nil, "", "", "")
	svc.updateIssuer(&fakeIssuer{tls.Crt{}, nil})

	_, _ = svc.Certify(context.Background(), req)
	return 1
}
