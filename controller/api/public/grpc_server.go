package public

import (
	"errors"

	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/k8s"
	log "github.com/sirupsen/logrus"
)

// Server specifies the interface the Public API server should implement
type Server interface {
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

	return grpcServer
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
