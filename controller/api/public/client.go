package public

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"go.opencensus.io/plugin/ochttp"
)

const (
	apiVersion    = "v1"
	apiPrefix     = "api/" + apiVersion + "/" // Must be relative (without a leading slash).
	apiPort       = 8085
	apiDeployment = "linkerd-controller"
)

// Client wraps one gRPC client interface for destination
type Client interface {
}

type grpcOverHTTPClient struct {
	serverURL             *url.URL
	httpClient            *http.Client
	controlPlaneNamespace string
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
