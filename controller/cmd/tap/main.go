package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/runconduit/conduit/controller/tap"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", ":8088", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9998", "address to serve scrapable metrics on")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config")
	tapPort := flag.Uint("tap-port", 4190, "proxy tap port to connect to")
	flag.Parse()

	log.SetLevel(log.DebugLevel) // TODO: make configurable
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	server, lis, err := tap.NewServer(*addr, *tapPort, *kubeConfigPath)
	if err != nil {
		log.Fatal(err.Error())
	}

	go func() {
		log.Println("starting gRPC server on", *addr)
		server.Serve(lis)
	}()

	go func() {
		fmt.Println("serving scrapable metrics on", *metricsAddr)
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(*metricsAddr, nil)
	}()

	<-stop

	log.Println("shutting down gRPC server on", *addr)
	server.GracefulStop()
}
