package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/runconduit/conduit/controller/destination"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", ":8089", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9999", "address to serve scrapable metrics on")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config")
	flag.Parse()

	log.SetLevel(log.DebugLevel) // TODO: make configurable

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})

	server, lis, err := destination.NewServer(*addr, *kubeConfigPath, done)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		log.Infof("starting gRPC server on %s\n", *addr)
		server.Serve(lis)
	}()

	go func() {
		fmt.Println("serving scrapable metrics on", *metricsAddr)
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(*metricsAddr, nil)
	}()

	<-stop

	log.Infof("shutting down gRPC server on %s\n", *addr)
	done <- struct{}{}
	server.GracefulStop()
}
