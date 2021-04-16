package public

import (
	"github.com/linkerd/linkerd2/controller/k8s"
)

// Server specifies the interface the Public API server should implement
type Server interface {
}

type grpcServer struct {
	k8sAPI              *k8s.API
	controllerNamespace string
	clusterDomain       string
}

func newGrpcServer(
	k8sAPI *k8s.API,
	controllerNamespace string,
	clusterDomain string,
) *grpcServer {

	grpcServer := &grpcServer{
		k8sAPI:              k8sAPI,
		controllerNamespace: controllerNamespace,
		clusterDomain:       clusterDomain,
	}

	return grpcServer
}
