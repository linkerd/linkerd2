package k8s

import (
	tsclient "github.com/deislabs/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	spclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"k8s.io/client-go/rest"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func wrapTransport (config *rest.Config, telemetryName string) *rest.Config {
	wt := config.WrapTransport
	config.WrapTransport = prometheus.ClientWithTelemetry(telemetryName, wt)
	return config
}

// NewSpClientSet returns a Kubernetes ServiceProfile client for the given
// configuration.
func NewSpClientSet(kubeConfig *rest.Config) (*spclient.Clientset, error) {
	return spclient.NewForConfig(wrapTransport(kubeConfig,"sp"))
}

// NewTsClientSet returns a Kubernetes TrafficSplit client for the given
// configuration.
func NewTsClientSet(kubeConfig *rest.Config) (*tsclient.Clientset, error) {
	return tsclient.NewForConfig(wrapTransport(kubeConfig,"ts"))
}
