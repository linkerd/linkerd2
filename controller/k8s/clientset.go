package k8s

import (
	spclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"k8s.io/client-go/kubernetes"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// NewClientSet returns a Kubernetes client for the given configuration.
func NewClientSet(kubeConfig string) (*kubernetes.Clientset, error) {
	config, err := k8s.GetConfig(kubeConfig, "")
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

// NewSpClientSet returns a Kubernetes ServiceProfile client for the given
// configuration.
func NewSpClientSet(kubeConfig string) (*spclient.Clientset, error) {
	config, err := k8s.GetConfig(kubeConfig, "")
	if err != nil {
		return nil, err
	}

	return spclient.NewForConfig(config)
}
