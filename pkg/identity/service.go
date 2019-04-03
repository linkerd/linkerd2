package identity

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes"
	pb "github.com/linkerd/linkerd2-proxy-api/go/identity"
	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// DefaultIssuanceLifetime is the default lifetime of certificates issued by
	// the identity service.
	DefaultIssuanceLifetime = 24 * time.Hour
)

type (
	// Service implements the gRPC service in terms of a Validator and Issuer.
	Service struct {
		Validator
		tls.Issuer
	}

	// Validator implementors accept a bearer token, validates it, and returns a
	// DNS-form identity.
	Validator interface {
		// Validate takes an opaque authentication token, attempts to validate its
		// authenticity, and produces a DNS-like identifier.
		//
		// An InvalidToken error should be returned if the provided token was not in a
		// correct form.
		//
		// A NotAuthenticated error should be returned if the authenticity of the
		// token cannot be validated.
		Validate(context.Context, []byte) (string, error)
	}

	// InvalidToken is an error type returned by Validators to indicate that the
	// provided authentication token was not valid.
	InvalidToken struct{ Reason string }

	// NotAuthenticated is an error type returned by Validators to indicate that the
	// provided authentication token could not be authenticated.
	NotAuthenticated struct{}
)

// NewService creates a new identity service.
func NewService(v Validator, i tls.Issuer) *Service {
	return &Service{v, i}
}

// Register registers an identity service implementation in the provided gRPC
// server.
func Register(g *grpc.Server, s *Service) {
	pb.RegisterIdentityServer(g, s)
}

// Certify validates identity and signs certificates.
func (svc *Service) Certify(ctx context.Context, req *pb.CertifyRequest) (*pb.CertifyResponse, error) {
	// Extract the relevant info from the request.
	reqIdentity, tok, csr, err := checkRequest(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err = checkCSR(csr, reqIdentity); err != nil {
		log.Debugf("requester sent invalid CSR: %s", err)
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	// Authenticate the provided token against the Kubernetes API.
	log.Debugf("Validating token for %s", reqIdentity)
	tokIdentity, err := svc.Validate(ctx, tok)
	if err != nil {
		switch e := err.(type) {
		case NotAuthenticated:
			log.Infof("authentication failed for %s: %s", reqIdentity, e)
			return nil, status.Error(codes.FailedPrecondition, e.Error())
		case InvalidToken:
			log.Debugf("invalid token provided for %s: %s", reqIdentity, e)
			return nil, status.Error(codes.InvalidArgument, e.Error())
		default:
			msg := fmt.Sprintf("error validating token for %s: %s", reqIdentity, e)
			log.Error(msg)
			return nil, status.Error(codes.Internal, msg)
		}
	}

	// Ensure the requested identity matches the token's identity.
	if reqIdentity != tokIdentity {
		msg := fmt.Sprintf("requested identity did not match provided token: requested=%s; found=%s",
			reqIdentity, tokIdentity)
		log.Debug(msg)
		return nil, status.Error(codes.FailedPrecondition, msg)
	}

	// Create a certificate
	crt, err := svc.IssueEndEntityCrt(csr)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	crts := crt.ExtractRaw()
	if len(crts) == 0 {
		log.Fatal("the issuer provided a certificate without key material")
	}

	// Bundle issuer crt with certificate so the trust path to the root can be verified.
	log.Infof("certifying %s until %s", tokIdentity, crt.Certificate.NotAfter)
	validUntil, err := ptypes.TimestampProto(crt.Certificate.NotAfter)
	if err != nil {
		log.Errorf("invalid expiry time: %s", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	rsp := &pb.CertifyResponse{
		LeafCertificate:          crts[0],
		IntermediateCertificates: crts[1:],

		ValidUntil: validUntil,
	}
	return rsp, nil
}

func checkRequest(req *pb.CertifyRequest) (string, []byte, *x509.CertificateRequest, error) {
	reqIdentity := req.GetIdentity()
	if reqIdentity == "" {
		return "", nil, nil, errors.New("missing identity")
	}

	tok := req.GetToken()
	if len(tok) == 0 {
		return "", nil, nil, errors.New("missing token")
	}

	der := req.GetCertificateSigningRequest()
	if len(der) == 0 {
		return "", nil, nil,
			errors.New("missing certificate signing request")
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return "", nil, nil, err
	}

	return reqIdentity, tok, csr, nil
}

func checkCSR(csr *x509.CertificateRequest, identity string) error {
	if len(csr.DNSNames) != 1 {
		return errors.New("CSR must have exactly one DNSName")
	}
	if csr.DNSNames[0] != identity {
		return fmt.Errorf("CSR name does not match requested identity: csr=%s; req=%s", csr.DNSNames[0], identity)
	}

	switch csr.Subject.CommonName {
	case "", identity:
	default:
		return fmt.Errorf("invalid CommonName: %s", csr.Subject.CommonName)
	}

	if len(csr.EmailAddresses) > 0 {
		return errors.New("cannot validate email addresses")
	}
	if len(csr.IPAddresses) > 0 {
		return errors.New("cannot validate IP addresses")
	}
	if len(csr.URIs) > 0 {
		return errors.New("cannot validate URIs")
	}

	return nil
}

func (NotAuthenticated) Error() string {
	return "authentication token could not be authenticated"
}

func (e InvalidToken) Error() string {
	return e.Reason
}
