package cmd

import (
	"context"
	"fmt"

	"github.com/golang/protobuf/ptypes"
	pb "github.com/linkerd/linkerd2/controller/gen/config"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/issuercerts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// fetchIdentityValue checks the kubernetes API to fetch an existing
// linkerd identity configuration.
//
// This bypasses the public API so that we can access secrets and validate
// permissions.
func fetchIdentityValues(ctx context.Context, k kubernetes.Interface, idctx *pb.IdentityContext, values *charts.Values) error {
	if idctx == nil {
		return nil
	}

	if idctx.Scheme == "" {
		// if this is empty, then we are upgrading from a version
		// that did not support issuer schemes. Just default to the
		// linkerd one.
		idctx.Scheme = k8s.IdentityIssuerSchemeLinkerd
	}

	var trustAnchorsPEM string
	var issuerData *issuercerts.IssuerCertData
	var err error

	trustAnchorsPEM = idctx.GetTrustAnchorsPem()

	issuerData, err = fetchIssuer(ctx, k, trustAnchorsPEM, idctx.Scheme)
	if err != nil {
		return err
	}

	clockSkewDuration, err := ptypes.Duration(idctx.GetClockSkewAllowance())
	if err != nil {
		return fmt.Errorf("could not convert clock skew protobuf Duration format into golang Duration: %s", err)
	}

	issuanceLifetimeDuration, err := ptypes.Duration(idctx.GetIssuanceLifetime())
	if err != nil {
		return fmt.Errorf("could not convert issuance Lifetime protobuf Duration format into golang Duration: %s", err)
	}

	values.IdentityTrustAnchorsPEM = trustAnchorsPEM
	values.Identity.Issuer.Scheme = idctx.Scheme
	values.Identity.Issuer.ClockSkewAllowance = clockSkewDuration.String()
	values.Identity.Issuer.IssuanceLifetime = issuanceLifetimeDuration.String()
	values.Identity.Issuer.TLS.KeyPEM = issuerData.IssuerKey
	values.Identity.Issuer.TLS.CrtPEM = issuerData.IssuerCrt

	return nil
}

func fetchIssuer(ctx context.Context, k kubernetes.Interface, trustPEM string, scheme string) (*issuercerts.IssuerCertData, error) {
	var (
		issuerData *issuercerts.IssuerCertData
		err        error
	)
	switch scheme {
	case string(corev1.SecretTypeTLS):
		// Do not return external issuer certs as no need of storing them in config and upgrade secrets
		// Also contradicts condition in https://github.com/linkerd/linkerd2/blob/main/cli/cmd/options.go#L550
		return &issuercerts.IssuerCertData{}, nil
	default:
		issuerData, err = issuercerts.FetchIssuerData(ctx, k, trustPEM, controlPlaneNamespace)
		if issuerData != nil && issuerData.TrustAnchors != trustPEM {
			issuerData.TrustAnchors = trustPEM
		}
	}
	if err != nil {
		return nil, err
	}

	return issuerData, nil
}
