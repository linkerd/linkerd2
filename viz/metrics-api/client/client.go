package client

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/golang/protobuf/proto"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	log "github.com/sirupsen/logrus"
	"go.opencensus.io/plugin/ochttp"
	"google.golang.org/grpc"
)

const (
	// APIRoot is the API's root path. It must be absolute (with a leading slash)
	APIRoot = "/"

	apiVersion = "v1"

	// APIPrefix is the prefix all the API endpoints must share. It must be relative
	// (without a leading slash)
	APIPrefix = "api/" + apiVersion + "/"

	apiPort       = 8085
	apiDeployment = "metrics-api"
)

type grpcOverHTTPClient struct {
	serverURL  *url.URL
	httpClient *http.Client
	namespace  string
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

func (c *grpcOverHTTPClient) SelfCheck(ctx context.Context, req *pb.SelfCheckRequest, _ ...grpc.CallOption) (*pb.SelfCheckResponse, error) {
	var msg pb.SelfCheckResponse
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

func newClient(apiURL *url.URL, httpClientToUse *http.Client, namespace string) (pb.ApiClient, error) {
	if !apiURL.IsAbs() {
		return nil, fmt.Errorf("server URL must be absolute, was [%s]", apiURL.String())
	}

	serverURL := apiURL.ResolveReference(&url.URL{Path: APIPrefix})

	log.Debugf("Expecting API to be served over [%s]", serverURL)

	return &grpcOverHTTPClient{
		serverURL:  serverURL,
		httpClient: httpClientToUse,
		namespace:  namespace,
	}, nil
}

// NewInternalClient creates a new Viz API client intended to run inside a
// Kubernetes cluster.
func NewInternalClient(namespace string, kubeAPIHost string) (pb.ApiClient, error) {
	apiURL, err := url.Parse(fmt.Sprintf("http://%s/", kubeAPIHost))
	if err != nil {
		return nil, err
	}

	return newClient(apiURL, &http.Client{Transport: &ochttp.Transport{}}, namespace)
}

// NewExternalClient creates a new Viz API client intended to run from
// outside a Kubernetes cluster.
func NewExternalClient(ctx context.Context, namespace string, kubeAPI *k8s.KubernetesAPI) (pb.ApiClient, error) {
	portforward, err := k8s.NewPortForward(
		ctx,
		kubeAPI,
		namespace,
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

	return newClient(apiURL, httpClientToUse, namespace)
}
