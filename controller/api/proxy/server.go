package proxy

import (
	"io"
	"net"

	common "github.com/runconduit/conduit/controller/gen/common"
	destination "github.com/runconduit/conduit/controller/gen/proxy/destination"
	"github.com/runconduit/conduit/controller/util"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type (
	server struct {
		destinationClient destination.DestinationClient
	}
)

func (s *server) Get(dest *common.Destination, stream destination.Destination_GetServer) error {
	log := log.WithFields(
		log.Fields{
			"scheme": dest.Scheme,
			"path":   dest.Path,
		})
	log.Debug("Get")

	rsp, err := s.destinationClient.Get(stream.Context(), dest)
	if err != nil {
		log.Error(err)
		return err
	}
	for {
		update, err := rsp.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Error(err)
			return err
		}

		log.Debug("Get update: %v", update)
		stream.Send(update)
	}

	log.Debug("Get complete")
	return nil
}

/*
 * The Proxy-API server accepts requests from proxy instances and forwards those
 * requests to the appropriate controller service.
 */
func NewServer(addr string, destinationClient destination.DestinationClient) (*grpc.Server, net.Listener, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	s := util.NewGrpcServer()
	srv := server{destinationClient: destinationClient}
	destination.RegisterDestinationServer(s, &srv)

	return s, lis, nil
}
