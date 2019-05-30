package k8s

import (
	tsclient "github.com/deislabs/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	spclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// NewSpClientSet returns a Kubernetes ServiceProfile client for the given
// configuration.
func NewSpClientSet(kubeConfig string) (*spclient.Clientset, error) {
	config, err := k8s.GetConfig(kubeConfig, "")
	if err != nil {
		return nil, err
	}

	wt := config.WrapTransport
	config.WrapTransport = prometheus.ClientWithTelemetry("sp", wt)
	return spclient.NewForConfig(config)
}

// NewTsClientSet returns a Kubernetes TrafficSplit client for the given
// configuration.
func NewTsClientSet(kubeConfig string) (*tsclient.Clientset, error) {
	config, err := k8s.GetConfig(kubeConfig, "")
	if err != nil {
		return nil, err
	}

	wt := config.WrapTransport
	config.WrapTransport = prometheus.ClientWithTelemetry("ts", wt)
	return tsclient.NewForConfig(config)
}
