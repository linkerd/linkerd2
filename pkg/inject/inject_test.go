package inject

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOverrides(t *testing.T) {
	var (
		kind         = "Deployment"
		globalConfig = &config.Global{}

		testCases = []struct {
			id                   string
			podAnnotations       map[string]string
			namespaceAnnotations map[string]string
			proxyConfig          *config.Proxy
			expectedOverrides    map[string]string
		}{
			{id: "by pod annotations",
				podAnnotations: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:abcde",
					k8s.ProxyImagePullPolicyAnnotation:         "Always",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:abcde",
					k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
					k8s.ProxyControlPortAnnotation:             "4000",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "4222,6222",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "8079,8080",
					k8s.ProxyInboundPortAnnotation:             "5000",
					k8s.ProxyMetricsPortAnnotation:             "5001",
					k8s.ProxyOutboundPortAnnotation:            "5002",
					k8s.ProxyRequestCPUAnnotation:              "0.2",
					k8s.ProxyRequestMemoryAnnotation:           "64",
					k8s.ProxyLimitCPUAnnotation:                "1",
					k8s.ProxyLimitMemoryAnnotation:             "128",
					k8s.ProxyUIDAnnotation:                     "9700",
					k8s.ProxyLogLevelAnnotation:                "info,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
				expectedOverrides: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:abcde",
					k8s.ProxyImagePullPolicyAnnotation:         "Always",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:abcde",
					k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
					k8s.ProxyControlPortAnnotation:             "4000",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "4222,6222",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "8079,8080",
					k8s.ProxyInboundPortAnnotation:             "5000",
					k8s.ProxyMetricsPortAnnotation:             "5001",
					k8s.ProxyOutboundPortAnnotation:            "5002",
					k8s.ProxyRequestCPUAnnotation:              "0.2",
					k8s.ProxyRequestMemoryAnnotation:           "64",
					k8s.ProxyLimitCPUAnnotation:                "1",
					k8s.ProxyLimitMemoryAnnotation:             "128",
					k8s.ProxyUIDAnnotation:                     "9700",
					k8s.ProxyLogLevelAnnotation:                "info,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
			},
			{id: "by namespace annotations",
				namespaceAnnotations: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:abcde",
					k8s.ProxyImagePullPolicyAnnotation:         "Always",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:abcde",
					k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
					k8s.ProxyControlPortAnnotation:             "4000",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "4222,6222",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "8079,8080",
					k8s.ProxyInboundPortAnnotation:             "5000",
					k8s.ProxyMetricsPortAnnotation:             "5001",
					k8s.ProxyOutboundPortAnnotation:            "5002",
					k8s.ProxyRequestCPUAnnotation:              "0.2",
					k8s.ProxyRequestMemoryAnnotation:           "64",
					k8s.ProxyLimitCPUAnnotation:                "1",
					k8s.ProxyLimitMemoryAnnotation:             "128",
					k8s.ProxyUIDAnnotation:                     "9700",
					k8s.ProxyLogLevelAnnotation:                "info,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
				expectedOverrides: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:abcde",
					k8s.ProxyImagePullPolicyAnnotation:         "Always",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:abcde",
					k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
					k8s.ProxyControlPortAnnotation:             "4000",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "4222,6222",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "8079,8080",
					k8s.ProxyInboundPortAnnotation:             "5000",
					k8s.ProxyMetricsPortAnnotation:             "5001",
					k8s.ProxyOutboundPortAnnotation:            "5002",
					k8s.ProxyRequestCPUAnnotation:              "0.2",
					k8s.ProxyRequestMemoryAnnotation:           "64",
					k8s.ProxyLimitCPUAnnotation:                "1",
					k8s.ProxyLimitMemoryAnnotation:             "128",
					k8s.ProxyUIDAnnotation:                     "9700",
					k8s.ProxyLogLevelAnnotation:                "info,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
			},
			{id: "by default proxy config",
				proxyConfig: &config.Proxy{
					ProxyImage:     &config.Image{ImageName: "gcr.io/linkerd-io/proxy:abcde", PullPolicy: "Always"},
					ProxyInitImage: &config.Image{ImageName: "gcr.io/linkerd-io/proxy-init:abcde", PullPolicy: "Always"},
					ControlPort:    &config.Port{Port: 4000},
					IgnoreInboundPorts: []*config.Port{
						&config.Port{Port: 4222},
						&config.Port{Port: 6222},
					},
					IgnoreOutboundPorts: []*config.Port{
						&config.Port{Port: 8079},
						&config.Port{Port: 8080},
					},
					InboundPort:  &config.Port{Port: 5000},
					MetricsPort:  &config.Port{Port: 5001},
					OutboundPort: &config.Port{Port: 5002},
					Resource: &config.ResourceRequirements{
						RequestCpu:    "0.2",
						RequestMemory: "64",
						LimitCpu:      "1",
						LimitMemory:   "128",
					},
					ProxyUid:                9700,
					LogLevel:                &config.LogLevel{Level: "info,linkerd2_proxy=debug"},
					DisableExternalProfiles: true,
				},
				expectedOverrides: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:abcde",
					k8s.ProxyImagePullPolicyAnnotation:         "Always",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:abcde",
					k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
					k8s.ProxyControlPortAnnotation:             "4000",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "4222,6222",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "8079,8080",
					k8s.ProxyInboundPortAnnotation:             "5000",
					k8s.ProxyMetricsPortAnnotation:             "5001",
					k8s.ProxyOutboundPortAnnotation:            "5002",
					k8s.ProxyRequestCPUAnnotation:              "0.2",
					k8s.ProxyRequestMemoryAnnotation:           "64",
					k8s.ProxyLimitCPUAnnotation:                "1",
					k8s.ProxyLimitMemoryAnnotation:             "128",
					k8s.ProxyUIDAnnotation:                     "9700",
					k8s.ProxyLogLevelAnnotation:                "info,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
			},
			{id: "by pod annotations over namespace annotations",
				namespaceAnnotations: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:abcde",
					k8s.ProxyImagePullPolicyAnnotation:         "IfNotPresent",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:abcde",
					k8s.ProxyInitImagePullPolicyAnnotation:     "IfNotPresent",
					k8s.ProxyControlPortAnnotation:             "4000",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "4222,6222",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "8079,8080",
					k8s.ProxyInboundPortAnnotation:             "5000",
					k8s.ProxyMetricsPortAnnotation:             "5001",
					k8s.ProxyOutboundPortAnnotation:            "5002",
					k8s.ProxyRequestCPUAnnotation:              "0.2",
					k8s.ProxyRequestMemoryAnnotation:           "64",
					k8s.ProxyLimitCPUAnnotation:                "1",
					k8s.ProxyLimitMemoryAnnotation:             "128",
					k8s.ProxyUIDAnnotation:                     "9700",
					k8s.ProxyLogLevelAnnotation:                "info,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
				podAnnotations: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:fghij",
					k8s.ProxyImagePullPolicyAnnotation:         "Always",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:fghij",
					k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
					k8s.ProxyControlPortAnnotation:             "5000",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "3306,6379",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "7000,7070",
					k8s.ProxyInboundPortAnnotation:             "9000",
					k8s.ProxyMetricsPortAnnotation:             "9001",
					k8s.ProxyOutboundPortAnnotation:            "9002",
					k8s.ProxyRequestCPUAnnotation:              "0.15",
					k8s.ProxyRequestMemoryAnnotation:           "120",
					k8s.ProxyLimitCPUAnnotation:                "1.5",
					k8s.ProxyLimitMemoryAnnotation:             "256",
					k8s.ProxyUIDAnnotation:                     "8500",
					k8s.ProxyLogLevelAnnotation:                "debug,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
				expectedOverrides: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:fghij",
					k8s.ProxyImagePullPolicyAnnotation:         "Always",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:fghij",
					k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
					k8s.ProxyControlPortAnnotation:             "5000",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "3306,6379",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "7000,7070",
					k8s.ProxyInboundPortAnnotation:             "9000",
					k8s.ProxyMetricsPortAnnotation:             "9001",
					k8s.ProxyOutboundPortAnnotation:            "9002",
					k8s.ProxyRequestCPUAnnotation:              "0.15",
					k8s.ProxyRequestMemoryAnnotation:           "120",
					k8s.ProxyLimitCPUAnnotation:                "1.5",
					k8s.ProxyLimitMemoryAnnotation:             "256",
					k8s.ProxyUIDAnnotation:                     "8500",
					k8s.ProxyLogLevelAnnotation:                "debug,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
			},
			{id: "by merging pod and namespace annotations",
				namespaceAnnotations: map[string]string{
					k8s.ProxyImageAnnotation:               "gcr.io/linkerd-io/proxy:abcde",
					k8s.ProxyImagePullPolicyAnnotation:     "Always",
					k8s.ProxyInitImageAnnotation:           "gcr.io/linkerd-io/proxy-init:abcde",
					k8s.ProxyInitImagePullPolicyAnnotation: "Always",
					k8s.ProxyControlPortAnnotation:         "4000",
					k8s.ProxyIgnoreInboundPortsAnnotation:  "4222,6222",
					k8s.ProxyIgnoreOutboundPortsAnnotation: "8079,8080",
					k8s.ProxyInboundPortAnnotation:         "5000",
					k8s.ProxyMetricsPortAnnotation:         "5001",
					k8s.ProxyOutboundPortAnnotation:        "5002"},
				podAnnotations: map[string]string{
					k8s.ProxyRequestCPUAnnotation:              "0.15",
					k8s.ProxyRequestMemoryAnnotation:           "120",
					k8s.ProxyLimitCPUAnnotation:                "1.5",
					k8s.ProxyLimitMemoryAnnotation:             "256",
					k8s.ProxyUIDAnnotation:                     "8500",
					k8s.ProxyLogLevelAnnotation:                "debug,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
				expectedOverrides: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:abcde",
					k8s.ProxyImagePullPolicyAnnotation:         "Always",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:abcde",
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
			{id: "by pod annotations over namespace annotations and config map",
				namespaceAnnotations: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:abcde",
					k8s.ProxyImagePullPolicyAnnotation:         "Never",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:abcde",
					k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
					k8s.ProxyControlPortAnnotation:             "Never",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "4222,6222",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "8079,8080",
					k8s.ProxyInboundPortAnnotation:             "5000",
					k8s.ProxyMetricsPortAnnotation:             "5001",
					k8s.ProxyOutboundPortAnnotation:            "5002",
					k8s.ProxyRequestCPUAnnotation:              "0.2",
					k8s.ProxyRequestMemoryAnnotation:           "64",
					k8s.ProxyLimitCPUAnnotation:                "1",
					k8s.ProxyLimitMemoryAnnotation:             "128",
					k8s.ProxyUIDAnnotation:                     "9700",
					k8s.ProxyLogLevelAnnotation:                "info,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
				proxyConfig: &config.Proxy{
					ProxyImage:     &config.Image{ImageName: "gcr.io/linkerd-io/proxy:klmno", PullPolicy: "IfNotPresent"},
					ProxyInitImage: &config.Image{ImageName: "gcr.io/linkerd-io/proxy-init:klmno", PullPolicy: "IfNotPresent"},
					ControlPort:    &config.Port{Port: 9000},
					IgnoreInboundPorts: []*config.Port{
						&config.Port{Port: 53},
					},
					IgnoreOutboundPorts: []*config.Port{
						&config.Port{Port: 9079},
					},
					InboundPort:  &config.Port{Port: 6000},
					MetricsPort:  &config.Port{Port: 6001},
					OutboundPort: &config.Port{Port: 6002},
					Resource: &config.ResourceRequirements{
						RequestCpu:    "0.2",
						RequestMemory: "64",
						LimitCpu:      "1",
						LimitMemory:   "128",
					},
					ProxyUid:                8888,
					LogLevel:                &config.LogLevel{Level: "info,linkerd2_proxy=debug"},
					DisableExternalProfiles: true,
				},
				podAnnotations: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:fghij",
					k8s.ProxyImagePullPolicyAnnotation:         "Always",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:fghij",
					k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
					k8s.ProxyControlPortAnnotation:             "5000",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "3306,6379",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "7000,7070",
					k8s.ProxyInboundPortAnnotation:             "9000",
					k8s.ProxyMetricsPortAnnotation:             "9001",
					k8s.ProxyOutboundPortAnnotation:            "9002",
					k8s.ProxyRequestCPUAnnotation:              "0.15",
					k8s.ProxyRequestMemoryAnnotation:           "120",
					k8s.ProxyLimitCPUAnnotation:                "1.5",
					k8s.ProxyLimitMemoryAnnotation:             "256",
					k8s.ProxyUIDAnnotation:                     "8500",
					k8s.ProxyLogLevelAnnotation:                "debug,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
				expectedOverrides: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:fghij",
					k8s.ProxyImagePullPolicyAnnotation:         "Always",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:fghij",
					k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
					k8s.ProxyControlPortAnnotation:             "5000",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "3306,6379",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "7000,7070",
					k8s.ProxyInboundPortAnnotation:             "9000",
					k8s.ProxyMetricsPortAnnotation:             "9001",
					k8s.ProxyOutboundPortAnnotation:            "9002",
					k8s.ProxyRequestCPUAnnotation:              "0.15",
					k8s.ProxyRequestMemoryAnnotation:           "120",
					k8s.ProxyLimitCPUAnnotation:                "1.5",
					k8s.ProxyLimitMemoryAnnotation:             "256",
					k8s.ProxyUIDAnnotation:                     "8500",
					k8s.ProxyLogLevelAnnotation:                "debug,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
			},
			{id: "by merging pod and namespace annotations, and config map",
				namespaceAnnotations: map[string]string{
					k8s.ProxyImageAnnotation:               "gcr.io/linkerd-io/proxy:abcde",
					k8s.ProxyImagePullPolicyAnnotation:     "Always",
					k8s.ProxyInitImageAnnotation:           "gcr.io/linkerd-io/proxy-init:abcde",
					k8s.ProxyInitImagePullPolicyAnnotation: "Always",
				},
				proxyConfig: &config.Proxy{
					ControlPort:  &config.Port{Port: 9000},
					InboundPort:  &config.Port{Port: 6000},
					MetricsPort:  &config.Port{Port: 6001},
					OutboundPort: &config.Port{Port: 6002},
				},
				podAnnotations: map[string]string{
					k8s.ProxyIgnoreInboundPortsAnnotation:      "3306,6379",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "7000,7070",
					k8s.ProxyRequestCPUAnnotation:              "0.15",
					k8s.ProxyRequestMemoryAnnotation:           "120",
					k8s.ProxyLimitCPUAnnotation:                "1.5",
					k8s.ProxyLimitMemoryAnnotation:             "256",
					k8s.ProxyUIDAnnotation:                     "8500",
					k8s.ProxyLogLevelAnnotation:                "debug,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
				expectedOverrides: map[string]string{
					k8s.ProxyImageAnnotation:                   "gcr.io/linkerd-io/proxy:abcde",
					k8s.ProxyImagePullPolicyAnnotation:         "Always",
					k8s.ProxyInitImageAnnotation:               "gcr.io/linkerd-io/proxy-init:abcde",
					k8s.ProxyInitImagePullPolicyAnnotation:     "Always",
					k8s.ProxyControlPortAnnotation:             "9000",
					k8s.ProxyIgnoreInboundPortsAnnotation:      "3306,6379",
					k8s.ProxyIgnoreOutboundPortsAnnotation:     "7000,7070",
					k8s.ProxyInboundPortAnnotation:             "6000",
					k8s.ProxyMetricsPortAnnotation:             "6001",
					k8s.ProxyOutboundPortAnnotation:            "6002",
					k8s.ProxyRequestCPUAnnotation:              "0.15",
					k8s.ProxyRequestMemoryAnnotation:           "120",
					k8s.ProxyLimitCPUAnnotation:                "1.5",
					k8s.ProxyLimitMemoryAnnotation:             "256",
					k8s.ProxyUIDAnnotation:                     "8500",
					k8s.ProxyLogLevelAnnotation:                "debug,linkerd2_proxy=debug",
					k8s.ProxyDisableExternalProfilesAnnotation: "true"},
			},
		}
	)

	for _, testCase := range testCases {
		t.Run(fmt.Sprintf("%s", testCase.id), func(t *testing.T) {
			resourceConfig := NewResourceConfig(globalConfig, testCase.proxyConfig)

			// insert the namespace and pod annotations
			resourceConfig = resourceConfig.WithKind(kind).WithNsAnnotations(testCase.namespaceAnnotations)
			resourceConfig.podMeta = objMeta{&metav1.ObjectMeta{}}
			resourceConfig.podMeta.Annotations = testCase.podAnnotations

			if err := resourceConfig.useOverridesOrDefaults(); err != nil {
				t.Fatal(err)
			}

			if len(resourceConfig.proxyConfigOverrides) != len(testCase.expectedOverrides) {
				t.Errorf("Number of config overrides don't match. Expected: %d. Actual: %d", len(testCase.expectedOverrides), len(resourceConfig.proxyConfigOverrides))
			}

			for key, actual := range resourceConfig.proxyConfigOverrides {
				expected, exist := testCase.expectedOverrides[key]
				if !exist {
					t.Errorf("Expected annotation %q to exist", key)
				}

				if !reflect.DeepEqual(expected, actual) {
					t.Errorf("Annotation: %q. Expected: %v (%T). Actual: %v (%T)", key, expected, expected, actual, actual)
				}
			}
		})
	}
}
