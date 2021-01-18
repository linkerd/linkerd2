package pkg

import (
	"context"
	"fmt"

	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetVizNamespace returns the namespace where the viz extension is installed
func GetVizNamespace(ctx context.Context, k8sAPI *k8s.KubernetesAPI) (string, error) {
	namespaces, err := k8sAPI.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: k8s.LinkerdExtensionLabel})
	if err != nil {
		return "", err
	}
	if len(namespaces.Items) == 0 {
		return "", fmt.Errorf("could not find the linkerd-viz extension. It can be installed by running `linkerd viz install | kubectl apply -f -`")
	}

	return namespaces.Items[0].Name, nil
}
