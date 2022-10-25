package linkerd2

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/go-test/deep"
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

	defaultDeploymentStrategy := map[string]interface{}{
		"rollingUpdate": map[string]interface{}{
			"maxUnavailable": "25%",
			"maxSurge":       "25%",
		},
	}
	expected := &Values{
		ControllerImage:              "cr.l5d.io/linkerd/controller",
		ControllerReplicas:           1,
		ControllerUID:                2103,
		EnableH2Upgrade:              true,
		EnablePodAntiAffinity:        false,
		WebhookFailurePolicy:         "Ignore",
		DisableHeartBeat:             false,
		DeploymentStrategy:           defaultDeploymentStrategy,
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
		EnablePodDisruptionBudget:    false,
		PodMonitor: &PodMonitor{
			Enabled:        false,
			ScrapeInterval: "10s",
			ScrapeTimeout:  "10s",
			Controller: &PodMonitorController{
				Enabled: true,
				NamespaceSelector: `matchNames:
  - {{ .Release.Namespace }}
  - linkerd-viz
  - linkerd-jaeger
`,
			},
			ServiceMirror: &PodMonitorComponent{Enabled: true},
			Proxy:         &PodMonitorComponent{Enabled: true},
		},
		PolicyController: &PolicyController{
			Image: &Image{
				Name: "cr.l5d.io/linkerd/policy-controller",
			},
			LogLevel: "info",
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
			ProbeNetworks: []string{"0.0.0.0/0"},
		},
		Proxy: &Proxy{
			EnableExternalProfiles: false,
			Image: &Image{
				Name:    "cr.l5d.io/linkerd/proxy",
				Version: "",
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
			DefaultInboundPolicy:   "all-unauthenticated",
		},
		ProxyInit: &ProxyInit{
			IptablesMode:        "legacy",
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
			RunAsRoot: false,
			RunAsUser: 65534,
		},
		NetworkValidator: &NetworkValidator{
			LogLevel:    "debug",
			LogFormat:   "plain",
			ConnectAddr: "1.1.1.1:20001",
			ListenAddr:  "0.0.0.0:4140",
			Timeout:     "10s",
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

	if diff := deep.Equal(expected, actual); diff != nil {
		t.Errorf("Helm values\n%+v", diff)
	}

	t.Run("HA", func(t *testing.T) {

		err := MergeHAValues(actual)

		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		haDeploymentStrategy := map[string]interface{}{
			"rollingUpdate": map[string]interface{}{
				"maxUnavailable": 1.0,
				"maxSurge":       "25%",
			},
		}

		expected.ControllerReplicas = 3
		expected.EnablePodAntiAffinity = true
		expected.EnablePodDisruptionBudget = true
		expected.DeploymentStrategy = haDeploymentStrategy
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

		if diff := deep.Equal(expected, actual); diff != nil {
			t.Errorf("HA Helm values\n%+v", diff)
		}
	})
}

// TestHAValuesParsing tests whether values commonly used in HA deployments have
// appropriate types and can be successfully parsed.
func TestHAValuesParsing(t *testing.T) {
	yml := `
enablePodDisruptionBudget: true
deploymentStrategy:
  rollingUpdate:
    maxUnavailable: 1
    maxSurge: 25%
enablePodAntiAffinity: true
nodeAffinity:
  requiredDuringSchedulingIgnoredDuringExecution:
    nodeSelectorTerms:
    - matchExpressions:
      - key: cloud.google.com/gke-preemptible
        operator: DoesNotExist
nodeSelector:
  kubernetes.io/os: linux
proxy:
  resources:
    cpu:
      request: 100m
    memory:
      limit: 250Mi
      request: 20Mi`

	err := yaml.Unmarshal([]byte(yml), &Values{})
	if err != nil {
		t.Errorf("Failed to unamarshal HA values from yaml: %v\nValues: %v", err, yml)
	}
}
