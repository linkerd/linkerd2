package discovery

import (
	"context"
	"strings"

	pb "github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	"github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/addr"
	"google.golang.org/grpc"
)

// MockDiscoveryClient satisfies the Discovery API's gRPC interface
// (discovery.DiscoveryClient).
type MockDiscoveryClient struct {
	ErrorToReturn             error
	EndpointsResponseToReturn *pb.EndpointsResponse
}

// Endpoints provides a mock of a Discovery API method.
func (c *MockDiscoveryClient) Endpoints(ctx context.Context, in *pb.EndpointsParams, _ ...grpc.CallOption) (*pb.EndpointsResponse, error) {
	return c.EndpointsResponseToReturn, c.ErrorToReturn
}

// GenEndpointsResponse generates a mock Public API Endpoints object.
// identities is a list of "pod.namespace" strings
func GenEndpointsResponse(identities []string) *pb.EndpointsResponse {
	resp := &pb.EndpointsResponse{
		ServicePorts: make(map[string]*pb.ServicePort),
	}
	for _, identity := range identities {
		parts := strings.SplitN(identity, ".", 2)
		pod := parts[0]
		ns := parts[1]
		ip, _ := addr.ParsePublicIPV4("1.2.3.4")
		resp.ServicePorts[identity] = &pb.ServicePort{
			PortEndpoints: map[uint32]*pb.PodAddresses{
				8080: {
					PodAddresses: []*pb.PodAddress{
						{
							Addr: &public.TcpAddress{
								Ip:   ip,
								Port: 8080,
							},
							Pod: &public.Pod{
								Name:            ns + "/" + pod,
								Status:          "running",
								PodIP:           "1.2.3.4",
								ResourceVersion: "1234",
							},
						},
					},
				},
			},
		}
	}

	return resp
}
