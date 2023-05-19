package k8s

import (
	l5dcrdclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"k8s.io/client-go/rest"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func wrapTransport(config *rest.Config, telemetryName string) *rest.Config {
	wt := config.WrapTransport
	config.WrapTransport = prometheus.ClientWithTelemetry(telemetryName, wt)
	return config
}

// NewL5DCRDClient returns a Linkerd controller client for the given
// configuration.
func NewL5DCRDClient(kubeConfig *rest.Config) (*l5dcrdclient.Clientset, error) {
	return l5dcrdclient.NewForConfig(wrapTransport(kubeConfig, "l5dCrd"))
}
