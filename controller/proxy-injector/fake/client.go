package fake

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// NewClient returns a fake Kubernetes clientset.
func NewClient(kubeconfig string, objs ...runtime.Object) kubernetes.Interface {
	return fake.NewSimpleClientset(objs...)
}
