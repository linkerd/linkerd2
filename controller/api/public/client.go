package public

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/golang/protobuf/proto"
	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	publicPb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	healthcheckPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz/healthcheck"
	log "github.com/sirupsen/logrus"
	"go.opencensus.io/plugin/ochttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	apiRoot       = "/" // Must be absolute (with a leading slash).
	apiVersion    = "v1"
	apiPrefix     = "api/" + apiVersion + "/" // Must be relative (without a leading slash).
	apiPort       = 8085
	apiDeployment = "linkerd-controller"
)

// PublicAPIClient wraps one gRPC client interface for publicPb.Api:
// TODO: remove stuttering name when viz api code moves to /viz
// nolint
type PublicAPIClient interface {
	publicPb.ApiClient
	destinationPb.DestinationClient
}

// VizAPIClient wraps one gRPC client interface for pb.Api:
type VizAPIClient interface {
	pb.ApiClient
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

func (c *grpcOverHTTPClient) Gateways(ctx context.Context, req *pb.GatewaysRequest, _ ...grpc.CallOption) (*pb.GatewaysResponse, error) {
	var msg pb.GatewaysResponse
	err := c.apiRequest(ctx, "Gateways", req, &msg)
	return &msg, err
}

func (c *grpcOverHTTPClient) Version(ctx context.Context, req *publicPb.Empty, _ ...grpc.CallOption) (*publicPb.VersionInfo, error) {
	var msg publicPb.VersionInfo
	err := c.apiRequest(ctx, "Version", req, &msg)
	return &msg, err
}

func (c *grpcOverHTTPClient) SelfCheck(ctx context.Context, req *healthcheckPb.SelfCheckRequest, _ ...grpc.CallOption) (*healthcheckPb.SelfCheckResponse, error) {
	var msg healthcheckPb.SelfCheckResponse
	err := c.apiRequest(ctx, "SelfCheck", req, &msg)
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
	return nil, status.Error(codes.Unimplemented, "Tap is deprecated in public API, use tap APIServer")
}

func (c *grpcOverHTTPClient) TapByResource(ctx context.Context, req *pb.TapByResourceRequest, _ ...grpc.CallOption) (pb.Api_TapByResourceClient, error) {
	return nil, status.Error(codes.Unimplemented, "Tap is deprecated in public API, use tap APIServer")
}

func (c *grpcOverHTTPClient) Get(ctx context.Context, req *destinationPb.GetDestination, _ ...grpc.CallOption) (destinationPb.Destination_GetClient, error) {
	url := c.endpointNameToPublicAPIURL("DestinationGet")
	httpRsp, err := c.post(ctx, url, req)
	if err != nil {
		return nil, err
	}

	client, err := getStreamClient(ctx, httpRsp)
	if err != nil {
		return nil, err
	}

	return &destinationClient{client}, nil
}

func (c *grpcOverHTTPClient) GetProfile(ctx context.Context, _ *destinationPb.GetDestination, _ ...grpc.CallOption) (destinationPb.Destination_GetProfileClient, error) {
	// Not implemented through this client. The proxies use the gRPC server directly instead.
	return nil, errors.New("Not implemented")
}

func (c *grpcOverHTTPClient) apiRequest(ctx context.Context, endpoint string, req proto.Message, protoResponse proto.Message) error {
	url := c.endpointNameToPublicAPIURL(endpoint)

	log.Debugf("Making gRPC-over-HTTP call to [%s] [%+v]", url.String(), req)
	httpRsp, err := c.post(ctx, url, req)
	if err != nil {
		return err
	}
	defer httpRsp.Body.Close()
	log.Debugf("gRPC-over-HTTP call returned status [%s] and content length [%d]", httpRsp.Status, httpRsp.ContentLength)

	if err := protohttp.CheckIfResponseHasError(httpRsp); err != nil {
		return err
	}

	reader := bufio.NewReader(httpRsp.Body)
	return protohttp.FromByteStreamToProtocolBuffers(reader, protoResponse)
}

func (c *grpcOverHTTPClient) post(ctx context.Context, url *url.URL, req proto.Message) (*http.Response, error) {
	reqBytes, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest(
		http.MethodPost,
		url.String(),
		bytes.NewReader(reqBytes),
	)
	if err != nil {
		return nil, err
	}

	rsp, err := c.httpClient.Do(httpReq.WithContext(ctx))
	if err != nil {
		log.Debugf("Error invoking [%s]: %v", url.String(), err)
	} else {
		log.Debugf("Response from [%s] had headers: %v", url.String(), rsp.Header)
	}

	return rsp, err
}

func (c *grpcOverHTTPClient) endpointNameToPublicAPIURL(endpoint string) *url.URL {
	return c.serverURL.ResolveReference(&url.URL{Path: endpoint})
}

type destinationClient struct {
	streamClient
}

func (c destinationClient) Recv() (*destinationPb.Update, error) {
	var msg destinationPb.Update
	err := protohttp.FromByteStreamToProtocolBuffers(c.reader, &msg)
	return &msg, err
}

func newClient(apiURL *url.URL, httpClientToUse *http.Client, controlPlaneNamespace string) (VizAPIClient, error) {
	if !apiURL.IsAbs() {
		return nil, fmt.Errorf("server URL must be absolute, was [%s]", apiURL.String())
	}

	serverURL := apiURL.ResolveReference(&url.URL{Path: apiPrefix})

	log.Debugf("Expecting API to be served over [%s]", serverURL)

	return &grpcOverHTTPClient{
		serverURL:             serverURL,
		httpClient:            httpClientToUse,
		controlPlaneNamespace: controlPlaneNamespace,
	}, nil
}

func newPublicClient(apiURL *url.URL, httpClientToUse *http.Client, controlPlaneNamespace string) (PublicAPIClient, error) {
	client, err := newClient(apiURL, httpClientToUse, controlPlaneNamespace)
	if err != nil {
		return nil, err
	}
	return client.(PublicAPIClient), nil
}

// NewInternalClient creates a new Viz API client intended to run inside a
// Kubernetes cluster.
func NewInternalClient(controlPlaneNamespace string, kubeAPIHost string) (VizAPIClient, error) {
	apiURL, err := url.Parse(fmt.Sprintf("http://%s/", kubeAPIHost))
	if err != nil {
		return nil, err
	}

	return newClient(apiURL, &http.Client{Transport: &ochttp.Transport{}}, controlPlaneNamespace)
}

// NewInternalPublicClient creates a new Public API client intended to run inside a
// Kubernetes cluster.
func NewInternalPublicClient(controlPlaneNamespace string, kubeAPIHost string) (PublicAPIClient, error) {
	client, err := NewInternalClient(controlPlaneNamespace, kubeAPIHost)
	if err != nil {
		return nil, err
	}
	return client.(PublicAPIClient), nil
}

// NewExternalClient creates a new Viz API client intended to run from
// outside a Kubernetes cluster.
func NewExternalClient(ctx context.Context, controlPlaneNamespace string, kubeAPI *k8s.KubernetesAPI) (VizAPIClient, error) {
	portforward, err := k8s.NewPortForward(
		ctx,
		kubeAPI,
		controlPlaneNamespace,
		apiDeployment,
		"localhost",
		0,
		apiPort,
		false,
	)
	if err != nil {
		return nil, err
	}

	apiURL, err := url.Parse(portforward.URLFor(""))
	if err != nil {
		return nil, err
	}

	if err = portforward.Init(); err != nil {
		return nil, err
	}

	httpClientToUse, err := kubeAPI.NewClient()
	if err != nil {
		return nil, err
	}

	return newClient(apiURL, httpClientToUse, controlPlaneNamespace)
}

// NewExternalPublicClient creates a new Public API client intended to run from
// outside a Kubernetes cluster.
func NewExternalPublicClient(ctx context.Context, controlPlaneNamespace string, kubeAPI *k8s.KubernetesAPI) (PublicAPIClient, error) {
	client, err := NewExternalClient(ctx, controlPlaneNamespace, kubeAPI)
	if err != nil {
		return nil, err
	}
	return client.(PublicAPIClient), nil
}
