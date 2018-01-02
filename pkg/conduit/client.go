package conduit

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
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

type (
	Config struct {
		ServerURL *url.URL
	}

	client struct {
		serverURL *url.URL
		client    *http.Client
	}

	tapClient struct {
		ctx    context.Context
		reader *bufio.Reader
	}
)

func NewInternalClient(config *Config) (pb.ApiClient, error) {
	if !config.ServerURL.IsAbs() {
		return nil, fmt.Errorf("server URL must be absolute, was [%s]", config.ServerURL.String())
	}

	return &client{
		serverURL: config.ServerURL.ResolveReference(&url.URL{Path: ApiPrefix}),
		client:    http.DefaultClient,
	}, nil
}

func NewExternalClient(config *Config, k8sApi k8s.KubernetesApi) (pb.ApiClient, error) {
	if !config.ServerURL.IsAbs() {
		return nil, fmt.Errorf("server URL must be absolute, was [%s]", config.ServerURL.String())
	}

	k8sRestClient, err := k8sApi.NewClient()
	if err != nil {
		return nil, err
	}

	return &client{
		serverURL: config.ServerURL.ResolveReference(&url.URL{Path: ApiPrefix}),
		client:    k8sRestClient,
	}, nil
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

func clientUnmarshal(r *bufio.Reader, errorMsg string, msg proto.Message) error {
	byteSize := make([]byte, 4)
	_, err := r.Read(byteSize)
	if err != nil {
		return err
	}

	size := binary.LittleEndian.Uint32(byteSize)
	bytes := make([]byte, size)
	_, err = io.ReadFull(r, bytes)
	if err != nil {
		return err
	}

	if errorMsg != "" {
		var apiError pb.ApiError
		err = proto.Unmarshal(bytes, &apiError)
		if err != nil {
			return err
		}
		return fmt.Errorf("%s: %s", errorMsg, apiError.Error)
	}

	return proto.Unmarshal(bytes, msg)
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
