package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"

	"github.com/linkerd/linkerd2/controller/k8s"
	injector "github.com/linkerd/linkerd2/controller/proxy-injector"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
)

func main() {
	metricsAddr := flag.String("metrics-addr", ":9995", "address to serve scrapable metrics on")
	addr := flag.String("addr", ":8443", "address to serve on")
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig")
	controllerNamespace := flag.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	webhookServiceName := flag.String("webhook-service", "linkerd-proxy-injector.linkerd.io", "name of the admission webhook")
	noInitContainer := flag.Bool("no-init-container", false, "whether to use an init container or the linkerd-cni plugin")
	tlsEnabled := flag.Bool("tls-enabled", false, "whether the control plane was installed with TLS enabled")
	flags.ConfigureAndParse()

	stop := make(chan os.Signal, 1)
	defer close(stop)
	signal.Notify(stop, os.Interrupt, os.Kill)

	k8sClient, err := k8s.NewClientSet(*kubeconfig)
	if err != nil {
		log.Fatalf("failed to initialize Kubernetes client: %s", err)
	}

	rootCA, err := tls.GenerateRootCA("Proxy Injector Mutating Webhook Admission Controller CA")
	if err != nil {
		log.Fatalf("failed to create root CA: %s", err)
	}

	webhookConfig, err := injector.NewWebhookConfig(k8sClient, *controllerNamespace, *webhookServiceName, rootCA)
	if err != nil {
		log.Fatalf("failed to read the trust anchor file: %s", err)
	}

	mwc, err := webhookConfig.CreateOrUpdate()
	if err != nil {
		log.Fatalf("failed to create the mutating webhook configurations resource: %s", err)
	}
	log.Infof("created or updated mutating webhook configuration: %s", mwc.ObjectMeta.SelfLink)

	resources := &injector.WebhookResources{
		FileProxySpec:                k8sPkg.MountPathConfigProxySpec,
		FileProxyInitSpec:            k8sPkg.MountPathConfigProxyInitSpec,
		FileTLSTrustAnchorVolumeSpec: k8sPkg.MountPathTLSTrustAnchorVolumeSpec,
		FileTLSIdentityVolumeSpec:    k8sPkg.MountPathTLSIdentityVolumeSpec,
	}

	s, err := injector.NewWebhookServer(k8sClient, resources, *addr, *controllerNamespace, *noInitContainer, *tlsEnabled, rootCA)
	if err != nil {
		log.Fatalf("failed to initialize the webhook server: %s", err)
	}

	go func() {
		log.Infof("listening at %s", *addr)
		if err := s.ListenAndServeTLS("", ""); err != nil {
			if err == http.ErrServerClosed {
				return
			}
			log.Fatal(err)
		}
	}()
	go admin.StartServer(*metricsAddr)

	<-stop
	log.Info("shutting down webhook server")
	if err := s.Shutdown(); err != nil {
		log.Error(err)
	}
}
