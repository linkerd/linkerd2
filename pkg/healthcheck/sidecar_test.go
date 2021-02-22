package healthcheck

import (
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

func TestHasExistingSidecars(t *testing.T) {
	for _, tc := range []struct {
		podSpec  *corev1.PodSpec
		expected bool
	}{
		{
			podSpec:  &corev1.PodSpec{},
			expected: false,
		},
		{
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "foo",
						Image: "bar",
					},
				},
				InitContainers: []corev1.Container{
					{
						Name:  "foo",
						Image: "bar",
					},
				},
			},
			expected: false,
		},
		{
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: k8s.ProxyContainerName,
					},
				},
			},
			expected: true,
		},
		{
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
					},
				},
			},
			expected: true,
		},
		{
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: "cr.l5d.io/linkerd/proxy:1.0.0",
					},
				},
			},
			expected: true,
		},
		{
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: "gcr.io/istio-release/proxyv2:1.0.0",
					},
				},
			},
			expected: true,
		},
		{
			podSpec: &corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name: "linkerd-init",
					},
				},
			},
			expected: true,
		},
		{
			podSpec: &corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name: "istio-init",
					},
				},
			},
			expected: true,
		},
		{
			podSpec: &corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Image: "cr.l5d.io/linkerd/proxy-init:1.0.0",
					},
				},
			},
			expected: true,
		},
		{
			podSpec: &corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Image: "gcr.io/istio-release/proxy_init:1.0.0",
					},
				},
			},
			expected: true,
		},
	} {
		if !reflect.DeepEqual(HasExistingSidecars(tc.podSpec), tc.expected) {
			t.Errorf("expected: %v, got: %v", tc.expected, HasExistingSidecars(tc.podSpec))
		}
	}
}
