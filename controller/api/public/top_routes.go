package public

import (
	"context"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *grpcServer) TopRoutes(ctx context.Context, req *pb.TopRoutesRequest) (*pb.TopRoutesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Not Implemented")
}
