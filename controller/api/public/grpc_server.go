package public

import (
	"context"
	"errors"
	"runtime"

	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
)

// Server specifies the interface the Public API server should implement
type Server interface {
	pb.ApiServer
	destinationPb.DestinationServer
}

type grpcServer struct {
	destinationClient   destinationPb.DestinationClient
	k8sAPI              *k8s.API
	controllerNamespace string
	clusterDomain       string
}

func newGrpcServer(
	destinationClient destinationPb.DestinationClient,
	k8sAPI *k8s.API,
	controllerNamespace string,
	clusterDomain string,
) *grpcServer {

	grpcServer := &grpcServer{
		destinationClient:   destinationClient,
		k8sAPI:              k8sAPI,
		controllerNamespace: controllerNamespace,
		clusterDomain:       clusterDomain,
	}

	pb.RegisterApiServer(prometheus.NewGrpcServer(), grpcServer)

	return grpcServer
}

func (*grpcServer) Version(ctx context.Context, req *pb.Empty) (*pb.VersionInfo, error) {
	return &pb.VersionInfo{GoVersion: runtime.Version(), ReleaseVersion: version.Version, BuildDate: "1970-01-01T00:00:00Z"}, nil
}

// Pass through to Destination service
func (s *grpcServer) Get(req *destinationPb.GetDestination, stream destinationPb.Destination_GetServer) error {
	destinationStream := stream.(destinationServer)
	destinationClient, err := s.destinationClient.Get(destinationStream.Context(), req)
	if err != nil {
		log.Errorf("Unexpected error on Destination.Get [%v]: %v", req, err)
		return err
	}
	for {
		select {
		case <-destinationStream.Context().Done():
			return nil
		default:
			event, err := destinationClient.Recv()
			if err != nil {
				return err
			}
			destinationStream.Send(event)
		}
	}
}

func (s *grpcServer) GetProfile(_ *destinationPb.GetDestination, _ destinationPb.Destination_GetProfileServer) error {
	// Not implemented in the Public API. Instead, the proxies should reach the Destination gRPC server directly.
	return errors.New("Not implemented")
}
