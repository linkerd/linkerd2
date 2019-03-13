package inject

import (
	"reflect"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

type expectedProxyConfigs struct {
	image                      string
	imagePullPolicy            corev1.PullPolicy
	controlPort                int32
	inboundPort                int32
	metricsPort                int32
	outboundPort               int32
	logLevel                   string
	resourceRequirements       corev1.ResourceRequirements
	controlURL                 string
	controlListener            string
	inboundListener            string
	metricsListener            string
	outboundListener           string
	proxyUID                   int64
	probe                      *corev1.Probe
	destinationProfileSuffixes string
	initImage                  string
	initImagePullPolicy        corev1.PullPolicy
	initArgs                   []string
	inboundSkipPorts           string
	outboundSkipPorts          string
}

func TestConfigAccessors(t *testing.T) {
	// this test uses an annotated deployment and a proxyConfig object to verify
	// all the proxy config accessors. The first test run ensures that the
	// accessors picks up the pod-level config annotations. The second test run
	// ensures that the defaults in the config map is used.

	proxyConfig := &config.Proxy{
		ProxyImage:          &config.Image{ImageName: "gcr.io/linkerd-io/proxy", PullPolicy: "IfNotPresent"},
		ProxyInitImage:      &config.Image{ImageName: "gcr.io/linkerd-io/proxy-init", PullPolicy: "IfNotPresent"},
		ControlPort:         &config.Port{Port: 9000},
		InboundPort:         &config.Port{Port: 6000},
		MetricsPort:         &config.Port{Port: 6001},
		OutboundPort:        &config.Port{Port: 6002},
		IgnoreInboundPorts:  []*config.Port{{Port: 53}},
		IgnoreOutboundPorts: []*config.Port{{Port: 9079}},
		Resource: &config.ResourceRequirements{
			RequestCpu:    "0.2",
			RequestMemory: "64",
			LimitCpu:      "1",
			LimitMemory:   "128",
		},
		ProxyUid:                8888,
		LogLevel:                &config.LogLevel{Level: "info,linkerd2_proxy=debug"},
		DisableExternalProfiles: false,
	}
	globalConfig := &config.Global{LinkerdNamespace: "linkerd"}
	resourceConfig := NewResourceConfig(globalConfig, proxyConfig).WithKind("Deployment")

	var testCases = []struct {
		id       string
		spec     appsv1.DeploymentSpec
		expected expectedProxyConfigs
	}{
		{id: "use overrides",
			spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					metav1.ObjectMeta{
						Annotations: map[string]string{
							k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy",
							k8s.ProxyImagePullPolicyAnnotation:         "Always",
							k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init",
							k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
							k8s.ProxyControlPortAnnotation:             "4000",
							k8s.ProxyInboundPortAnnotation:             "5000",
							k8s.ProxyMetricsPortAnnotation:             "5001",
							k8s.ProxyOutboundPortAnnotation:            "5002",
							k8s.ProxyIgnoreInboundPortsAnnotation:      "4222,6222",
							k8s.ProxyIgnoreOutboundPortsAnnotation:     "8079,8080",
							k8s.ProxyRequestCPUAnnotation:              "0.15",
							k8s.ProxyRequestMemoryAnnotation:           "120",
							k8s.ProxyLimitCPUAnnotation:                "1.5",
							k8s.ProxyLimitMemoryAnnotation:             "256",
							k8s.ProxyUIDAnnotation:                     "8500",
							k8s.ProxyLogLevelAnnotation:                "debug,linkerd2_proxy=debug",
							k8s.ProxyDisableExternalProfilesAnnotation: "true"},
					},
					corev1.PodSpec{},
				},
			},
			expected: expectedProxyConfigs{
				image:           "gcr.io/linkerd-io/proxy",
				imagePullPolicy: corev1.PullPolicy("Always"),
				controlPort:     int32(4000),
				inboundPort:     int32(5000),
				metricsPort:     int32(5001),
				outboundPort:    int32(5002),
				logLevel:        "debug,linkerd2_proxy=debug",
				resourceRequirements: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"cpu":    k8sResource.MustParse("0.15"),
						"memory": k8sResource.MustParse("120"),
					},
					Limits: corev1.ResourceList{
						"cpu":    k8sResource.MustParse("1.5"),
						"memory": k8sResource.MustParse("256"),
					},
				},
				controlURL:       "tcp://linkerd-destination.linkerd.svc.cluster.local:8086",
				controlListener:  "tcp://0.0.0.0:4000",
				inboundListener:  "tcp://0.0.0.0:5000",
				metricsListener:  "tcp://0.0.0.0:5001",
				outboundListener: "tcp://127.0.0.1:5002",
				proxyUID:         int64(8500),
				probe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/metrics",
							Port: intstr.IntOrString{
								IntVal: int32(5001),
							},
						},
					},
					InitialDelaySeconds: 10,
				},
				destinationProfileSuffixes: "svc.cluster.local.",
				initImage:                  "gcr.io/linkerd-io/proxy-init",
				initImagePullPolicy:        corev1.PullPolicy("Always"),
				initArgs: []string{
					"--incoming-proxy-port", "5000",
					"--outgoing-proxy-port", "5002",
					"--proxy-uid", "8500",
					"--inbound-ports-to-ignore", "4222,6222,4000,5001",
					"--outbound-ports-to-ignore", "8079,8080",
				},
				inboundSkipPorts:  "4222,6222",
				outboundSkipPorts: "8079,8080",
			},
		},
		{id: "use defaults",
			spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					metav1.ObjectMeta{},
					corev1.PodSpec{},
				},
			},
			expected: expectedProxyConfigs{
				image:           "gcr.io/linkerd-io/proxy",
				imagePullPolicy: corev1.PullPolicy("IfNotPresent"),
				controlPort:     int32(9000),
				inboundPort:     int32(6000),
				metricsPort:     int32(6001),
				outboundPort:    int32(6002),
				logLevel:        "info,linkerd2_proxy=debug",
				resourceRequirements: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"cpu":    k8sResource.MustParse("0.2"),
						"memory": k8sResource.MustParse("64"),
					},
					Limits: corev1.ResourceList{
						"cpu":    k8sResource.MustParse("1"),
						"memory": k8sResource.MustParse("128"),
					},
				},
				controlURL:       "tcp://linkerd-destination.linkerd.svc.cluster.local:8086",
				controlListener:  "tcp://0.0.0.0:9000",
				inboundListener:  "tcp://0.0.0.0:6000",
				metricsListener:  "tcp://0.0.0.0:6001",
				outboundListener: "tcp://127.0.0.1:6002",
				proxyUID:         int64(8888),
				probe: &corev1.Probe{
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
				destinationProfileSuffixes: ".",
				initImage:                  "gcr.io/linkerd-io/proxy-init",
				initImagePullPolicy:        corev1.PullPolicy("IfNotPresent"),
				initArgs: []string{
					"--incoming-proxy-port", "6000",
					"--outgoing-proxy-port", "6002",
					"--proxy-uid", "8888",
					"--inbound-ports-to-ignore", "53,9000,6001",
					"--outbound-ports-to-ignore", "9079",
				},
				inboundSkipPorts:  "53",
				outboundSkipPorts: "9079",
			},
		},
	}

	for _, tc := range testCases {
		testCase := tc
		t.Run(testCase.id, func(t *testing.T) {
			data, err := yaml.Marshal(&appsv1.Deployment{Spec: testCase.spec})
			if err != nil {
				t.Fatal(err)
			}

			if err := resourceConfig.parse(data); err != nil {
				t.Fatal(err)
			}

			t.Run("proxyImage", func(t *testing.T) {
				expected := testCase.expected.image
				if actual := resourceConfig.proxyImage(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyImagePullPolicy", func(t *testing.T) {
				expected := testCase.expected.imagePullPolicy
				if actual := resourceConfig.proxyImagePullPolicy(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyControlPort", func(t *testing.T) {
				expected := testCase.expected.controlPort
				if actual := resourceConfig.proxyControlPort(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyInboundPort", func(t *testing.T) {
				expected := testCase.expected.inboundPort
				if actual := resourceConfig.proxyInboundPort(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyMetricsPort", func(t *testing.T) {
				expected := testCase.expected.metricsPort
				if actual := resourceConfig.proxyMetricsPort(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyOutboundPort", func(t *testing.T) {
				expected := testCase.expected.outboundPort
				if actual := resourceConfig.proxyOutboundPort(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyLogLevel", func(t *testing.T) {
				expected := testCase.expected.logLevel
				if actual := resourceConfig.proxyLogLevel(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyResourceRequirements", func(t *testing.T) {
				expected := testCase.expected.resourceRequirements
				if actual := resourceConfig.proxyResourceRequirements(); !reflect.DeepEqual(expected, actual) {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyControlURL", func(t *testing.T) {
				expected := testCase.expected.controlURL
				if actual := resourceConfig.proxyControlURL(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyControlListener", func(t *testing.T) {
				expected := testCase.expected.controlListener
				if actual := resourceConfig.proxyControlListener(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyInboundListener", func(t *testing.T) {
				expected := testCase.expected.inboundListener
				if actual := resourceConfig.proxyInboundListener(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyMetricsListener", func(t *testing.T) {
				expected := testCase.expected.metricsListener
				if actual := resourceConfig.proxyMetricsListener(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyOutboundListener", func(t *testing.T) {
				expected := testCase.expected.outboundListener
				if actual := resourceConfig.proxyOutboundListener(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyUID", func(t *testing.T) {
				expected := testCase.expected.proxyUID
				if actual := resourceConfig.proxyUID(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyProbe", func(t *testing.T) {
				expected := testCase.expected.probe
				if actual := resourceConfig.proxyProbe(); !reflect.DeepEqual(expected, actual) {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyDestinationProfileSuffixes", func(t *testing.T) {
				expected := testCase.expected.destinationProfileSuffixes
				if actual := resourceConfig.proxyDestinationProfileSuffixes(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyInitImage", func(t *testing.T) {
				expected := testCase.expected.initImage
				if actual := resourceConfig.proxyInitImage(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyInitImagePullPolicy", func(t *testing.T) {
				expected := testCase.expected.initImagePullPolicy
				if actual := resourceConfig.proxyInitImagePullPolicy(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyInitArgs", func(t *testing.T) {
				expected := testCase.expected.initArgs
				if actual := resourceConfig.proxyInitArgs(); !reflect.DeepEqual(expected, actual) {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyInboundSkipPorts", func(t *testing.T) {
				expected := testCase.expected.inboundSkipPorts
				if actual := resourceConfig.proxyInboundSkipPorts(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyOutboundSkipPorts", func(t *testing.T) {
				expected := testCase.expected.outboundSkipPorts
				if actual := resourceConfig.proxyOutboundSkipPorts(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})
		})
	}
}

func TestOverrideProxyConfigs(t *testing.T) {
	// this test creates an annotated deployment with a single pod, containing
	// two containers, and two init container. It ensures that all the pod-level
	// config annotations are used, while other existing fields are preserved.

	t.Run("with annotations but preserve other fields", func(t *testing.T) {
		containers := []corev1.Container{
			{
				Name: "nginx",
				Ports: []corev1.ContainerPort{
					{Name: "http", ContainerPort: 80},
				},
			},
			{
				Name: k8s.ProxyContainerName,
				Ports: []corev1.ContainerPort{
					{Name: k8s.ProxyPortName, ContainerPort: 4143},
					{Name: k8s.ProxyMetricsPortName, ContainerPort: 4191},
				},
				Env: []corev1.EnvVar{
					{Name: envVarProxyLog, Value: "info,linkerd2_proxy=warn"},
					{Name: envVarProxyControlListener, Value: "tcp://0.0.0.0:4190"},
					{Name: envVarProxyMetricsListener, Value: "tcp://0.0.0.0:4191"},
					{Name: envVarProxyOutboundListener, Value: "tcp://0.0.0.0:4140"},
					{Name: envVarProxyInboundListener, Value: "tcp://0.0.0.0:4143"},
					{Name: envVarProxyDestinationProfileSuffixes, Value: "."},
					{Name: envVarProxyID, Value: "8500"},
				},
			},
		}

		initContainers := []corev1.Container{
			{
				Name: "web-config",
				VolumeMounts: []corev1.VolumeMount{
					{Name: "www", MountPath: "/var/www/html"},
				},
			},
			{
				Name: k8s.InitContainerName,
				Args: strings.Split(`--incoming-proxy-port "4143" --outgoing-proxy-port "4140" --proxy-uid "2102" --inbound-ports-to-ignore 4190,4191 --outbound-ports-to-ignore "443"`, " "),
			},
		}

		deploymentSpec := appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy",
						k8s.ProxyImagePullPolicyAnnotation:         "Always",
						k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init",
						k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
						k8s.ProxyControlPortAnnotation:             "4000",
						k8s.ProxyIgnoreInboundPortsAnnotation:      "4222,6222",
						k8s.ProxyIgnoreOutboundPortsAnnotation:     "8079,8080",
						k8s.ProxyInboundPortAnnotation:             "5000",
						k8s.ProxyMetricsPortAnnotation:             "5001",
						k8s.ProxyOutboundPortAnnotation:            "5002",
						k8s.ProxyRequestCPUAnnotation:              "0.15",
						k8s.ProxyRequestMemoryAnnotation:           "120",
						k8s.ProxyLimitCPUAnnotation:                "1.5",
						k8s.ProxyLimitMemoryAnnotation:             "256",
						k8s.ProxyUIDAnnotation:                     "8500",
						k8s.ProxyLogLevelAnnotation:                "debug,linkerd2_proxy=debug",
						k8s.ProxyDisableExternalProfilesAnnotation: "true"},
				},
				corev1.PodSpec{
					InitContainers: initContainers,
					Containers:     containers,
				},
			},
		}

		expectedProxyUID := int64(8500)
		expectedSpec := struct {
			containers     []corev1.Container
			initContainers []corev1.Container
		}{
			containers: []corev1.Container{
				{
					Name: "nginx",
					Ports: []corev1.ContainerPort{
						{Name: "http", ContainerPort: 80},
					},
				},
				{
					Name:            k8s.ProxyContainerName,
					Image:           "gcr.io/linkerd-io/proxy:",
					ImagePullPolicy: "Always",
					Ports: []corev1.ContainerPort{
						{Name: k8s.ProxyPortName, ContainerPort: 5000},
						{Name: k8s.ProxyMetricsPortName, ContainerPort: 5001},
					},
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: &expectedProxyUID,
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("0.15"),
							corev1.ResourceMemory: resource.MustParse("120"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1.5"),
							corev1.ResourceMemory: resource.MustParse("256"),
						},
					},
					Env: []corev1.EnvVar{
						{Name: envVarProxyLog, Value: "debug,linkerd2_proxy=debug"},
						{Name: envVarProxyControlListener, Value: "tcp://0.0.0.0:4000"},
						{Name: envVarProxyMetricsListener, Value: "tcp://0.0.0.0:5001"},
						{Name: envVarProxyOutboundListener, Value: "tcp://127.0.0.1:5002"},
						{Name: envVarProxyInboundListener, Value: "tcp://0.0.0.0:5000"},
						{Name: envVarProxyDestinationProfileSuffixes, Value: "svc.cluster.local."},
						{Name: envVarProxyID, Value: "8500"},
					},
					LivenessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/metrics",
								Port: intstr.IntOrString{
									IntVal: int32(5001),
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
									IntVal: int32(5001),
								},
							},
						},
						InitialDelaySeconds: 10,
					},
				},
			},
			initContainers: []corev1.Container{
				{
					Name: "web-config",
					VolumeMounts: []corev1.VolumeMount{
						{Name: "www", MountPath: "/var/www/html"},
					},
				},
				{
					Name:            k8s.InitContainerName,
					Image:           "gcr.io/linkerd-io/proxy-init:",
					ImagePullPolicy: "Always",
					Args:            strings.Split("--incoming-proxy-port 5000 --outgoing-proxy-port 5002 --proxy-uid 8500 --inbound-ports-to-ignore 4222,6222,4000,5001 --outbound-ports-to-ignore 8079,8080", " "),
				},
			},
		}

		resourceConfig := NewResourceConfig(&config.Global{}, &config.Proxy{}).WithKind("Deployment")
		data, err := yaml.Marshal(&appsv1.Deployment{Spec: deploymentSpec})
		if err != nil {
			t.Fatal(err)
		}
		if err := resourceConfig.parse(data); err != nil {
			t.Fatal(err)
		}

		patch := NewPatchDeployment()
		resourceConfig.overrideProxyConfigs(patch)

		if len(expectedSpec.containers) != len(resourceConfig.podSpec.Containers) {
			t.Errorf("Containers count mismatch. Expected: %d. Actual: %d", len(expectedSpec.containers), len(resourceConfig.podSpec.Containers))
		}

		if len(expectedSpec.initContainers) != len(resourceConfig.podSpec.InitContainers) {
			t.Errorf("Init containers count mismatch. Expected: %d. Actual: %d", len(expectedSpec.initContainers), len(resourceConfig.podSpec.InitContainers))
		}

		// make sure all containers are still intact
		for _, actual := range resourceConfig.podSpec.Containers {
			for _, expected := range expectedSpec.containers {
				if expected.Name == actual.Name {
					if !reflect.DeepEqual(expected, actual) {
						t.Errorf("Containers mismatch\nExpected: %+v\nActual: %+v\n", expected, actual)
					}
				}
			}
		}

		// make sure all init containers are still intact
		for _, actual := range resourceConfig.podSpec.InitContainers {
			for _, expected := range expectedSpec.initContainers {
				if expected.Name == actual.Name {
					if !reflect.DeepEqual(expected, actual) {
						t.Errorf("Init containers mismatch\nExpected: %+v\nActual: %+v\n", expected, actual)
					}
				}
			}
		}
	})
}
