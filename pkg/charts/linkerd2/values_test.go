package linkerd2

import (
	"reflect"
	"testing"
)

func TestNewValues(t *testing.T) {
	actual, err := NewValues(false)
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	testVersion := "linkerd-dev"

	expected := &Values{
		Stage:                       "",
		ControllerImage:             "gcr.io/linkerd-io/controller",
		WebImage:                    "gcr.io/linkerd-io/web",
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
			ImagePullPolicy:          "IfNotPresent",
			CliVersion:               "linkerd/cli dev-undefined",
			ControllerComponentLabel: "linkerd.io/control-plane-component",
			ControllerLogLevel:       "info",
			ControllerImageVersion:   testVersion,
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
			Proxy: &Proxy{
				EnableExternalProfiles: false,
				Image: &Image{
					Name:       "gcr.io/linkerd-io/proxy",
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
				DestinationGetNetworks: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16",
				OutboundConnectTimeout: "1000ms",
				InboundConnectTimeout:  "100ms",
			},
			ProxyInit: &ProxyInit{
				Image: &Image{
					Name:       "gcr.io/linkerd-io/proxy-init",
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
				IssuanceLifetime:    "86400s",
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
				Name:       "gcr.io/linkerd-io/debug",
				PullPolicy: "IfNotPresent",
				Version:    testVersion,
			},
		},

		ProxyInjector:    &ProxyInjector{TLS: &TLS{}},
		ProfileValidator: &ProfileValidator{TLS: &TLS{}},
		Tap:              &Tap{TLS: &TLS{}},
		SMIMetrics: &SMIMetrics{
			Image: "deislabs/smi-metrics:v0.2.1",
			TLS:   &TLS{},
		},
		Grafana: Grafana{
			"enabled": true,
			"name":    "linkerd-grafana",
			"image": map[string]interface{}{
				"name": "gcr.io/linkerd-io/grafana",
			},
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
		t.Errorf("Mismatch Helm values.\nExpected: %+v\nActual: %+v", expected, actual)
	}

	t.Run("HA", func(t *testing.T) {
		actual, err := NewValues(true)

		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		expected.ControllerReplicas = 3
		expected.EnablePodAntiAffinity = true
		expected.WebhookFailurePolicy = "Fail"

		controllerResources := &Resources{
			CPU: Constraints{
				Limit:   "1",
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
			"name":    "linkerd-grafana",
			"image": map[string]interface{}{
				"name": "gcr.io/linkerd-io/grafana",
			},
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
					"limit":   "4",
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
				Limit:   controllerResources.CPU.Limit,
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
