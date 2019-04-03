package main

import (
	"github.com/linkerd/linkerd2/controller/k8s"
	validator "github.com/linkerd/linkerd2/controller/sp-validator"
	"github.com/linkerd/linkerd2/controller/sp-validator/tmpl"
	"github.com/linkerd/linkerd2/controller/webhook"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
)

func main() {
	webhook.Launch(&webhook.Config{
		TemplateStr: tmpl.ValidatingWebhookConfigurationSpec,
		Ops:         &validator.Ops{},
	},
		[]k8s.APIResource{},
		9997,
		pkgK8s.SPValidatorWebhookServiceName,
		validator.AdmitSP,
	)
}
