package conduit

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"net/url"

	"github.com/runconduit/conduit/pkg/k8s"

	"github.com/golang/protobuf/proto"
	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type client struct {
	serverURL *url.URL
	client    *http.Client
}

type tapClient struct {
	ctx    context.Context
	reader *bufio.Reader
}

func (c *client) Stat(ctx context.Context, req *pb.MetricRequest, _ ...grpc.CallOption) (*pb.MetricResponse, error) {
	var msg pb.MetricResponse
	err := c.apiRequest(ctx, "Stat", req, &msg)
	return &msg, err
}

func (c *client) Version(ctx context.Context, req *pb.Empty, _ ...grpc.CallOption) (*pb.VersionInfo, error) {
	var msg pb.VersionInfo
	err := c.apiRequest(ctx, "Version", req, &msg)
	return &msg, err
}

func (c *client) ListPods(ctx context.Context, req *pb.Empty, _ ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	var msg pb.ListPodsResponse
	err := c.apiRequest(ctx, "ListPods", req, &msg)
	return &msg, err
}

func (c *client) Tap(ctx context.Context, req *pb.TapRequest, _ ...grpc.CallOption) (pb.Api_TapClient, error) {
	rsp, err := c.post(ctx, "Tap", req)
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		rsp.Body.Close()
	}()

	return &tapClient{ctx: ctx, reader: bufio.NewReader(rsp.Body)}, nil
}

func (c tapClient) Recv() (*common.TapEvent, error) {
	var msg common.TapEvent
	err := clientUnmarshal(c.reader, "", &msg)
	return &msg, err
}

// satisfy the pb.Api_TapClient interface
func (c tapClient) Header() (metadata.MD, error) { return nil, nil }
func (c tapClient) Trailer() metadata.MD         { return nil }
func (c tapClient) CloseSend() error             { return nil }
func (c tapClient) Context() context.Context     { return c.ctx }
func (c tapClient) SendMsg(interface{}) error    { return nil }
func (c tapClient) RecvMsg(interface{}) error    { return nil }

func (c *client) apiRequest(ctx context.Context, endpoint string, req proto.Message, rsp proto.Message) error {
	httpRsp, err := c.post(ctx, endpoint, req)
	if err != nil {
		return err
	}
	defer httpRsp.Body.Close()

	reader := bufio.NewReader(httpRsp.Body)
	errorMsg := httpRsp.Header.Get(ErrorHeader)
	return clientUnmarshal(reader, errorMsg, rsp)
}

func (c *client) post(ctx context.Context, endpoint string, req proto.Message) (*http.Response, error) {
	url := c.serverURL.ResolveReference(&url.URL{Path: endpoint})

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

	return c.client.Do(httpReq.WithContext(ctx))
}

func NewInternalClient(kubernetesApiHost string) (pb.ApiClient, error) {
	apiURL := &url.URL{
		Scheme: "http",
		Host:   kubernetesApiHost,
		Path:   "/",
	}

	return newClient(apiURL, http.DefaultClient)
}

func NewExternalClient(controlPlaneNamespace string, kubeApi k8s.KubernetesApi) (pb.ApiClient, error) {
	apiURL, err := kubeApi.UrlFor(controlPlaneNamespace, "/services/http:api:http/proxy/")
	if err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	httpClientToUse, err := kubeApi.NewClient()
	if err != nil {
		return nil, err
	}

	return newClient(apiURL, httpClientToUse)
}

func newClient(apiURL *url.URL, httpClientToUse *http.Client) (pb.ApiClient, error) {

	if !apiURL.IsAbs() {
		return nil, fmt.Errorf("server URL must be absolute, was [%s]", apiURL.String())
	}

	return &client{
		serverURL: apiURL.ResolveReference(&url.URL{Path: ApiPrefix}),
		client:    httpClientToUse,
	}, nil
}
