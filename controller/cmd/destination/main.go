package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/runconduit/conduit/controller/destination"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/pkg/admin"
	"github.com/runconduit/conduit/pkg/version"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8089", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9999", "address to serve scrapable metrics on")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config")
	k8sDNSZone := flag.String("kubernetes-dns-zone", "", "The DNS suffix for the local Kubernetes zone.")
	logLevel := flag.String("log-level", log.InfoLevel.String(), "log level, must be one of: panic, fatal, error, warn, info, debug")
	enableTLS := flag.Bool("enable-tls", false, "Enable TLS connections among pods in the service mesh")
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
		k8s.Endpoint,
		k8s.Pod,
		k8s.Svc,
	)

	done := make(chan struct{})
	ready := make(chan struct{})

	server, lis, err := destination.NewServer(*addr, *k8sDNSZone, *enableTLS, k8sAPI, done)
	if err != nil {
		log.Fatal(err)
	}

	go k8sAPI.Sync(ready)

	go func() {
		log.Infof("starting gRPC server on %s", *addr)
		server.Serve(lis)
	}()

	go admin.StartServer(*metricsAddr, ready)

	<-stop

	log.Infof("shutting down gRPC server on %s", *addr)
	close(done)
	server.GracefulStop()
}
