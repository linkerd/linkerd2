package linkerd2

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/linkerd/linkerd2/pkg/version"
)

func TestNewValues(t *testing.T) {
	actual, err := NewValues()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	testVersion := "linkerd-dev"

	namespaceSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "config.linkerd.io/admission-webhooks",
				Operator: "NotIn",
				Values:   []string{"disabled"},
			},
		},
	}

	expected := &Values{
		ControllerImage:             "ghcr.io/linkerd/controller",
		WebImage:                    "ghcr.io/linkerd/web",
		ControllerReplicas:          1,
		ControllerUID:               2103,
		EnableH2Upgrade:             true,
		EnablePodAntiAffinity:       false,
		WebhookFailurePolicy:        "Ignore",
		OmitWebhookSideEffects:      false,
		RestrictDashboardPrivileges: false,
		DisableHeartBeat:            false,
		HeartbeatSchedule:           "0 0 * * *",
		InstallNamespace:            true,
		Prometheus: Prometheus{
			"enabled": true,
		},
		Global: &Global{
			Namespace:                "linkerd",
			ClusterDomain:            "cluster.local",
			ClusterNetworks:          "10.0.0.0/8,100.64.0.0/10,172.16.0.0/12,192.168.0.0/16",
			ImagePullPolicy:          "IfNotPresent",
			CliVersion:               "linkerd/cli dev-undefined",
			ControllerComponentLabel: "linkerd.io/control-plane-component",
			ControllerLogLevel:       "info",
			ControllerImageVersion:   testVersion,
			LinkerdVersion:           version.Version,
			ControllerNamespaceLabel: "linkerd.io/control-plane-ns",
			WorkloadNamespaceLabel:   "linkerd.io/workload-ns",
			CreatedByAnnotation:      "linkerd.io/created-by",
			ProxyInjectAnnotation:    "linkerd.io/inject",
			ProxyInjectDisabled:      "disabled",
			LinkerdNamespaceLabel:    "linkerd.io/is-control-plane",
			ProxyContainerName:       "linkerd-proxy",
			CNIEnabled:               false,
			ControlPlaneTracing:      false,
			HighAvailability:         false,
			IdentityTrustDomain:      "cluster.local",
			PodAnnotations:           map[string]string{},
			PodLabels:                map[string]string{},
			Proxy: &Proxy{
				EnableExternalProfiles: false,
				Image: &Image{
					Name:       "ghcr.io/linkerd/proxy",
					PullPolicy: "IfNotPresent",
					Version:    testVersion,
				},
				LogLevel:  "warn,linkerd=info",
				LogFormat: "plain",
				Ports: &Ports{
					Admin:    4191,
					Control:  4190,
					Inbound:  4143,
					Outbound: 4140,
				},
				Resources: &Resources{
					CPU: Constraints{
						Limit:   "",
						Request: "",
					},
					Memory: Constraints{
						Limit:   "",
						Request: "",
					},
				},
				Trace: &Trace{
					CollectorSvcAddr:    "",
					CollectorSvcAccount: "default",
				},
				UID:                    2102,
				WaitBeforeExitSeconds:  0,
				OutboundConnectTimeout: "1000ms",
				InboundConnectTimeout:  "100ms",
			},
			ProxyInit: &ProxyInit{
				IgnoreInboundPorts:  "25,443,587,3306,11211,5432",
				IgnoreOutboundPorts: "25,443,587,3306,11211,5432",
				Image: &Image{
					Name:       "ghcr.io/linkerd/proxy-init",
					PullPolicy: "IfNotPresent",
					Version:    testVersion,
				},
				Resources: &Resources{
					CPU: Constraints{
						Limit:   "100m",
						Request: "10m",
					},
					Memory: Constraints{
						Limit:   "50Mi",
						Request: "10Mi",
					},
				},
				XTMountPath: &VolumeMountPath{
					Name:      "linkerd-proxy-init-xtables-lock",
					MountPath: "/run",
				},
			},
		},
		Identity: &Identity{
			Issuer: &Issuer{
				ClockSkewAllowance:  "20s",
				IssuanceLifetime:    "24h0m0s",
				CrtExpiryAnnotation: "linkerd.io/identity-issuer-expiry",
				TLS:                 &IssuerTLS{},
				Scheme:              "linkerd.io/tls",
			},
		},
		NodeSelector: map[string]string{
			"beta.kubernetes.io/os": "linux",
		},
		Dashboard: &Dashboard{
			Replicas: 1,
		},
		DebugContainer: &DebugContainer{
			Image: &Image{
				Name:       "ghcr.io/linkerd/debug",
				PullPolicy: "IfNotPresent",
				Version:    testVersion,
			},
		},

		ProxyInjector:    &ProxyInjector{TLS: &TLS{}, NamespaceSelector: namespaceSelector},
		ProfileValidator: &ProfileValidator{TLS: &TLS{}, NamespaceSelector: namespaceSelector},
		Tap:              &Tap{TLS: &TLS{}},
		Grafana: Grafana{
			"enabled": true,
		},
	}

	// pin the versions to ensure consistent test result.
	// in non-test environment, the default versions are read from the
	// values.yaml.
	actual.Global.ControllerImageVersion = testVersion
	actual.Global.Proxy.Image.Version = testVersion
	actual.Global.ProxyInit.Image.Version = testVersion
	actual.DebugContainer.Image.Version = testVersion

	// Make Add-On Values nil to not have to check for their defaults
	actual.Tracing = nil
	actual.Global.ImagePullSecrets = nil

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("Mismatch Helm values.\nExpected: %+v\nActual: %+v", expected, actual)
	}

	t.Run("HA", func(t *testing.T) {
		err := MergeHAValues(actual)

		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		expected.ControllerReplicas = 3
		expected.EnablePodAntiAffinity = true
		expected.WebhookFailurePolicy = "Fail"

		controllerResources := &Resources{
			CPU: Constraints{
				Request: "100m",
			},
			Memory: Constraints{
				Limit:   "250Mi",
				Request: "50Mi",
			},
		}
		expected.DestinationResources = controllerResources
		expected.PublicAPIResources = controllerResources
		expected.ProxyInjectorResources = controllerResources
		expected.SPValidatorResources = controllerResources
		expected.TapResources = controllerResources
		expected.WebResources = controllerResources
		expected.HeartbeatResources = controllerResources

		expected.Grafana = Grafana{
			"enabled": true,
			"resources": map[string]interface{}{
				"cpu": map[string]interface{}{
					"limit":   controllerResources.CPU.Limit,
					"request": controllerResources.CPU.Request,
				},
				"memory": map[string]interface{}{
					"limit":   "1024Mi",
					"request": "50Mi",
				},
			},
		}

		expected.IdentityResources = &Resources{
			CPU: Constraints{
				Limit:   controllerResources.CPU.Limit,
				Request: controllerResources.CPU.Request,
			},
			Memory: Constraints{
				Limit:   controllerResources.Memory.Limit,
				Request: "10Mi",
			},
		}

		expected.Prometheus = Prometheus{
			"enabled": true,
			"resources": map[string]interface{}{
				"cpu": map[string]interface{}{
					"limit":   "",
					"request": "300m",
				},
				"memory": map[string]interface{}{
					"limit":   "8192Mi",
					"request": "300Mi",
				},
			},
		}

		expected.Global.Proxy.Resources = &Resources{
			CPU: Constraints{
				Limit:   "",
				Request: controllerResources.CPU.Request,
			},
			Memory: Constraints{
				Limit:   controllerResources.Memory.Limit,
				Request: "20Mi",
			},
		}

		// pin the versions to ensure consistent test result.
		// in non-test environment, the default versions are read from the
		// values.yaml.
		actual.Global.ControllerImageVersion = testVersion
		actual.Global.Proxy.Image.Version = testVersion
		actual.Global.ProxyInit.Image.Version = testVersion
		actual.DebugContainer.Image.Version = testVersion
		// Make Add-On Values nil to not have to check for their defaults
		actual.Tracing = nil

		if !reflect.DeepEqual(expected, actual) {
			t.Errorf("Mismatch Helm HA defaults.\nExpected: %+v\nActual: %+v", expected, actual)
		}
	})
}
