package charts

import (
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
)

func TestNewValues(t *testing.T) {
	actual, err := NewValues()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	expected := &Values{
		Stage:                       "",
		Namespace:                   "linkerd",
		ClusterDomain:               "cluster.local",
		ControllerImage:             "gcr.io/linkerd-io/controller",
		WebImage:                    "gcr.io/linkerd-io/web",
		PrometheusImage:             "prom/prometheus:v2.11.1",
		GrafanaImage:                "gcr.io/linkerd-io/grafana",
		ImagePullPolicy:             "IfNotPresent",
		UUID:                        "",
		CliVersion:                  "linkerd/cli dev-undefined",
		ControllerReplicas:          1,
		ControllerLogLevel:          "info",
		PrometheusLogLevel:          "info",
		ControllerComponentLabel:    "linkerd.io/control-plane-component",
		ControllerNamespaceLabel:    "linkerd.io/control-plane-ns",
		CreatedByAnnotation:         "linkerd.io/created-by",
		ProxyContainerName:          "linkerd-proxy",
		ProxyInjectAnnotation:       "linkerd.io/inject",
		ProxyInjectDisabled:         "disabled",
		LinkerdNamespaceLabel:       "linkerd.io/is-control-plane",
		ControllerUID:               2103,
		EnableH2Upgrade:             true,
		EnablePodAntiAffinity:       false,
		HighAvailability:            false,
		NoInitContainer:             false,
		WebhookFailurePolicy:        "Ignore",
		OmitWebhookSideEffects:      false,
		RestrictDashboardPrivileges: false,
		HeartbeatSchedule:           "0 0 * * *",

		DestinationResources:   &Resources{},
		GrafanaResources:       &Resources{},
		HeartbeatResources:     &Resources{},
		IdentityResources:      &Resources{},
		PrometheusResources:    &Resources{},
		ProxyInjectorResources: &Resources{},
		PublicAPIResources:     &Resources{},
		SPValidatorResources:   &Resources{},
		TapResources:           &Resources{},
		WebResources:           &Resources{},

		Identity: &Identity{
			TrustDomain: "cluster.local",
			Issuer: &Issuer{
				ClockSkewAllowance:  "20s",
				IssuanceLifetime:    "86400s",
				CrtExpiryAnnotation: "linkerd.io/identity-issuer-expiry",
				TLS:                 &TLS{},
			},
		},

		ProxyInjector:    &ProxyInjector{TLS: &TLS{}},
		ProfileValidator: &ProfileValidator{TLS: &TLS{}},
		Tap:              &Tap{TLS: &TLS{}},

		Proxy: &Proxy{
			Component:              k8s.Deployment,
			EnableExternalProfiles: false,
			Image: &Image{
				Name:       "gcr.io/linkerd-io/proxy",
				PullPolicy: "IfNotPresent",
				Version:    "edge-19.8.3",
			},
			LogLevel: "warn,linkerd2_proxy=info",
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
			UID: 2102,
		},

		ProxyInit: &ProxyInit{
			Image: &Image{
				Name:       "gcr.io/linkerd-io/proxy-init",
				PullPolicy: "IfNotPresent",
				Version:    "v1.0.0",
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
		},
	}

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("Mismatch Helm values.\nExpected: %+v\nActual: %+v", expected.Identity.Issuer, actual.Identity.Issuer)
	}
}
