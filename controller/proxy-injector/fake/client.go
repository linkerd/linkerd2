package fake

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// Client is a fake clientset that implements the kubernetes.Interface.
type Client struct {
	kubernetes.Interface
}

// NewClient returns a fake Kubernetes clientset.
func NewClient(kubeconfig string) (kubernetes.Interface, error) {
	client := fake.NewSimpleClientset()
	return &Client{client}, nil
}
