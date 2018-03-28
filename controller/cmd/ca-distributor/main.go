package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/runconduit/conduit/controller/ca"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/pkg/version"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/informers"
)

const configMapName = "conduit-ca-bundle"

func main() {
	controllerNamespace := flag.String("controller-namespace", "conduit", "namespace in which Conduit is installed")
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

	clientSet, err := k8s.NewClientSet(*kubeConfigPath)
	if err != nil {
		log.Fatalf(err.Error())
	}

	sharedInformers := informers.NewSharedInformerFactory(clientSet, 10*time.Minute)
	controller := ca.NewCertificateController(
		clientSet,
		*controllerNamespace,
		sharedInformers.Core().V1().Pods(),
		sharedInformers.Core().V1().ConfigMaps(),
	)
	stopCh := make(chan struct{})

	sharedInformers.Start(stopCh)
	go controller.Run(stopCh)

	<-stop

	log.Info("shutting down")
	close(stopCh)
}
