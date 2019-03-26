package webhook

import (
	"flag"
	"os"
	"os/signal"
	"strconv"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
)

// Launch sets up and starts the webhook and metrics servers
func Launch(config *Config) {
	p := strconv.FormatUint(uint64(config.MetricsPort), 10)
	metricsAddr := flag.String("metrics-addr", ":"+p, "address to serve scrapable metrics on")
	addr := flag.String("addr", ":8443", "address to serve on")
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig")
	controllerNamespace := flag.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	flags.ConfigureAndParse()

	stop := make(chan os.Signal, 1)
	defer close(stop)
	signal.Notify(stop, os.Interrupt, os.Kill)

	k8sClient, err := k8s.NewClientSet(*kubeconfig)
	if err != nil {
		log.Fatalf("failed to initialize Kubernetes client: %s", err)
	}

	rootCA, err := tls.GenerateRootCAWithDefaults(config.WebhookServiceName)
	if err != nil {
		log.Fatalf("failed to create root CA: %s", err)
	}

	config.client = k8sClient
	config.controllerNamespace = *controllerNamespace
	config.rootCA = rootCA

	selfLink, err := config.Create()
	if err != nil {
		log.Fatalf("failed to create the webhook configurations resource: %s", err)
	}
	log.Infof("created webhook configuration: %s", selfLink)

	s, err := NewServer(k8sClient, *addr, config.WebhookServiceName, *controllerNamespace, rootCA, config.Handler)
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
