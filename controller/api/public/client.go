package public

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/golang/protobuf/proto"
	"github.com/linkerd/linkerd2/controller/api"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	configPb "github.com/linkerd/linkerd2/controller/gen/config"
	discoveryPb "github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	apiRoot = "/" // Must be absolute (with a leading slash).
)

// APIClient wraps two gRPC client interfaces:
// 1) public.Api
// 2) controller/discovery.Discovery
// This aligns with Public API Server's `handler` struct supporting both gRPC
// servers.
type APIClient interface {
	pb.ApiClient
	discoveryPb.DiscoveryClient
}

type grpcOverHTTPClient struct {
	serverURL             *url.URL
	httpClient            *http.Client
	controlPlaneNamespace string
}

func (c *grpcOverHTTPClient) StatSummary(ctx context.Context, req *pb.StatSummaryRequest, _ ...grpc.CallOption) (*pb.StatSummaryResponse, error) {
	var msg pb.StatSummaryResponse
	err := c.apiRequest(ctx, "StatSummary", req, &msg)
	return &msg, err
}

func (c *grpcOverHTTPClient) Edges(ctx context.Context, req *pb.EdgesRequest, _ ...grpc.CallOption) (*pb.EdgesResponse, error) {
	var msg pb.EdgesResponse
	err := c.apiRequest(ctx, "Edges", req, &msg)
	return &msg, err
}

func (c *grpcOverHTTPClient) TopRoutes(ctx context.Context, req *pb.TopRoutesRequest, _ ...grpc.CallOption) (*pb.TopRoutesResponse, error) {
	var msg pb.TopRoutesResponse
	err := c.apiRequest(ctx, "TopRoutes", req, &msg)
	return &msg, err
}

func (c *grpcOverHTTPClient) Version(ctx context.Context, req *pb.Empty, _ ...grpc.CallOption) (*pb.VersionInfo, error) {
	var msg pb.VersionInfo
	err := c.apiRequest(ctx, "Version", req, &msg)
	return &msg, err
}

func (c *grpcOverHTTPClient) SelfCheck(ctx context.Context, req *healthcheckPb.SelfCheckRequest, _ ...grpc.CallOption) (*healthcheckPb.SelfCheckResponse, error) {
	var msg healthcheckPb.SelfCheckResponse
	err := c.apiRequest(ctx, "SelfCheck", req, &msg)
	return &msg, err
}

func (c *grpcOverHTTPClient) Config(ctx context.Context, req *pb.Empty, _ ...grpc.CallOption) (*configPb.All, error) {
	var msg configPb.All
	err := c.apiRequest(ctx, "Config", req, &msg)
	return &msg, err
}

func (c *grpcOverHTTPClient) ListPods(ctx context.Context, req *pb.ListPodsRequest, _ ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	var msg pb.ListPodsResponse
	err := c.apiRequest(ctx, "ListPods", req, &msg)
	return &msg, err
}

func (c *grpcOverHTTPClient) ListServices(ctx context.Context, req *pb.ListServicesRequest, _ ...grpc.CallOption) (*pb.ListServicesResponse, error) {
	var msg pb.ListServicesResponse
	err := c.apiRequest(ctx, "ListServices", req, &msg)
	return &msg, err
}

func (c *grpcOverHTTPClient) Tap(ctx context.Context, req *pb.TapRequest, _ ...grpc.CallOption) (pb.Api_TapClient, error) {
	return nil, status.Error(codes.Unimplemented, "Tap is deprecated, use TapByResource")
}

func (c *grpcOverHTTPClient) TapByResource(ctx context.Context, req *pb.TapByResourceRequest, _ ...grpc.CallOption) (pb.Api_TapByResourceClient, error) {
	url := api.EndpointNameToPublicAPIURL(c.serverURL, "TapByResource")
	httpRsp, err := api.HTTPPost(ctx, c.httpClient, url, req)
	if err != nil {
		return nil, err
	}

	if err := api.CheckIfResponseHasError(httpRsp); err != nil {
		httpRsp.Body.Close()
		return nil, err
	}

	go func() {
		<-ctx.Done()
		log.Debug("Closing response body after context marked as done")
		httpRsp.Body.Close()
	}()

	return &tapClient{api.StreamClient{Ctx: ctx, Reader: bufio.NewReader(httpRsp.Body)}}, nil
}

func (c *grpcOverHTTPClient) Endpoints(ctx context.Context, req *discoveryPb.EndpointsParams, _ ...grpc.CallOption) (*discoveryPb.EndpointsResponse, error) {
	var msg discoveryPb.EndpointsResponse
	err := c.apiRequest(ctx, "Endpoints", req, &msg)
	return &msg, err
}

func (c *grpcOverHTTPClient) apiRequest(ctx context.Context, endpoint string, req proto.Message, protoResponse proto.Message) error {
	url := api.EndpointNameToPublicAPIURL(c.serverURL, endpoint)

	log.Debugf("Making gRPC-over-HTTP call to [%s] [%+v]", url.String(), req)
	httpRsp, err := api.HTTPPost(ctx, c.httpClient, url, req)
	if err != nil {
		return err
	}
	defer httpRsp.Body.Close()
	log.Debugf("gRPC-over-HTTP call returned status [%s] and content length [%d]", httpRsp.Status, httpRsp.ContentLength)

	if err := api.CheckIfResponseHasError(httpRsp); err != nil {
		return err
	}

	reader := bufio.NewReader(httpRsp.Body)
	return api.FromByteStreamToProtocolBuffers(reader, protoResponse)
}

type tapClient struct {
	api.StreamClient
}

func (c tapClient) Recv() (*pb.TapEvent, error) {
	var msg pb.TapEvent
	err := api.FromByteStreamToProtocolBuffers(c.Reader, &msg)
	return &msg, err
}

func newClient(apiURL *url.URL, httpClientToUse *http.Client, controlPlaneNamespace string) (APIClient, error) {
	if !apiURL.IsAbs() {
		return nil, fmt.Errorf("server URL must be absolute, was [%s]", apiURL.String())
	}

	serverURL := apiURL.ResolveReference(&url.URL{Path: api.ApiPrefix})

	log.Debugf("Expecting API to be served over [%s]", serverURL)

	return &grpcOverHTTPClient{
		serverURL:             serverURL,
		httpClient:            httpClientToUse,
		controlPlaneNamespace: controlPlaneNamespace,
	}, nil
}

// NewInternalClient creates a new Public API client intended to run inside a
// Kubernetes cluster.
func NewInternalClient(controlPlaneNamespace string, kubeAPIHost string) (APIClient, error) {
	apiURL, err := url.Parse(fmt.Sprintf("http://%s/", kubeAPIHost))
	if err != nil {
		return nil, err
	}

	return newClient(apiURL, http.DefaultClient, controlPlaneNamespace)
}

// NewExternalPublicAPIClient creates a new Public API client intended to run from
// outside a Kubernetes cluster.
func NewExternalPublicAPIClient(controlPlaneNamespace string, kubeAPI *k8s.KubernetesAPI) (APIClient, error) {
	portforward, err := k8s.NewPortForward(
		kubeAPI,
		controlPlaneNamespace,
		api.ApiDeployment,
		0,
		api.ApiPort,
		false,
	)
	if err != nil {
		return nil, err
	}
	apiURL, httpClientToUse, err := portforward.Init(controlPlaneNamespace, kubeAPI)
	if err != nil {
		return nil, err
	}

	return newClient(apiURL, httpClientToUse, controlPlaneNamespace)
}
