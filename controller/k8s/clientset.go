package k8s

import (
	tsclient "github.com/deislabs/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	spclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"k8s.io/client-go/rest"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func newConfig(kubeConfig string, telemetryName string) (*rest.Config, error) {
	config, err := k8s.GetConfig(kubeConfig, "")
	if err != nil {
		return nil, err
	}

	wt := config.WrapTransport
	config.WrapTransport = prometheus.ClientWithTelemetry(telemetryName, wt)
	return config, nil
}

// NewSpClientSet returns a Kubernetes ServiceProfile client for the given
// configuration.
func NewSpClientSet(kubeConfig string) (*spclient.Clientset, error) {
	config, err := newConfig(kubeConfig, "sp")
	if err != nil {
		return nil, err
	}

	return spclient.NewForConfig(config)
}

// NewTsClientSet returns a Kubernetes TrafficSplit client for the given
// configuration.
func NewTsClientSet(kubeConfig string) (*tsclient.Clientset, error) {
	config, err := newConfig(kubeConfig, "ts")
	if err != nil {
		return nil, err
	}

	return tsclient.NewForConfig(config)
}
