package healthcheck

import (
	"context"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

func FuzzFetchCurrentConfiguration(data []byte) int {
	clientset, err := k8s.NewFakeAPI(string(data))
	if err != nil {
		return 0
	}

	_, _, _ = FetchCurrentConfiguration(context.Background(), clientset, "linkerd")
	return 1
}
