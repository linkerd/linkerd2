package main

import (
	validator "github.com/linkerd/linkerd2/controller/sp-validator"
	"github.com/linkerd/linkerd2/controller/sp-validator/tmpl"
	"github.com/linkerd/linkerd2/controller/webhook"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

func main() {
	webhook.Launch(&webhook.Config{
		MetricsPort:        9997,
		WebhookConfigName:  k8s.SPValidatorWebhookConfigName,
		WebhookServiceName: k8s.SPValidatorWebhookServiceName,
		TemplateStr:        tmpl.ValidatingWebhookConfigurationSpec,
		Ops:                &validator.Ops{},
		Handler:            validator.AdmitSP,
	})
}
