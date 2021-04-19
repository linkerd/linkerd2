package destination

import (
	"context"
	"errors"
	"io"
	"sync"

	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"google.golang.org/grpc"
)

// MockAPIClient satisfies the destination API's interfaces
type MockAPIClient struct {
	ErrorToReturn                error
	DestinationGetClientToReturn destinationPb.Destination_GetClient
}

// Get provides a mock of a destination API method.
func (c *MockAPIClient) Get(ctx context.Context, in *destinationPb.GetDestination, opts ...grpc.CallOption) (destinationPb.Destination_GetClient, error) {
	return c.DestinationGetClientToReturn, c.ErrorToReturn
}

// GetProfile provides a mock of a destination API method
func (c *MockAPIClient) GetProfile(ctx context.Context, _ *destinationPb.GetDestination, _ ...grpc.CallOption) (destinationPb.Destination_GetProfileClient, error) {
	// Not implemented through this client. The proxies use the gRPC server directly instead.
	return nil, errors.New("Not implemented")
}

// MockDestinationGetClient satisfies the Destination_GetClient gRPC interface.
type MockDestinationGetClient struct {
	UpdatesToReturn []destinationPb.Update
	ErrorsToReturn  []error
	grpc.ClientStream
	sync.Mutex
}

// Recv satisfies the Destination_GetClient.Recv() gRPC method.
func (a *MockDestinationGetClient) Recv() (*destinationPb.Update, error) {
	a.Lock()
	defer a.Unlock()
	var updatePopped *destinationPb.Update
	var errorPopped error
	if len(a.UpdatesToReturn) == 0 && len(a.ErrorsToReturn) == 0 {
		return nil, io.EOF
	}
	if len(a.UpdatesToReturn) != 0 {
		updatePopped, a.UpdatesToReturn = &a.UpdatesToReturn[0], a.UpdatesToReturn[1:]
	}
	if len(a.ErrorsToReturn) != 0 {
		errorPopped, a.ErrorsToReturn = a.ErrorsToReturn[0], a.ErrorsToReturn[1:]
	}

	return updatePopped, errorPopped
}

// AuthorityEndpoints holds the details for the Endpoints associated to an authority
type AuthorityEndpoints struct {
	Namespace string
	ServiceID string
	Pods      []PodDetails
}

// PodDetails holds the details for pod associated to an Endpoint
type PodDetails struct {
	Name string
	IP   uint32
	Port uint32
}

// BuildAddrSet converts AuthorityEndpoints into its protobuf representation
func BuildAddrSet(endpoint AuthorityEndpoints) *destinationPb.WeightedAddrSet {
	addrs := make([]*destinationPb.WeightedAddr, 0)
	for _, pod := range endpoint.Pods {
		addr := &net.TcpAddress{
			Ip:   &net.IPAddress{Ip: &net.IPAddress_Ipv4{Ipv4: pod.IP}},
			Port: pod.Port,
		}
		labels := map[string]string{"pod": pod.Name}
		weightedAddr := &destinationPb.WeightedAddr{Addr: addr, MetricLabels: labels}
		addrs = append(addrs, weightedAddr)
	}
	labels := map[string]string{"namespace": endpoint.Namespace, "service": endpoint.ServiceID}
	return &destinationPb.WeightedAddrSet{Addrs: addrs, MetricLabels: labels}
}
