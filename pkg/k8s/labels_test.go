package k8s

import (
	"reflect"
	"testing"

	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetPodLabels(t *testing.T) {
	t.Run("Maps proxy labels to prometheus labels", func(t *testing.T) {
		pod := &coreV1.Pod{
			ObjectMeta: metaV1.ObjectMeta{
				Name: "test-pod",
				Labels: map[string]string{
					ControllerNSLabel:                      "linkerd-namespace",
					appsV1.DefaultDeploymentUniqueLabelKey: "test-pth",
				},
			},
		}

		ownerKind := "deployment"
		ownerName := "test-deployment"

		expectedLabels := map[string]string{
			"control_plane_ns":  "linkerd-namespace",
			"deployment":        "test-deployment",
			"pod":               "test-pod",
			"pod_template_hash": "test-pth",
		}

		podLabels := GetPodLabels(ownerKind, ownerName, pod)

		if !reflect.DeepEqual(podLabels, expectedLabels) {
			t.Fatalf("Expected pod labels [%v] but got [%v]", expectedLabels, podLabels)
		}
	})
}
