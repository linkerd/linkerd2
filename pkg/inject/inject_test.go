package inject

import (
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/config"
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/testutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type expectedProxyConfigs struct {
	identityContext               *config.IdentityContext
	image                         string
	imagePullPolicy               string
	proxyVersion                  string
	controlPort                   int32
	inboundPort                   int32
	adminPort                     int32
	outboundPort                  int32
	proxyWaitBeforeExitSeconds    uint64
	logLevel                      string
	resourceRequirements          *l5dcharts.Resources
	proxyUID                      int64
	initImage                     string
	initImagePullPolicy           string
	initVersion                   string
	inboundSkipPorts              string
	outboundSkipPorts             string
	requireIdentityOnInboundPorts string
	destinationGetNetworks        string
	trace                         *l5dcharts.Trace
}

func TestConfigAccessors(t *testing.T) {
	// this test uses an annotated deployment and a proxyConfig object to verify
	// all the proxy config accessors. The first test suite ensures that the
	// accessors picks up the pod-level config annotations. The second test suite
	// ensures that the defaults in the config map is used.

	var (
		controlPlaneVersion  = "control-plane-version"
		proxyVersion         = "proxy-version"
		proxyVersionOverride = "proxy-version-override"
	)

	proxyConfig := &config.Proxy{
		ProxyImage:          &config.Image{ImageName: "gcr.io/linkerd-io/proxy", PullPolicy: "IfNotPresent"},
		ProxyInitImage:      &config.Image{ImageName: "gcr.io/linkerd-io/proxy-init", PullPolicy: "IfNotPresent"},
		ControlPort:         &config.Port{Port: 9000},
		InboundPort:         &config.Port{Port: 6000},
		AdminPort:           &config.Port{Port: 6001},
		OutboundPort:        &config.Port{Port: 6002},
		IgnoreInboundPorts:  []*config.PortRange{{PortRange: "53,58-59"}},
		IgnoreOutboundPorts: []*config.PortRange{{PortRange: "9079-9080"}},
		Resource: &config.ResourceRequirements{
			RequestCpu:    "0.2",
			RequestMemory: "64",
			LimitCpu:      "1",
			LimitMemory:   "128",
		},
		ProxyUid:                8888,
		LogLevel:                &config.LogLevel{Level: "info,linkerd2_proxy=debug"},
		DisableExternalProfiles: false,
		ProxyVersion:            proxyVersion,
		DestinationGetNetworks:  "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16",
	}

	globalConfig := &config.Global{
		LinkerdNamespace: "linkerd",
		Version:          controlPlaneVersion,
		IdentityContext:  &config.IdentityContext{},
		ClusterDomain:    "cluster.local",
	}

	configs := &config.All{Global: globalConfig, Proxy: proxyConfig}

	var testCases = []struct {
		id            string
		nsAnnotations map[string]string
		spec          appsv1.DeploymentSpec
		expected      expectedProxyConfigs
	}{
		{id: "use overrides",
			spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							k8s.ProxyDisableIdentityAnnotation:               "true",
							k8s.ProxyImageAnnotation:                         "gcr.io/linkerd-io/proxy",
							k8s.ProxyImagePullPolicyAnnotation:               "Always",
							k8s.ProxyInitImageAnnotation:                     "gcr.io/linkerd-io/proxy-init",
							k8s.ProxyControlPortAnnotation:                   "4000",
							k8s.ProxyInboundPortAnnotation:                   "5000",
							k8s.ProxyAdminPortAnnotation:                     "5001",
							k8s.ProxyOutboundPortAnnotation:                  "5002",
							k8s.ProxyIgnoreInboundPortsAnnotation:            "4222,6222",
							k8s.ProxyIgnoreOutboundPortsAnnotation:           "8079,8080",
							k8s.ProxyCPURequestAnnotation:                    "0.15",
							k8s.ProxyMemoryRequestAnnotation:                 "120",
							k8s.ProxyCPULimitAnnotation:                      "1.5",
							k8s.ProxyMemoryLimitAnnotation:                   "256",
							k8s.ProxyUIDAnnotation:                           "8500",
							k8s.ProxyLogLevelAnnotation:                      "debug,linkerd2_proxy=debug",
							k8s.ProxyEnableExternalProfilesAnnotation:        "false",
							k8s.ProxyVersionOverrideAnnotation:               proxyVersionOverride,
							k8s.ProxyTraceCollectorSvcAddrAnnotation:         "oc-collector.tracing:55678",
							k8s.ProxyTraceCollectorSvcAccountAnnotation:      "default",
							k8s.ProxyWaitBeforeExitSecondsAnnotation:         "123",
							k8s.ProxyRequireIdentityOnInboundPortsAnnotation: "8888,9999",
							k8s.ProxyDestinationGetNetworks:                  "10.0.0.0/8",
						},
					},
					Spec: corev1.PodSpec{},
				},
			},
			expected: expectedProxyConfigs{
				image:                      "gcr.io/linkerd-io/proxy",
				imagePullPolicy:            "Always",
				proxyVersion:               proxyVersionOverride,
				controlPort:                int32(4000),
				inboundPort:                int32(5000),
				adminPort:                  int32(5001),
				outboundPort:               int32(5002),
				proxyWaitBeforeExitSeconds: 123,
				logLevel:                   "debug,linkerd2_proxy=debug",
				resourceRequirements: &l5dcharts.Resources{
					CPU: l5dcharts.Constraints{
						Limit:   "1500m",
						Request: "150m",
					},
					Memory: l5dcharts.Constraints{
						Limit:   "256",
						Request: "120",
					},
				},
				proxyUID:            int64(8500),
				initImage:           "gcr.io/linkerd-io/proxy-init",
				initImagePullPolicy: "Always",
				initVersion:         version.ProxyInitVersion,
				inboundSkipPorts:    "4222,6222",
				outboundSkipPorts:   "8079,8080",
				trace: &l5dcharts.Trace{
					CollectorSvcAddr:    "oc-collector.tracing:55678",
					CollectorSvcAccount: "default.tracing",
				},
				requireIdentityOnInboundPorts: "8888,9999",
				destinationGetNetworks:        "10.0.0.0/8",
			},
		},
		{id: "use defaults",
			spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{},
					Spec:       corev1.PodSpec{},
				},
			},
			expected: expectedProxyConfigs{
				identityContext:            &config.IdentityContext{},
				image:                      "gcr.io/linkerd-io/proxy",
				imagePullPolicy:            "IfNotPresent",
				proxyVersion:               proxyVersion,
				controlPort:                int32(9000),
				inboundPort:                int32(6000),
				adminPort:                  int32(6001),
				outboundPort:               int32(6002),
				proxyWaitBeforeExitSeconds: 0,
				logLevel:                   "info,linkerd2_proxy=debug",
				resourceRequirements: &l5dcharts.Resources{
					CPU: l5dcharts.Constraints{
						Limit:   "1",
						Request: "200m",
					},
					Memory: l5dcharts.Constraints{
						Limit:   "128",
						Request: "64",
					},
				},
				proxyUID:               int64(8888),
				initImage:              "gcr.io/linkerd-io/proxy-init",
				initImagePullPolicy:    "IfNotPresent",
				initVersion:            version.ProxyInitVersion,
				inboundSkipPorts:       "53,58-59",
				outboundSkipPorts:      "9079-9080",
				destinationGetNetworks: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16",
			},
		},
		{id: "use namespace overrides",
			nsAnnotations: map[string]string{
				k8s.ProxyDisableIdentityAnnotation:          "true",
				k8s.ProxyImageAnnotation:                    "gcr.io/linkerd-io/proxy",
				k8s.ProxyImagePullPolicyAnnotation:          "Always",
				k8s.ProxyInitImageAnnotation:                "gcr.io/linkerd-io/proxy-init",
				k8s.ProxyControlPortAnnotation:              "4000",
				k8s.ProxyInboundPortAnnotation:              "5000",
				k8s.ProxyAdminPortAnnotation:                "5001",
				k8s.ProxyOutboundPortAnnotation:             "5002",
				k8s.ProxyIgnoreInboundPortsAnnotation:       "4222,6222",
				k8s.ProxyIgnoreOutboundPortsAnnotation:      "8079,8080",
				k8s.ProxyCPURequestAnnotation:               "0.15",
				k8s.ProxyMemoryRequestAnnotation:            "120",
				k8s.ProxyCPULimitAnnotation:                 "1.5",
				k8s.ProxyMemoryLimitAnnotation:              "256",
				k8s.ProxyUIDAnnotation:                      "8500",
				k8s.ProxyLogLevelAnnotation:                 "debug,linkerd2_proxy=debug",
				k8s.ProxyEnableExternalProfilesAnnotation:   "false",
				k8s.ProxyVersionOverrideAnnotation:          proxyVersionOverride,
				k8s.ProxyTraceCollectorSvcAddrAnnotation:    "oc-collector.tracing:55678",
				k8s.ProxyTraceCollectorSvcAccountAnnotation: "default",
				k8s.ProxyWaitBeforeExitSecondsAnnotation:    "123",
			},
			spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
			expected: expectedProxyConfigs{
				image:                      "gcr.io/linkerd-io/proxy",
				imagePullPolicy:            "Always",
				proxyVersion:               proxyVersionOverride,
				controlPort:                int32(4000),
				inboundPort:                int32(5000),
				adminPort:                  int32(5001),
				outboundPort:               int32(5002),
				proxyWaitBeforeExitSeconds: 123,
				logLevel:                   "debug,linkerd2_proxy=debug",
				resourceRequirements: &l5dcharts.Resources{
					CPU: l5dcharts.Constraints{
						Limit:   "1500m",
						Request: "150m",
					},
					Memory: l5dcharts.Constraints{
						Limit:   "256",
						Request: "120",
					},
				},
				proxyUID:            int64(8500),
				initImage:           "gcr.io/linkerd-io/proxy-init",
				initImagePullPolicy: "Always",
				initVersion:         version.ProxyInitVersion,
				inboundSkipPorts:    "4222,6222",
				outboundSkipPorts:   "8079,8080",
				trace: &l5dcharts.Trace{
					CollectorSvcAddr:    "oc-collector.tracing:55678",
					CollectorSvcAccount: "default.tracing",
				},
				destinationGetNetworks: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16",
			},
		},
		{id: "use not a uint value for ProxyWaitBeforeExitSecondsAnnotation annotation",
			nsAnnotations: map[string]string{
				k8s.ProxyWaitBeforeExitSecondsAnnotation: "-111",
			},
			spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{},
					Spec:       corev1.PodSpec{},
				},
			},
			expected: expectedProxyConfigs{
				identityContext:            &config.IdentityContext{},
				image:                      "gcr.io/linkerd-io/proxy",
				imagePullPolicy:            "IfNotPresent",
				proxyVersion:               proxyVersion,
				controlPort:                int32(9000),
				inboundPort:                int32(6000),
				adminPort:                  int32(6001),
				outboundPort:               int32(6002),
				proxyWaitBeforeExitSeconds: 0,
				logLevel:                   "info,linkerd2_proxy=debug",
				resourceRequirements: &l5dcharts.Resources{
					CPU: l5dcharts.Constraints{
						Limit:   "1",
						Request: "200m",
					},
					Memory: l5dcharts.Constraints{
						Limit:   "128",
						Request: "64",
					},
				},
				proxyUID:               int64(8888),
				initImage:              "gcr.io/linkerd-io/proxy-init",
				initImagePullPolicy:    "IfNotPresent",
				initVersion:            version.ProxyInitVersion,
				inboundSkipPorts:       "53,58-59",
				outboundSkipPorts:      "9079-9080",
				destinationGetNetworks: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16",
			},
		},
		{id: "use empty string for dst networks",
			nsAnnotations: map[string]string{
				k8s.ProxyDestinationGetNetworks: "",
			},
			spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{},
					Spec:       corev1.PodSpec{},
				},
			},
			expected: expectedProxyConfigs{
				identityContext:            &config.IdentityContext{},
				image:                      "gcr.io/linkerd-io/proxy",
				imagePullPolicy:            "IfNotPresent",
				proxyVersion:               proxyVersion,
				controlPort:                int32(9000),
				inboundPort:                int32(6000),
				adminPort:                  int32(6001),
				outboundPort:               int32(6002),
				proxyWaitBeforeExitSeconds: 0,
				logLevel:                   "info,linkerd2_proxy=debug",
				resourceRequirements: &l5dcharts.Resources{
					CPU: l5dcharts.Constraints{
						Limit:   "1",
						Request: "200m",
					},
					Memory: l5dcharts.Constraints{
						Limit:   "128",
						Request: "64",
					},
				},
				proxyUID:               int64(8888),
				initImage:              "gcr.io/linkerd-io/proxy-init",
				initImagePullPolicy:    "IfNotPresent",
				initVersion:            version.ProxyInitVersion,
				inboundSkipPorts:       "53,58-59",
				outboundSkipPorts:      "9079-9080",
				destinationGetNetworks: "",
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

			resourceConfig := NewResourceConfig(configs, OriginUnknown).WithKind("Deployment").WithNsAnnotations(testCase.nsAnnotations)
			if err := resourceConfig.parse(data); err != nil {
				t.Fatal(err)
			}

			t.Run("identityContext", func(t *testing.T) {
				expected := testCase.expected.identityContext
				if actual := resourceConfig.identityContext(); !reflect.DeepEqual(expected, actual) {
					testutil.Errorf(t, "Expected: %+v Actual: %+v", expected, actual)
				}
			})

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

			t.Run("proxyVersion", func(t *testing.T) {
				expected := testCase.expected.proxyVersion
				if actual := resourceConfig.proxyVersion(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyInitVersion", func(t *testing.T) {
				expected := testCase.expected.initVersion
				if actual := resourceConfig.proxyInitVersion(); expected != actual {
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

			t.Run("proxyAdminPort", func(t *testing.T) {
				expected := testCase.expected.adminPort
				if actual := resourceConfig.proxyAdminPort(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyOutboundPort", func(t *testing.T) {
				expected := testCase.expected.outboundPort
				if actual := resourceConfig.proxyOutboundPort(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyWaitBeforeExitSeconds", func(t *testing.T) {
				expected := testCase.expected.proxyWaitBeforeExitSeconds
				if actual := resourceConfig.proxyWaitBeforeExitSeconds(); expected != actual {
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

			t.Run("proxyUID", func(t *testing.T) {
				expected := testCase.expected.proxyUID
				if actual := resourceConfig.proxyUID(); expected != actual {
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

			t.Run("proxyRequireIdentityOnInboundPorts", func(t *testing.T) {
				expected := testCase.expected.requireIdentityOnInboundPorts
				if actual := resourceConfig.requireIdentityOnInboundPorts(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("destinationGetNetworks", func(t *testing.T) {
				expected := testCase.expected.destinationGetNetworks
				if actual := resourceConfig.destinationGetNetworks(); expected != actual {
					t.Errorf("Expected: %v Actual: %v", expected, actual)
				}
			})

			t.Run("proxyTraceCollectorService", func(t *testing.T) {
				var expected *l5dcharts.Trace
				if testCase.expected.trace != nil {
					expected = &l5dcharts.Trace{
						CollectorSvcAddr:    testCase.expected.trace.CollectorSvcAddr,
						CollectorSvcAccount: testCase.expected.trace.CollectorSvcAccount,
					}
				}

				if actual := resourceConfig.trace(); !reflect.DeepEqual(expected, actual) {
					t.Errorf("Expected: %+v Actual: %+v", expected, actual)
				}
			})
		})
	}
}
