package k8s

import (
	spclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func NewClientSet(kubeConfig string) (*kubernetes.Clientset, error) {
	config, err := parseConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

func NewSpClientSet(kubeConfig string) (*spclient.Clientset, error) {
	config, err := parseConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	spclientset, err := spclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return spclientset, nil
}

func parseConfig(kubeConfig string) (*rest.Config, error) {
	var config *rest.Config
	var err error

	if kubeConfig == "" {
		// configure client while running inside the k8s cluster
		// uses Service Acct token mounted in the Pod
		config, err = rest.InClusterConfig()
	} else {
		// configure access to the cluster from outside
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
	}
	if err != nil {
		return nil, err
	}
	return config, nil
}
