package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/runconduit/conduit/controller/telemetry"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", ":8087", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9997", "address to serve scrapable metrics on")
	prometheusUrl := flag.String("prometheus-url", "http://127.0.0.1:9090", "prometheus url")
	ignoredNamespaces := flag.String("ignore-namespaces", "", "comma separated list of namespaces to not list pods from")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config")
	flag.Parse()

	log.SetLevel(log.DebugLevel) // TODO: make configurable
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	server, lis, err := telemetry.NewServer(*addr, *prometheusUrl, strings.Split(*ignoredNamespaces, ","), *kubeConfigPath)
	if err != nil {
		log.Fatal(err.Error())
	}

	go func() {
		log.Println("starting gRPC server on", *addr)
		server.Serve(lis)
	}()

	go func() {
		log.Info("serving scrapable metrics on", *metricsAddr)
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(*metricsAddr, nil)
	}()

	<-stop

	log.Println("shutting down gRPC server on", *addr)
	server.GracefulStop()
}
