package inject

import (
	"fmt"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInjectable(t *testing.T) {
	var testCases = []struct {
		podSpec             *corev1.PodSpec
		podMeta             *metav1.ObjectMeta
		nsAnnotations       map[string]string
		unsupportedResource bool
		expected            bool
	}{
		{
			podSpec:  &corev1.PodSpec{HostNetwork: false},
			podMeta:  &metav1.ObjectMeta{},
			expected: true,
		},
		{
			podSpec:  &corev1.PodSpec{HostNetwork: true},
			podMeta:  &metav1.ObjectMeta{},
			expected: false,
		},
		{
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  k8s.ProxyContainerName,
						Image: "gcr.io/linkerd-io/proxy:",
					},
				},
			},
			podMeta:  &metav1.ObjectMeta{},
			expected: false,
		},
		{
			podSpec: &corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:  k8s.InitContainerName,
						Image: "gcr.io/linkerd-io/proxy-init:",
					},
				},
			},
			podMeta:  &metav1.ObjectMeta{},
			expected: false,
		},
		{
			unsupportedResource: true,
			podSpec:             &corev1.PodSpec{},
			podMeta:             &metav1.ObjectMeta{},
			expected:            false,
		},
		{
			podSpec: &corev1.PodSpec{},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
			},
			expected: true,
		},
		{
			podSpec: &corev1.PodSpec{},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
				},
			},
			expected: false,
		},
		{
			podSpec: &corev1.PodSpec{},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
			},
			nsAnnotations: map[string]string{
				k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
			},
			expected: true,
		},
		{
			podSpec: &corev1.PodSpec{},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
				},
			},
			nsAnnotations: map[string]string{
				k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
			},
			expected: false,
		},
		{
			podSpec: &corev1.PodSpec{},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
				},
			},
			nsAnnotations: map[string]string{
				k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
			},
			expected: false,
		},
		{
			podSpec: &corev1.PodSpec{},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
			},
			nsAnnotations: map[string]string{
				k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
			},
			expected: true,
		},
	}

	for i, testCase := range testCases {
		testCase := testCase
		t.Run(fmt.Sprintf("test case #%d", i), func(t *testing.T) {
			resourceConfig := &ResourceConfig{}
			resourceConfig.WithNsAnnotations(testCase.nsAnnotations)
			resourceConfig.pod.spec = testCase.podSpec
			resourceConfig.pod.meta = testCase.podMeta

			report := newReport(resourceConfig)
			report.update(resourceConfig)
			report.UnsupportedResource = testCase.unsupportedResource

			if actual := report.Injectable(); testCase.expected != actual {
				t.Errorf("Expected %t. Actual %t", testCase.expected, actual)
			}
		})
	}
}
