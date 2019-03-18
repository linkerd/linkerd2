package inject

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestProxyPatch(t *testing.T) {
	var (
		resourceKind        = "Pod"
		controllerNamespace = "linkerd"
		proxyUID            = int64(8888)
	)

	t.Run("Non TLS", func(t *testing.T) {
		var (
			identity = k8s.TLSIdentity{
				Name:                "emojivoto",
				Kind:                resourceKind,
				Namespace:           "emojivoto",
				ControllerNamespace: controllerNamespace,
			}

			globalConfig = &config.Global{
				LinkerdNamespace: controllerNamespace,
				Version:          "abcde",
			}

			container = &corev1.Container{
				Name:                     k8s.ProxyContainerName,
				Image:                    "gcr.io/linkerd-io/proxy:abcde",
				ImagePullPolicy:          "IfNotPresent",
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: &proxyUID,
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"cpu":    k8sResource.MustParse("0.2"),
						"memory": k8sResource.MustParse("64"),
					},
					Limits: corev1.ResourceList{
						"cpu":    k8sResource.MustParse("1"),
						"memory": k8sResource.MustParse("128"),
					},
				},
				Ports: []corev1.ContainerPort{
					{Name: k8s.ProxyPortName, ContainerPort: 6000},
					{Name: k8s.ProxyMetricsPortName, ContainerPort: 6001},
				},
				Env: []corev1.EnvVar{
					{Name: envVarProxyLog, Value: "info,linkerd2_proxy=debug"},
					{Name: envVarProxyControlURL, Value: fmt.Sprintf("tcp://linkerd-destination.%s.svc.cluster.local:8086", controllerNamespace)},
					{Name: envVarProxyControlListener, Value: "tcp://0.0.0.0:9000"},
					{Name: envVarProxyMetricsListener, Value: "tcp://0.0.0.0:6001"},
					{Name: envVarProxyOutboundListener, Value: "tcp://127.0.0.1:6002"},
					{Name: envVarProxyInboundListener, Value: "tcp://0.0.0.0:6000"},
					{Name: envVarProxyDestinationProfileSuffixes, Value: "."},
					{Name: envVarProxyPodNamespace, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
					}},
					{Name: envVarProxyInboundAcceptKeepAlive, Value: "10000ms"},
					{Name: envVarProxyOutboundConnectKeepAlive, Value: "10000ms"},
					{Name: envVarProxyID, Value: identity.ToDNSName()},
				},
				LivenessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/metrics",
							Port: intstr.IntOrString{
								IntVal: int32(6001),
							},
						},
					},
					InitialDelaySeconds: 10,
				},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/metrics",
							Port: intstr.IntOrString{
								IntVal: int32(6001),
							},
						},
					},
					InitialDelaySeconds: 10,
				},
			}
		)
		resourceConfig := NewResourceConfig(globalConfig, &config.Proxy{}).WithKind(resourceKind)

		t.Run("create new proxy", func(t *testing.T) {
			expected := &Patch{
				patchPathContainerRoot:     "/spec/containers",
				patchPathContainer:         "/spec/containers/-",
				patchPathInitContainerRoot: "/spec/initContainers",
				patchPathInitContainer:     "/spec/initContainers/-",
				patchPathVolumeRoot:        "/spec/volumes",
				patchPathVolume:            "/spec/volumes/-",
				patchPathPodLabels:         patchPathRootLabels,
				patchPathPodAnnotations:    "/metadata/annotations",
				patchOps: []*patchOp{
					{Op: "add", Path: "/spec/containers/-", Value: container},
				},
			}

			// the empty containers list emulates an unmeshed pod
			resourceConfig.pod.spec = &corev1.PodSpec{}
			if actual := newProxyPatch(container, identity, resourceConfig); !reflect.DeepEqual(expected, actual) {
				t.Errorf("Expected: %+v\nActual: %+v", expected, actual)
			}
		})

		t.Run("override existing proxy", func(t *testing.T) {
			expected := &Patch{
				patchPathContainerRoot:     "/spec/containers",
				patchPathContainer:         "/spec/containers/-",
				patchPathInitContainerRoot: "/spec/initContainers",
				patchPathInitContainer:     "/spec/initContainers/-",
				patchPathVolumeRoot:        "/spec/volumes",
				patchPathVolume:            "/spec/volumes/-",
				patchPathPodLabels:         patchPathRootLabels,
				patchPathPodAnnotations:    "/metadata/annotations",
				patchOps: []*patchOp{
					{Op: "replace", Path: "/spec/containers/0", Value: container},
				},
			}

			// the non-empty containers list emulates a meshed pod
			resourceConfig.pod.spec = &corev1.PodSpec{
				Containers: []corev1.Container{*container},
			}
			if actual := newOverrideProxyPatch(container, resourceConfig); !reflect.DeepEqual(expected, actual) {
				fmt.Printf("%v\n", actual.patchOps[0])
				t.Errorf("Expected: %+v\nActual: %+v", expected, actual)
			}
		})
	})
}

func TestNewProxyInitPatch(t *testing.T) {
	var (
		resourceKind   = "Pod"
		globalConfig   = &config.Global{Version: "abcde"}
		resourceConfig = NewResourceConfig(globalConfig, &config.Proxy{}).WithKind(resourceKind)
		nonRoot        = false
		runAsUser      = int64(0)
		container      = &corev1.Container{
			Name:                     k8s.InitContainerName,
			Image:                    "gcr.io/linkerd-io/proxy-init:abcde",
			ImagePullPolicy:          "IfNotPresent",
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			Args: []string{
				"--incoming-proxy-port", "4143",
				"--outgoing-proxy-port", "4140",
				"--proxy-uid", "2102",
			},
			SecurityContext: &corev1.SecurityContext{
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{corev1.Capability("NET_ADMIN")},
				},
				Privileged:   &nonRoot,
				RunAsNonRoot: &nonRoot,
				RunAsUser:    &runAsUser,
			},
		}
	)

	t.Run("create new proxy-init", func(t *testing.T) {
		expected := &Patch{
			patchPathContainerRoot:     "/spec/containers",
			patchPathContainer:         "/spec/containers/-",
			patchPathInitContainerRoot: "/spec/initContainers",
			patchPathInitContainer:     "/spec/initContainers/-",
			patchPathVolumeRoot:        "/spec/volumes",
			patchPathVolume:            "/spec/volumes/-",
			patchPathPodLabels:         patchPathRootLabels,
			patchPathPodAnnotations:    "/metadata/annotations",
			patchOps: []*patchOp{
				{Op: "add", Path: "/spec/initContainers", Value: []*corev1.Container{}},
				{Op: "add", Path: "/spec/initContainers/-", Value: container},
			},
		}

		// the empty init-containers list emulates an unmeshed pod
		resourceConfig.pod.spec = &corev1.PodSpec{}
		if actual := newProxyInitPatch(container, resourceConfig); !reflect.DeepEqual(expected, actual) {
			t.Errorf("Expected: %+v\nActual: %+v", expected, actual)
		}
	})

	t.Run("overrides existing proxy-init", func(t *testing.T) {
		expected := &Patch{
			patchPathContainerRoot:     "/spec/containers",
			patchPathContainer:         "/spec/containers/-",
			patchPathInitContainerRoot: "/spec/initContainers",
			patchPathInitContainer:     "/spec/initContainers/-",
			patchPathVolumeRoot:        "/spec/volumes",
			patchPathVolume:            "/spec/volumes/-",
			patchPathPodLabels:         patchPathRootLabels,
			patchPathPodAnnotations:    "/metadata/annotations",
			patchOps: []*patchOp{
				{Op: "replace", Path: "/spec/initContainers/0", Value: container},
			},
		}

		// the non-empty init-containers list emulates a meshed pod
		resourceConfig.pod.spec = &corev1.PodSpec{
			InitContainers: []corev1.Container{*container},
		}
		if actual := newOverrideProxyInitPatch(container, resourceConfig); !reflect.DeepEqual(expected, actual) {
			t.Errorf("Expected: %+v\nActual: %+v", expected, actual)
		}
	})
}

func TestNewObjectMetaPatch(t *testing.T) {
	var (
		resourceKind   = "Pod"
		globalConfig   = &config.Global{Version: "abcde"}
		resourceConfig = NewResourceConfig(globalConfig, &config.Proxy{}).WithKind(resourceKind)
	)
	resourceConfig.pod.labels = map[string]string{"app": "nginx"}
	resourceConfig.pod.meta = objMeta{&metav1.ObjectMeta{}}

	t.Run("Non-TLS", func(t *testing.T) {
		expected := &Patch{
			patchPathContainerRoot:     "/spec/containers",
			patchPathContainer:         "/spec/containers/-",
			patchPathInitContainerRoot: "/spec/initContainers",
			patchPathInitContainer:     "/spec/initContainers/-",
			patchPathVolumeRoot:        "/spec/volumes",
			patchPathVolume:            "/spec/volumes/-",
			patchPathPodLabels:         patchPathRootLabels,
			patchPathPodAnnotations:    "/metadata/annotations",
			patchOps: []*patchOp{
				{Op: "add", Path: "/metadata/annotations", Value: map[string]string{}},
				{Op: "add", Path: "/metadata/annotations/linkerd.io~1proxy-version", Value: "abcde"},
				{Op: "add", Path: "/metadata/annotations/linkerd.io~1identity-mode", Value: "disabled"},
				{Op: "add", Path: "/metadata/labels/app", Value: "nginx"},
			},
		}
		if actual := newObjectMetaPatch(resourceConfig); !reflect.DeepEqual(expected, actual) {
			t.Errorf("Expected: %+v\nActual: %+v", expected, actual)
		}
	})
}
