package public

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/golang/protobuf/proto"
	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	ApiRoot                 = "/" // Must be absolute (with a leading slash).
	ApiVersion              = "v1"
	JsonContentType         = "application/json"
	ApiPrefix               = "api/" + ApiVersion + "/" // Must be relative (without a leading slash).
	ProtobufContentType     = "application/octet-stream"
	ErrorHeader             = "conduit-error"
	ConduitApiSubsystemName = "conduit-api"
)

type grpcOverHttpClient struct {
	serverURL  *url.URL
	httpClient *http.Client
}

type tapClient struct {
	ctx    context.Context
	reader *bufio.Reader
}

func (c *grpcOverHttpClient) Stat(ctx context.Context, req *pb.MetricRequest, _ ...grpc.CallOption) (*pb.MetricResponse, error) {
	var msg pb.MetricResponse
	err := c.apiRequest(ctx, "Stat", req, &msg)
	return &msg, err
}

func (c *grpcOverHttpClient) Version(ctx context.Context, req *pb.Empty, _ ...grpc.CallOption) (*pb.VersionInfo, error) {
	var msg pb.VersionInfo
	err := c.apiRequest(ctx, "Version", req, &msg)
	return &msg, err
}

func (c *grpcOverHttpClient) SelfCheck(ctx context.Context, req *common.SelfCheckRequest, _ ...grpc.CallOption) (*common.SelfCheckResponse, error) {
	var msg common.SelfCheckResponse
	err := c.apiRequest(ctx, "SelfCheck", req, &msg)
	return &msg, err
}

func (c *grpcOverHttpClient) ListPods(ctx context.Context, req *pb.Empty, _ ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	var msg pb.ListPodsResponse
	err := c.apiRequest(ctx, "ListPods", req, &msg)
	return &msg, err
}

func (c *grpcOverHttpClient) Tap(ctx context.Context, req *pb.TapRequest, _ ...grpc.CallOption) (pb.Api_TapClient, error) {
	url := c.endpointNameToPublicApiUrl("Tap")
	rsp, err := c.post(ctx, url, req)
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
	err := fromByteStreamToProtocolBuffers(c.reader, "", &msg)
	return &msg, err
}

// satisfy the pb.Api_TapClient interface
func (c tapClient) Header() (metadata.MD, error) { return nil, nil }
func (c tapClient) Trailer() metadata.MD         { return nil }
func (c tapClient) CloseSend() error             { return nil }
func (c tapClient) Context() context.Context     { return c.ctx }
func (c tapClient) SendMsg(interface{}) error    { return nil }
func (c tapClient) RecvMsg(interface{}) error    { return nil }

func (c *grpcOverHttpClient) apiRequest(ctx context.Context, endpoint string, req proto.Message, rsp proto.Message) error {
	url := c.endpointNameToPublicApiUrl(endpoint)

	httpRsp, err := c.post(ctx, url, req)
	if err != nil {
		return err
	}

	clientSideErrorStatusCode := httpRsp.StatusCode >= 400 && httpRsp.StatusCode <= 499
	if clientSideErrorStatusCode {
		return fmt.Errorf("POST to Conduit API endpoint [%s] returned HTTP status [%s]", url, httpRsp.Status)
	}

	defer httpRsp.Body.Close()

	reader := bufio.NewReader(httpRsp.Body)
	errorMsg := httpRsp.Header.Get(ErrorHeader)
	return fromByteStreamToProtocolBuffers(reader, errorMsg, rsp)
}

func (c *grpcOverHttpClient) post(ctx context.Context, url *url.URL, req proto.Message) (*http.Response, error) {
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

	return c.httpClient.Do(httpReq.WithContext(ctx))
}

func (c *grpcOverHttpClient) endpointNameToPublicApiUrl(endpoint string) *url.URL {
	return c.serverURL.ResolveReference(&url.URL{Path: endpoint})
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

	return &grpcOverHttpClient{
		serverURL:  apiURL.ResolveReference(&url.URL{Path: ApiPrefix}),
		httpClient: httpClientToUse,
	}, nil
}

func fromByteStreamToProtocolBuffers(byteStreamContainingMessage *bufio.Reader, errorMessageReturnedAsMetadata string, out proto.Message) error {
	//TODO: why the magic number 4?
	byteSize := make([]byte, 4)

	//TODO: why is this necessary?
	_, err := byteStreamContainingMessage.Read(byteSize)
	if err != nil {
		return fmt.Errorf("error reading byte stream header: %v", err)
	}

	size := binary.LittleEndian.Uint32(byteSize)
	bytes := make([]byte, size)
	_, err = io.ReadFull(byteStreamContainingMessage, bytes)
	if err != nil {
		return fmt.Errorf("error reading byte stream content: %v", err)
	}

	if errorMessageReturnedAsMetadata != "" {
		var apiError pb.ApiError
		err = proto.Unmarshal(bytes, &apiError)
		if err != nil {
			return fmt.Errorf("error unmarshalling error from byte stream: %v", err)
		}
		return fmt.Errorf("%s: %s", errorMessageReturnedAsMetadata, apiError.Error)
	}

	err = proto.Unmarshal(bytes, out)
	if err != nil {
		return fmt.Errorf("error unmarshalling bytes: %v", err)
	} else {
		return nil
	}
}
