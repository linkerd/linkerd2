package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/controller/ca"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
)

func main() {
	metricsAddr := flag.String("metrics-addr", ":9997", "address to serve scrapable metrics on")
	controllerNamespace := flag.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config")
	logLevel := flag.String("log-level", log.InfoLevel.String(), "log level, must be one of: panic, fatal, error, warn, info, debug")
	printVersion := version.VersionFlag()
	flag.Parse()

	// set global log level
	level, err := log.ParseLevel(*logLevel)
	if err != nil {
		log.Fatalf("invalid log-level: %s", *logLevel)
	}
	log.SetLevel(level)

	version.MaybePrintVersionAndExit(*printVersion)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sClient, err := k8s.NewClientSet(*kubeConfigPath)
	if err != nil {
		log.Fatal(err.Error())
	}
	k8sAPI := k8s.NewAPI(
		k8sClient,
		k8s.Pod,
		k8s.RS,
	)

	controller, err := ca.NewCertificateController(*controllerNamespace, k8sAPI)
	if err != nil {
		log.Fatalf("Failed to create CertificateController: %v", err)
	}

	stopCh := make(chan struct{})
	ready := make(chan struct{})

	go k8sAPI.Sync(ready)

	go func() {
		log.Info("starting CA")
		controller.Run(ready, stopCh)
	}()

	go admin.StartServer(*metricsAddr, ready)

	<-stop

	log.Info("shutting down")
	close(stopCh)
}
