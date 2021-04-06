package public

import (
	"context"
	"runtime"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/version"
)

// Server specifies the interface the Public API server should implement
type Server interface {
	pb.ApiServer
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

	pb.RegisterApiServer(prometheus.NewGrpcServer(), grpcServer)

	return grpcServer
}

func (*grpcServer) Version(ctx context.Context, req *pb.Empty) (*pb.VersionInfo, error) {
	return &pb.VersionInfo{GoVersion: runtime.Version(), ReleaseVersion: version.Version, BuildDate: "1970-01-01T00:00:00Z"}, nil
}
