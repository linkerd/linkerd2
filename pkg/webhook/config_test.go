package webhook

import (
	"fmt"
	"testing"

	injector "github.com/linkerd/linkerd2/controller/proxy-injector"
	injectorTmpl "github.com/linkerd/linkerd2/controller/proxy-injector/tmpl"
	validator "github.com/linkerd/linkerd2/controller/sp-validator"
	validatorTmpl "github.com/linkerd/linkerd2/controller/sp-validator/tmpl"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCreate(t *testing.T) {
	client := fake.NewSimpleClientset()

	rootCA, err := tls.GenerateRootCAWithDefaults("Test CA")
	if err != nil {
		t.Fatalf("failed to create root CA: %s", err)
	}

	testCases := []struct {
		testName    string
		configName  string
		serviceName string
		templateStr string
		ops         ConfigOps
	}{
		{
			testName:    "Mutating webhook",
			configName:  k8sPkg.ProxyInjectorWebhookConfig,
			serviceName: "mutatingwebhook.linkerd.io",
			templateStr: injectorTmpl.MutatingWebhookConfigurationSpec,
			ops:         injector.NewOps(client),
		},
		{
			testName:    "Validating webhook",
			configName:  k8sPkg.SPValidatorWebhookConfig,
			serviceName: "validatingwebhook.linkerd.io",
			templateStr: validatorTmpl.ValidatingWebhookConfigurationSpec,
			ops:         validator.NewOps(client),
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf(tc.testName), func(t *testing.T) {
			webhookConfig := &Config{
				ControllerNamespace: "linkerd",
				WebhookConfigName:   tc.configName,
				WebhookServiceName:  tc.serviceName,
				RootCA:              rootCA,
				TemplateStr:         tc.templateStr,
				Ops:                 tc.ops,
			}

			// expect configuration to not exist
			exists, err := webhookConfig.Exists()
			if err != nil {
				t.Fatal("Unexpected error: ", err)
			}
			if exists {
				t.Error("Unexpected webhook configuration. Expect resource to not exist")
			}

			// create the webhook configuration
			if _, err := webhookConfig.Create(); err != nil {
				t.Fatal("Unexpected error: ", err)
			}

			// expect webhook configuration to exist
			exists, err = webhookConfig.Exists()
			if err != nil {
				t.Fatal("Unexpected error: ", err)
			}
			if !exists {
				t.Error("Expected webhook configuration to exist")
			}
		})
	}
}
