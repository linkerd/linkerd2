package util

import (
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
)

// returns a grpc server pre-configured with prometheus interceptors
func NewGrpcServer() *grpc.Server {
	server := grpc.NewServer(
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
		grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
	)
	grpc_prometheus.Register(server)
	return server
}
