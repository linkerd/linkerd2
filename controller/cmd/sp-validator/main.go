package main

import (
	"flag"
	"os"
	"os/signal"

	"github.com/linkerd/linkerd2/controller/k8s"
	validator "github.com/linkerd/linkerd2/controller/sp-validator"
	"github.com/linkerd/linkerd2/controller/sp-validator/tmpl"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/webhook"
	log "github.com/sirupsen/logrus"
)

func main() {
	metricsAddr := flag.String("metrics-addr", ":9990", "address to serve scrapable metrics on")
	addr := flag.String("addr", ":8443", "address to serve on")
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig")
	controllerNamespace := flag.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	webhookServiceName := flag.String("webhook-service", "linkerd-sp-validator.linkerd.io", "name of the admission webhook")

	flags.ConfigureAndParse()

	stop := make(chan os.Signal, 1)
	defer close(stop)
	signal.Notify(stop, os.Interrupt, os.Kill)

	k8sClient, err := k8s.NewClientSet(*kubeconfig)
	if err != nil {
		log.Fatalf("failed to initialize Kubernetes client: %s", err)
	}

	rootCA, err := tls.GenerateRootCAWithDefaults("Service Profiles Validating Webhook Admission Controller CA")
	if err != nil {
		log.Fatalf("failed to create root CA: %s", err)
	}

	webhookConfig := &webhook.Config{
		ControllerNamespace: *controllerNamespace,
		WebhookConfigName:   k8sPkg.SPValidatorWebhookConfig,
		WebhookServiceName:  *webhookServiceName,
		RootCA:              rootCA,
		TemplateStr:         tmpl.ValidatingWebhookConfigurationSpec,
		Ops:                 validator.NewOps(k8sClient),
	}
	selfLink, err := webhookConfig.Create()
	if err != nil {
		log.Fatalf("failed to create the validating webhook configurations resource: %s", err)
	}
	log.Infof("created validating webhook configuration: %s", selfLink)

	s, err := webhook.NewServer(k8sClient, *addr, "linkerd-sp-validator", *controllerNamespace, rootCA, validator.AdmitSP)
	if err != nil {
		log.Fatalf("failed to initialize the webhook server: %s", err)
	}

	go s.Start()
	go admin.StartServer(*metricsAddr)

	<-stop
	log.Info("shutting down webhook server")
	if err := s.Shutdown(); err != nil {
		log.Error(err)
	}
}
