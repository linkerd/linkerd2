package destination

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

// NewClient creates a client for the control plane Destination API that
// implements the Destination service.
func NewClient(addr string) (pb.DestinationClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return pb.NewDestinationClient(conn), conn, nil
}

type grpcOverHTTPClient struct {
	serverURL             *url.URL
	httpClient            *http.Client
	controlPlaneNamespace string
}

func (c *grpcOverHTTPClient) Get(ctx context.Context, req *pb.GetDestination, _ ...grpc.CallOption) (pb.Destination_GetClient, error) {
	url := api.EndpointNameToPublicAPIURL(c.serverURL, "Get")
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

	return &destinationClient{api.StreamClient{Ctx: ctx, Reader: bufio.NewReader(httpRsp.Body)}}, nil
}

func (c *grpcOverHTTPClient) GetProfile(ctx context.Context, _ *pb.GetDestination, _ ...grpc.CallOption) (pb.Destination_GetProfileClient, error) {
	// Not implemented through this client. The proxies use the gRPC server directly instead.
	return nil, errors.New("Not implemented")
}

func newClient(apiURL *url.URL, httpClientToUse *http.Client, controlPlaneNamespace string) (pb.DestinationClient, error) {
	if !apiURL.IsAbs() {
		return nil, fmt.Errorf("server URL must be absolute, was [%s]", apiURL.String())
	}

	serverURL := apiURL.ResolveReference(&url.URL{Path: api.ApiPrefix})

	log.Debugf("Expecting Destination API to be served over [%s]", serverURL)

	return &grpcOverHTTPClient{
		serverURL:             serverURL,
		httpClient:            httpClientToUse,
		controlPlaneNamespace: controlPlaneNamespace,
	}, nil
}

type destinationClient struct {
	api.StreamClient
}

func (c destinationClient) Recv() (*pb.Update, error) {
	var msg pb.Update
	err := api.FromByteStreamToProtocolBuffers(c.Reader, &msg)
	return &msg, err
}

// NewExternalDestinationAPIClient creates a new Destination API client intended to run from
// outside a Kubernetes cluster.
func NewExternalDestinationAPIClient(controlPlaneNamespace string, kubeAPI *k8s.KubernetesAPI) (pb.DestinationClient, error) {
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
