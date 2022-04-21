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

	matchExpressionsSimple := []metav1.LabelSelectorRequirement{
		{
			Key:      "config.linkerd.io/admission-webhooks",
			Operator: "NotIn",
			Values:   []string{"disabled"},
		},
	}
	matchExpressionsInjector := append(matchExpressionsSimple, metav1.LabelSelectorRequirement{
		Key:      "kubernetes.io/metadata.name",
		Operator: "NotIn",
		Values:   []string{"kube-system", "cert-manager"},
	},
	)

	namespaceSelectorSimple := &metav1.LabelSelector{MatchExpressions: matchExpressionsSimple}
	namespaceSelectorInjector := &metav1.LabelSelector{MatchExpressions: matchExpressionsInjector}

	expected := &Values{
		ControllerImage:              "cr.l5d.io/linkerd/controller",
		ControllerReplicas:           1,
		ControllerUID:                2103,
		EnableH2Upgrade:              true,
		EnablePodAntiAffinity:        false,
		WebhookFailurePolicy:         "Ignore",
		DisableHeartBeat:             false,
		HeartbeatSchedule:            "",
		ClusterDomain:                "cluster.local",
		ClusterNetworks:              "10.0.0.0/8,100.64.0.0/10,172.16.0.0/12,192.168.0.0/16",
		ImagePullPolicy:              "IfNotPresent",
		CliVersion:                   "linkerd/cli dev-undefined",
		ControllerLogLevel:           "info",
		ControllerLogFormat:          "plain",
		LinkerdVersion:               version.Version,
		ProxyContainerName:           "linkerd-proxy",
		CNIEnabled:                   false,
		ControlPlaneTracing:          false,
		ControlPlaneTracingNamespace: "linkerd-jaeger",
		HighAvailability:             false,
		PodAnnotations:               map[string]string{},
		PodLabels:                    map[string]string{},
		EnableEndpointSlices:         true,
		PolicyController: &PolicyController{
			Image: &Image{
				Name: "cr.l5d.io/linkerd/policy-controller",
			},
			LogLevel:           "linkerd=info,warn",
			DefaultAllowPolicy: "all-unauthenticated",
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
		},
		Proxy: &Proxy{
			EnableExternalProfiles: false,
			Image: &Image{
				Name:    "cr.l5d.io/linkerd/proxy",
				Version: "dev-undefined",
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
			UID:                    2102,
			WaitBeforeExitSeconds:  0,
			OutboundConnectTimeout: "1000ms",
			InboundConnectTimeout:  "100ms",
			OpaquePorts:            "25,587,3306,4444,5432,6379,9300,11211",
			Await:                  true,
		},
		ProxyInit: &ProxyInit{
			IgnoreInboundPorts:  "4567,4568",
			IgnoreOutboundPorts: "4567,4568",
			LogLevel:            "",
			LogFormat:           "",
			Image: &Image{
				Name:    "cr.l5d.io/linkerd/proxy-init",
				Version: testVersion,
			},
			Resources: &Resources{
				CPU: Constraints{
					Limit:   "100m",
					Request: "100m",
				},
				Memory: Constraints{
					Limit:   "20Mi",
					Request: "20Mi",
				},
			},
			XTMountPath: &VolumeMountPath{
				Name:      "linkerd-proxy-init-xtables-lock",
				MountPath: "/run",
			},
		},
		Identity: &Identity{
			ServiceAccountTokenProjection: true,
			Issuer: &Issuer{
				ClockSkewAllowance: "20s",
				IssuanceLifetime:   "24h0m0s",
				TLS:                &IssuerTLS{},
				Scheme:             "linkerd.io/tls",
			},
		},
		NodeSelector: map[string]string{
			"kubernetes.io/os": "linux",
		},
		DebugContainer: &DebugContainer{
			Image: &Image{
				Name:    "cr.l5d.io/linkerd/debug",
				Version: "dev-undefined",
			},
		},

		ProxyInjector:    &Webhook{TLS: &TLS{}, NamespaceSelector: namespaceSelectorInjector},
		ProfileValidator: &Webhook{TLS: &TLS{}, NamespaceSelector: namespaceSelectorSimple},
		PolicyValidator:  &Webhook{TLS: &TLS{}, NamespaceSelector: namespaceSelectorSimple},
	}

	// pin the versions to ensure consistent test result.
	// in non-test environment, the default versions are read from the
	// values.yaml.
	actual.ProxyInit.Image.Version = testVersion

	// Make Add-On Values nil to not have to check for their defaults
	actual.ImagePullSecrets = nil

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
		expected.ProxyInjectorResources = controllerResources
		expected.HeartbeatResources = controllerResources

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

		expected.Proxy.Resources = &Resources{
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
		actual.ProxyInit.Image.Version = testVersion

		if !reflect.DeepEqual(expected, actual) {
			t.Errorf("Mismatch Helm HA defaults.\nExpected: %+v\nActual: %+v", expected, actual)
		}
	})
}
