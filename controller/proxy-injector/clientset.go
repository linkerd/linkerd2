package injector

import (
	"github.com/linkerd/linkerd2/controller/k8s"
	"k8s.io/client-go/kubernetes"
)

// NewClientset returns a new instance of Clientset. It authenticates with the cluster using the service account token mounted inside the webhook pod.
func NewClientset(kubeconfig string) (kubernetes.Interface, error) {
	return k8s.NewClientSet(kubeconfig)
}
