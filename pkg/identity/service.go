package identity

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"

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

	// EnvTrustAnchors is the environment variable holding the trust anchors for
	// the proxy identity.
	EnvTrustAnchors  = "LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS"
	eventTypeSkipped = "IssuerUpdateSkipped"
	eventTypeUpdated = "IssuerUpdated"
	eventTypeFailed  = "IssuerValidationFailed"
)

type (
	// Service implements the gRPC service in terms of a Validator and Issuer.
	Service struct {
		validator                                  Validator
		trustAnchors                               *x509.CertPool
		issuer                                     *tls.Issuer
		issuerMutex                                *sync.RWMutex
		validity                                   *tls.Validity
		recordEvent                                func(eventType, reason, message string)
		expectedName, issuerPathCrt, issuerPathKey string
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

// Initialize loads the issuer certs from disk so it can start service CSRs to proxies
func (svc *Service) Initialize() error {
	credentials, err := svc.loadCredentials()
	if err != nil {
		return err
	}
	svc.updateIssuer(credentials)
	return nil
}

func (svc *Service) updateIssuer(newIssuer tls.Issuer) {
	svc.issuerMutex.Lock()
	svc.issuer = &newIssuer
	log.Debug("Issuer has been updated")
	svc.issuerMutex.Unlock()
}

// Run reads from the issuer and error channels and reloads the issuer certs when necessary
func (svc *Service) Run(issuerEvent <-chan struct{}, issuerError <-chan error) {
	for {
		select {
		case <-issuerEvent:
			if err := svc.Initialize(); err != nil {
				message := fmt.Sprintf("Skipping issuer update as certs could not be read from disk: %s", err)
				log.Warn(message)
				svc.recordEvent(v1.EventTypeWarning, eventTypeSkipped, message)
			} else {
				message := "Updated identity issuer"
				log.Infof(message)
				svc.recordEvent(v1.EventTypeNormal, eventTypeUpdated, message)
			}
		case err := <-issuerError:
			log.Warnf("Received error from fs watcher: %s", err)
		}
	}
}

func (svc *Service) loadCredentials() (tls.Issuer, error) {
	creds, err := tls.ReadPEMCreds(
		svc.issuerPathKey,
		svc.issuerPathCrt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to read CA from disk: %s", err)
	}

	// Don't verify with dns name as this is not a leaf certificate
	if err := creds.Crt.Verify(svc.trustAnchors, "", time.Time{}); err != nil {
		return nil, fmt.Errorf("failed to verify issuer credentials for '%s' with trust anchors: %s", svc.expectedName, err)
	}

	log.Debugf("Loaded issuer cert: %s", creds.EncodeCertificatePEM())
	return tls.NewCA(*creds, *svc.validity), nil
}

// NewService creates a new identity service.
func NewService(validator Validator, trustAnchors *x509.CertPool, validity *tls.Validity, recordEvent func(eventType, reason, message string), expectedName, issuerPathCrt, issuerPathKey string) *Service {
	return &Service{
		validator,
		trustAnchors,
		nil,
		&sync.RWMutex{},
		validity,
		recordEvent,
		expectedName,
		issuerPathCrt,
		issuerPathKey,
	}
}

// Register registers an identity service implementation in the provided gRPC
// server.
func Register(g *grpc.Server, s *Service) {
	pb.RegisterIdentityServer(g, s)
}

// ensureIssuerStillValid should check that the CA is still good time wise
// and verifies just fine with the provided trust anchors
func (svc *Service) ensureIssuerStillValid() error {
	issuer := *svc.issuer
	switch is := issuer.(type) {
	case *tls.CA:
		// Don't verify with dns name as this is not a leaf certificate
		return is.Cred.Verify(svc.trustAnchors, "", time.Time{})
	default:
		return fmt.Errorf("unsupported issuer type. Expected *tls.CA, got %v", is)
	}
}

// Certify validates identity and signs certificates.
func (svc *Service) Certify(ctx context.Context, req *pb.CertifyRequest) (*pb.CertifyResponse, error) {
	svc.issuerMutex.RLock()
	defer svc.issuerMutex.RUnlock()

	if svc.issuer == nil {
		log.Warn("Certificate issuer is not ready")
		return nil, status.Error(codes.Unavailable, "cert issuer not ready yet")
	}

	// Extract the relevant info from the request.
	reqIdentity, tok, csr, err := checkRequest(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := svc.ensureIssuerStillValid(); err != nil {
		log.Errorf("could not process CSR because of CA cert validation failure: %s - CSR Identity : %s", err, reqIdentity)
		message := fmt.Sprintf("%s - CSR Identity : %s", err.Error(), reqIdentity)
		svc.recordEvent(v1.EventTypeWarning, eventTypeFailed, message)
		return nil, err
	}

	if err = checkCSR(csr, reqIdentity); err != nil {
		log.Debugf("requester sent invalid CSR: %s", err)
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	// Authenticate the provided token against the Kubernetes API.
	log.Debugf("Validating token for %s", reqIdentity)
	tokIdentity, err := svc.validator.Validate(ctx, tok)
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
	issuer := *svc.issuer
	crt, err := issuer.IssueEndEntityCrt(csr)
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
