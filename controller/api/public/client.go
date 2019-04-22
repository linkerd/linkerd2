package public

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/golang/protobuf/proto"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	configPb "github.com/linkerd/linkerd2/controller/gen/config"
	discoveryPb "github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	apiRoot       = "/" // Must be absolute (with a leading slash).
	apiVersion    = "v1"
	apiPrefix     = "api/" + apiVersion + "/" // Must be relative (without a leading slash).
	apiPort       = 8085
	apiDeployment = "linkerd-controller"
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

func (c *grpcOverHTTPClient) Edges(ctx context.Context, req *pb.StatSummaryRequest, _ ...grpc.CallOption) (*pb.EdgesResponse, error) {
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
	url := c.endpointNameToPublicAPIURL("TapByResource")
	httpRsp, err := c.post(ctx, url, req)
	if err != nil {
		return nil, err
	}

	if err := checkIfResponseHasError(httpRsp); err != nil {
		httpRsp.Body.Close()
		return nil, err
	}

	go func() {
		<-ctx.Done()
		log.Debug("Closing response body after context marked as done")
		httpRsp.Body.Close()
	}()

	return &tapClient{ctx: ctx, reader: bufio.NewReader(httpRsp.Body)}, nil
}

func (c *grpcOverHTTPClient) Endpoints(ctx context.Context, req *discoveryPb.EndpointsParams, _ ...grpc.CallOption) (*discoveryPb.EndpointsResponse, error) {
	var msg discoveryPb.EndpointsResponse
	err := c.apiRequest(ctx, "Endpoints", req, &msg)
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

	if err := checkIfResponseHasError(httpRsp); err != nil {
		return err
	}

	reader := bufio.NewReader(httpRsp.Body)
	return fromByteStreamToProtocolBuffers(reader, protoResponse)
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

type tapClient struct {
	ctx    context.Context
	reader *bufio.Reader
}

func (c tapClient) Recv() (*pb.TapEvent, error) {
	var msg pb.TapEvent
	err := fromByteStreamToProtocolBuffers(c.reader, &msg)
	return &msg, err
}

// satisfy the pb.Api_TapClient interface
func (c tapClient) Header() (metadata.MD, error) { return nil, nil }
func (c tapClient) Trailer() metadata.MD         { return nil }
func (c tapClient) CloseSend() error             { return nil }
func (c tapClient) Context() context.Context     { return c.ctx }
func (c tapClient) SendMsg(interface{}) error    { return nil }
func (c tapClient) RecvMsg(interface{}) error    { return nil }

func fromByteStreamToProtocolBuffers(byteStreamContainingMessage *bufio.Reader, out proto.Message) error {
	messageAsBytes, err := deserializePayloadFromReader(byteStreamContainingMessage)
	if err != nil {
		return fmt.Errorf("error reading byte stream header: %v", err)
	}

	err = proto.Unmarshal(messageAsBytes, out)
	if err != nil {
		return fmt.Errorf("error unmarshalling array of [%d] bytes error: %v", len(messageAsBytes), err)
	}

	return nil
}

func newClient(apiURL *url.URL, httpClientToUse *http.Client, controlPlaneNamespace string) (APIClient, error) {
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

// NewInternalClient creates a new Public API client intended to run inside a
// Kubernetes cluster.
func NewInternalClient(controlPlaneNamespace string, kubeAPIHost string) (APIClient, error) {
	apiURL, err := url.Parse(fmt.Sprintf("http://%s/", kubeAPIHost))
	if err != nil {
		return nil, err
	}

	return newClient(apiURL, http.DefaultClient, controlPlaneNamespace)
}

// NewExternalClient creates a new Public API client intended to run from
// outside a Kubernetes cluster.
func NewExternalClient(controlPlaneNamespace string, kubeAPI *k8s.KubernetesAPI) (APIClient, error) {
	portforward, err := k8s.NewPortForward(
		kubeAPI,
		controlPlaneNamespace,
		apiDeployment,
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

	log.Debugf("Starting port forward on [%s]", apiURL)

	wait := make(chan error, 1)

	go func() {
		if err := portforward.Run(); err != nil {
			wait <- err
		}

		portforward.Stop()
	}()

	select {
	case <-portforward.Ready():
		log.Debugf("Port forward initialised")

		break

	case err := <-wait:
		log.Debugf("Port forward failed: %v", err)

		return nil, err
	}

	httpClientToUse, err := kubeAPI.NewClient()
	if err != nil {
		return nil, err
	}

	return newClient(apiURL, httpClientToUse, controlPlaneNamespace)
}
