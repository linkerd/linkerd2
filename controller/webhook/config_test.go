package webhook

import (
	"fmt"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	injector "github.com/linkerd/linkerd2/controller/proxy-injector"
	injectorTmpl "github.com/linkerd/linkerd2/controller/proxy-injector/tmpl"
	validator "github.com/linkerd/linkerd2/controller/sp-validator"
	validatorTmpl "github.com/linkerd/linkerd2/controller/sp-validator/tmpl"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
)

func TestCreate(t *testing.T) {
	k8sAPI, err := k8s.NewFakeAPI()
	if err != nil {
		panic(err)
	}

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
			configName:  k8sPkg.ProxyInjectorWebhookConfigName,
			serviceName: "mutatingwebhook.linkerd.io",
			templateStr: injectorTmpl.MutatingWebhookConfigurationSpec,
			ops:         &injector.Ops{},
		},
		{
			testName:    "Validating webhook",
			configName:  k8sPkg.SPValidatorWebhookConfigName,
			serviceName: "validatingwebhook.linkerd.io",
			templateStr: validatorTmpl.ValidatingWebhookConfigurationSpec,
			ops:         &validator.Ops{},
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf(tc.testName), func(t *testing.T) {
			webhookConfig := &Config{
				WebhookConfigName:   tc.configName,
				WebhookServiceName:  tc.serviceName,
				TemplateStr:         tc.templateStr,
				Ops:                 tc.ops,
				api:                 k8sAPI,
				controllerNamespace: "linkerd",
				rootCA:              rootCA,
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
