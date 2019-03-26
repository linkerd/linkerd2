package main

import (
	injector "github.com/linkerd/linkerd2/controller/proxy-injector"
	"github.com/linkerd/linkerd2/controller/proxy-injector/tmpl"
	"github.com/linkerd/linkerd2/controller/webhook"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

func main() {
	webhook.Launch(&webhook.Config{
		MetricsPort:        9995,
		WebhookConfigName:  k8s.ProxyInjectorWebhookConfigName,
		WebhookServiceName: k8s.ProxyInjectorWebhookServiceName,
		TemplateStr:        tmpl.MutatingWebhookConfigurationSpec,
		Ops:                &injector.Ops{},
		Handler:            injector.Inject,
	})
}
