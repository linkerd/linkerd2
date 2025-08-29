package k8s

import (
	"fmt"

	l5dcrdclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"k8s.io/client-go/rest"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func wrapTransport(config *rest.Config, telemetryName string) (*rest.Config, error) {
	wt := config.WrapTransport
	wrapped, err := prometheus.ClientWithTelemetry(telemetryName, wt)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap transport: %w", err)
	}
	config.WrapTransport = wrapped
	return config, nil
}

// NewL5DCRDClient returns a Linkerd controller client for the given
// configuration.
func NewL5DCRDClient(kubeConfig *rest.Config) (*l5dcrdclient.Clientset, error) {
	config, err := wrapTransport(kubeConfig, "l5dCrd")
	if err != nil {
		return nil, err
	}
	return l5dcrdclient.NewForConfig(config)
}
