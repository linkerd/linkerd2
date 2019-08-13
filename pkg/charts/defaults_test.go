package charts

import (
	"reflect"
	"testing"
	"time"
)

func TestReadDefaults(t *testing.T) {
	actual, err := ReadDefaults("linkerd2/", false)
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	clockSkewAllowance, err := time.ParseDuration("20s")
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	issuanceLifetime, err := time.ParseDuration("86400s")
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	expected := &DefaultValues{
		ControllerReplicas:               1,
		ControllerLogLevel:               "info",
		ControllerUID:                    2103,
		EnableExternalProfiles:           false,
		EnableH2Upgrade:                  true,
		ImagePullPolicy:                  "IfNotPresent",
		IdentityTrustDomain:              "cluster.local",
		IdentityIssuerClockSkewAllowance: clockSkewAllowance,
		IdentityIssuerIssuanceLifetime:   issuanceLifetime,
		OmitWebhookSideEffects:           false,
		PrometheusImage:                  "prom/prometheus:v2.11.1",
		ProxyAdminPort:                   4191,
		ProxyControlPort:                 4190,
		ProxyCPULimit:                    "",
		ProxyCPURequest:                  "",
		ProxyImageName:                   "gcr.io/linkerd-io/proxy",
		ProxyInboundPort:                 4143,
		ProxyInitImageName:               "gcr.io/linkerd-io/proxy-init",
		ProxyInitCPULimit:                "100m",
		ProxyInitCPURequest:              "10m",
		ProxyInitMemoryLimit:             "50Mi",
		ProxyInitMemoryRequest:           "10Mi",
		ProxyLogLevel:                    "warn,linkerd2_proxy=info",
		ProxyMemoryLimit:                 "",
		ProxyMemoryRequest:               "",
		ProxyOutboundPort:                4140,
		ProxyUID:                         2102,
		WebhookFailurePolicy:             "Ignore",
	}

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("Mismatch Helm defaults.\nExpected: %+v\nActual: %+v", expected, actual)
	}

	t.Run("HA", func(t *testing.T) {
		actual, err := ReadDefaults("linkerd2/", true)
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		expected.ControllerCPULimit = "1"
		expected.ControllerCPURequest = "100m"
		expected.ControllerMemoryLimit = "250Mi"
		expected.ControllerMemoryRequest = "50Mi"
		expected.ControllerReplicas = 3
		expected.GrafanaCPULimit = expected.ControllerCPULimit
		expected.GrafanaCPURequest = expected.ControllerCPURequest
		expected.GrafanaMemoryLimit = "1024Mi"
		expected.GrafanaMemoryRequest = "50Mi"
		expected.IdentityCPULimit = expected.ControllerCPULimit
		expected.IdentityCPURequest = expected.ControllerCPURequest
		expected.IdentityMemoryLimit = expected.ControllerMemoryLimit
		expected.IdentityMemoryRequest = "10Mi"
		expected.PrometheusCPULimit = "4"
		expected.PrometheusCPURequest = "300m"
		expected.PrometheusMemoryLimit = "8192Mi"
		expected.PrometheusMemoryRequest = "300Mi"
		expected.ProxyCPULimit = expected.ControllerCPULimit
		expected.ProxyCPURequest = expected.ControllerCPURequest
		expected.ProxyMemoryLimit = expected.ControllerMemoryLimit
		expected.ProxyMemoryRequest = "20Mi"
		expected.WebhookFailurePolicy = "Fail"

		if !reflect.DeepEqual(expected, actual) {
			t.Errorf("Mismatch Helm defaults.\nExpected: %+v\nActual: %+v", expected, actual)
		}
	})
}
