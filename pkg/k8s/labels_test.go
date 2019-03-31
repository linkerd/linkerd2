package k8s

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetPodLabels(t *testing.T) {
	t.Run("Maps proxy labels to prometheus labels", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "test-ns",
				Labels: map[string]string{
					ControllerNSLabel:                      "linkerd-namespace",
					appsv1.DefaultDeploymentUniqueLabelKey: "test-pth",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "test-sa",
			},
		}

		ownerKind := "deployment"
		ownerName := "test-deployment"

		expectedLabels := map[string]string{
			"control_plane_ns":  "linkerd-namespace",
			"deployment":        "test-deployment",
			"pod":               "test-pod",
			"pod_template_hash": "test-pth",
			"serviceaccount":    "test-sa",
		}

		podLabels := GetPodLabels(ownerKind, ownerName, pod)

		if !reflect.DeepEqual(podLabels, expectedLabels) {
			t.Fatalf("Expected pod labels [%v] but got [%v]", expectedLabels, podLabels)
		}
	})
}

func TestGetPodWeight(t *testing.T) {
	t.Run("Maps annotations to weights", func(t *testing.T) {
		type testCase struct {
			s string
			n uint32
		}

		for _, tc := range []testCase{
			{s: "1m", n: 10},
			{s: "10m", n: 100},
			{s: "200m", n: 2000},
			{s: "1", n: 10000},
			{s: "", n: 10000},
		} {
			pod := &corev1.Pod{}
			if tc.s != "" {
				pod.ObjectMeta.Annotations = map[string]string{
					ProxyPodWeightAnnotation: tc.s,
				}
			}

			if w := GetPodWeight(pod); w != tc.n {
				t.Errorf("Unexpected weight for '%s': %d; expected %d", tc.s, w, tc.n)
			}
		}
	})
}
