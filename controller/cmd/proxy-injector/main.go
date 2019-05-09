package main

import (
	"github.com/linkerd/linkerd2/controller/k8s"
	injector "github.com/linkerd/linkerd2/controller/proxy-injector"
	"github.com/linkerd/linkerd2/controller/proxy-injector/tmpl"
	"github.com/linkerd/linkerd2/controller/webhook"
)

func main() {
	config := &webhook.Config{
		TemplateStr: tmpl.MutatingWebhookConfigurationSpec,
		Ops:         &injector.Ops{},
	}
	webhook.Launch(
		config,
		[]k8s.APIResource{k8s.NS, k8s.RS},
		9995,
		injector.Inject,
	)
}
