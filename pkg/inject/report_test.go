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
		injectable          bool
		reasons             []string
	}{
		{
			podSpec: &corev1.PodSpec{
				HostNetwork: false,
				Containers: []corev1.Container{
					{
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: k8s.MountPathServiceAccount,
							},
						},
					},
				},
			},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
			},
			injectable: true,
		},
		{
			podSpec: &corev1.PodSpec{
				HostNetwork: true,
				Containers: []corev1.Container{
					{
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: k8s.MountPathServiceAccount,
							},
						},
					},
				},
			},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
			},
			injectable: false,
			reasons:    []string{hostNetworkEnabled},
		},
		{
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  k8s.ProxyContainerName,
						Image: "cr.l5d.io/linkerd/proxy:",
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: k8s.MountPathServiceAccount,
							},
						},
					},
				},
			},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
			},
			injectable: false,
			reasons:    []string{sidecarExists},
		},
		{
			podSpec: &corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:  k8s.InitContainerName,
						Image: "cr.l5d.io/linkerd/proxy-init:",
					},
				},
				Containers: []corev1.Container{
					{
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: k8s.MountPathServiceAccount,
							},
						},
					},
				},
			},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
			},
			injectable: false,
			reasons:    []string{sidecarExists},
		},
		{
			unsupportedResource: true,
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: k8s.MountPathServiceAccount,
							},
						},
					},
				},
			},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
			},
			injectable: false,
			reasons:    []string{unsupportedResource},
		},
		{
			unsupportedResource: true,
			podSpec: &corev1.PodSpec{
				HostNetwork: true,
				Containers: []corev1.Container{
					{
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: k8s.MountPathServiceAccount,
							},
						},
					},
				},
			},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
			},

			injectable: false,
			reasons:    []string{hostNetworkEnabled, unsupportedResource},
		},
		{
			nsAnnotations: map[string]string{
				k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
			},
			podSpec: &corev1.PodSpec{
				HostNetwork: true,
				Containers: []corev1.Container{
					{
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: k8s.MountPathServiceAccount,
							},
						},
					},
				},
			},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
				},
			},

			injectable: false,
			reasons:    []string{hostNetworkEnabled, injectDisableAnnotationPresent},
		},
		{
			nsAnnotations: map[string]string{
				k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
			},
			unsupportedResource: true,
			podSpec: &corev1.PodSpec{
				HostNetwork: true,
				Containers: []corev1.Container{
					{
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: k8s.MountPathServiceAccount,
							},
						},
					},
				},
			},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
				},
			},

			injectable: false,
			reasons:    []string{hostNetworkEnabled, unsupportedResource, injectDisableAnnotationPresent},
		},
		{
			unsupportedResource: true,
			podSpec: &corev1.PodSpec{
				HostNetwork: true,
				Containers: []corev1.Container{
					{
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: k8s.MountPathServiceAccount,
							},
						},
					},
				},
			},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{},
			},

			injectable: false,
			reasons:    []string{hostNetworkEnabled, unsupportedResource, injectEnableAnnotationAbsent},
		},
		{
			podSpec: &corev1.PodSpec{HostNetwork: true,
				Containers: []corev1.Container{
					{
						Name:  k8s.ProxyContainerName,
						Image: "cr.l5d.io/linkerd/proxy:",
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: k8s.MountPathServiceAccount,
							},
						},
					},
				}},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{},
			},

			injectable: false,
			reasons:    []string{hostNetworkEnabled, sidecarExists, injectEnableAnnotationAbsent},
		},
		{
			podSpec: &corev1.PodSpec{},
			podMeta: &metav1.ObjectMeta{
				Annotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
			},
			injectable: false,
			reasons:    []string{disabledAutomountServiceAccountToken},
		},
	}

	for i, testCase := range testCases {
		testCase := testCase
		t.Run(fmt.Sprintf("test case #%d", i), func(t *testing.T) {
			resourceConfig := &ResourceConfig{}
			resourceConfig.WithNsAnnotations(testCase.nsAnnotations)
			resourceConfig.pod.spec = testCase.podSpec
			resourceConfig.origin = OriginWebhook
			resourceConfig.pod.meta = testCase.podMeta

			report := newReport(resourceConfig)
			report.UnsupportedResource = testCase.unsupportedResource

			actual, reasons := report.Injectable()
			if testCase.injectable != actual {
				t.Errorf("Expected %t. Actual %t", testCase.injectable, actual)
			}

			if len(reasons) != len(testCase.reasons) {
				t.Errorf("Expected %d number of reasons. Actual %d", len(testCase.reasons), len(reasons))
			}

			for i := range reasons {
				if testCase.reasons[i] != reasons[i] {
					t.Errorf("Expected reason '%s'. Actual reason '%s'", testCase.reasons[i], reasons[i])
				}
			}

		})
	}
}

func TestDisableByAnnotation(t *testing.T) {
	t.Run("webhook origin", func(t *testing.T) {
		var testCases = []struct {
			podMeta       *metav1.ObjectMeta
			nsAnnotations map[string]string
			expected      bool
		}{
			{
				podMeta: &metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
					},
				},
				expected: false,
			},
			{
				podMeta: &metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
					},
				},
				nsAnnotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
				expected: false,
			},
			{
				podMeta: &metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
					},
				},
				nsAnnotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
				},
				expected: false,
			},
			{
				podMeta: &metav1.ObjectMeta{},
				nsAnnotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
				expected: false,
			},
			{
				podMeta: &metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
					},
				},
				nsAnnotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
				},
				expected: true,
			},
			{
				podMeta: &metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
					},
				},
				nsAnnotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
				},
				expected: true,
			},
			{
				podMeta: &metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
					},
				},
				nsAnnotations: map[string]string{},
				expected:      true,
			},
			{
				podMeta: &metav1.ObjectMeta{},
				nsAnnotations: map[string]string{
					k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
				},
				expected: true,
			},
			{
				podMeta:       &metav1.ObjectMeta{},
				nsAnnotations: map[string]string{},
				expected:      true,
			},
		}

		for i, testCase := range testCases {
			testCase := testCase
			t.Run(fmt.Sprintf("test case #%d", i), func(t *testing.T) {
				resourceConfig := &ResourceConfig{origin: OriginWebhook}
				resourceConfig.WithNsAnnotations(testCase.nsAnnotations)
				resourceConfig.pod.meta = testCase.podMeta
				resourceConfig.pod.spec = &corev1.PodSpec{} //initialize empty spec to prevent test from failing

				report := newReport(resourceConfig)
				if actual, _, _ := report.disabledByAnnotation(resourceConfig); testCase.expected != actual {
					t.Errorf("Expected %t. Actual %t", testCase.expected, actual)
				}
			})
		}
	})

	t.Run("CLI origin", func(t *testing.T) {
		var testCases = []struct {
			podMeta  *metav1.ObjectMeta
			expected bool
		}{
			{
				podMeta:  &metav1.ObjectMeta{},
				expected: false,
			},
			{
				podMeta: &metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
					},
				},
				expected: false,
			},
			{
				podMeta: &metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.ProxyInjectAnnotation: k8s.ProxyInjectDisabled,
					},
				},
				expected: true,
			},
		}

		for i, testCase := range testCases {
			testCase := testCase
			t.Run(fmt.Sprintf("test case #%d", i), func(t *testing.T) {
				resourceConfig := &ResourceConfig{origin: OriginCLI}
				resourceConfig.pod.meta = testCase.podMeta
				resourceConfig.pod.spec = &corev1.PodSpec{} //initialize empty spec to prevent test from failing

				report := newReport(resourceConfig)
				if actual, _, _ := report.disabledByAnnotation(resourceConfig); testCase.expected != actual {
					t.Errorf("Expected %t. Actual %t", testCase.expected, actual)
				}
			})
		}
	})
}
