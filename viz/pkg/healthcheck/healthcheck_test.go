package healthcheck

import (
	"testing"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHealthChecker(t *testing.T) {
	t.Run("Does not return an error if the pod is Evicted", func(t *testing.T) {
		pods := []v1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
				Status: v1.PodStatus{
					Phase: "Evicted",
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
		}
		err := healthcheck.CheckPodsRunning(pods, "")
		if err != nil {
			t.Fatalf("Expected success, got %s", err)
		}
	})
}
