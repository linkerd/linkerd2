package k8s

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func NewClientSet(kubeConfig string) (*kubernetes.Clientset, error) {
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

	return kubernetes.NewForConfig(config)
}
