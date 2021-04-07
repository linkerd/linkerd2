package public

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/golang/protobuf/proto"
	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	log "github.com/sirupsen/logrus"
	"go.opencensus.io/plugin/ochttp"
	"google.golang.org/grpc"
)

const (
	apiRoot       = "/" // Must be absolute (with a leading slash).
	apiVersion    = "v1"
	apiPrefix     = "api/" + apiVersion + "/" // Must be relative (without a leading slash).
	apiPort       = 8085
	apiDeployment = "linkerd-controller"
)

// Client wraps one gRPC client interface for destination
type Client interface {
	destinationPb.DestinationClient
}

type grpcOverHTTPClient struct {
	serverURL             *url.URL
	httpClient            *http.Client
	controlPlaneNamespace string
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

func newClient(apiURL *url.URL, httpClientToUse *http.Client, controlPlaneNamespace string) (Client, error) {
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

// NewInternalClient creates a new public API client intended to run inside a
// Kubernetes cluster.
func NewInternalClient(controlPlaneNamespace string, kubeAPIHost string) (Client, error) {
	apiURL, err := url.Parse(fmt.Sprintf("http://%s/", kubeAPIHost))
	if err != nil {
		return nil, err
	}

	return newClient(apiURL, &http.Client{Transport: &ochttp.Transport{}}, controlPlaneNamespace)
}

// NewExternalClient creates a new public API client intended to run from
// outside a Kubernetes cluster.
func NewExternalClient(ctx context.Context, controlPlaneNamespace string, kubeAPI *k8s.KubernetesAPI) (Client, error) {
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
