package autoregistration

import (
	"context"

	pb "github.com/linkerd/linkerd2-proxy-api/go/autoregistration"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type (
	// Service implements the gRPC service in terms of a Validator and Issuer.
	Service struct {
		pb.UnimplementedAutoregistrationServer
	}
)

// NewService creates a new identity service.
func NewService() *Service {
	return &Service{
		pb.UnimplementedAutoregistrationServer{},
	}
}

// Register registers an identity service implementation in the provided gRPC
// server.
func Register(g *grpc.Server, s *Service) {
	pb.RegisterAutoregistrationServer(g, s)
}

// Certify validates identity and signs certificates.
func (svc *Service) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	log.Infof("Got a registration reques SNI: %s, Identity: %s, Addr: %s", req.Sni, req.Identity, req.Addr.String())
	return &pb.RegisterResponse{}, nil
}
