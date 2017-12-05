package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/runconduit/conduit/controller/api/proxy"
	"github.com/runconduit/conduit/controller/destination"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", ":8086", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9996", "address to serve scrapable metrics on")
	telemetryAddr := flag.String("telemetry-addr", ":8087", "address of telemetry service")
	destinationAddr := flag.String("destination-addr", ":8089", "address of destination service")
	flag.Parse()

	log.SetLevel(log.DebugLevel) // TODO: make configurable

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	telemetryClient, conn, err := proxy.NewTelemetryClient(*telemetryAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	destinationClient, conn, err := destination.NewClient(*destinationAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	server, lis, err := proxy.NewServer(*addr, telemetryClient, destinationClient)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		log.Infof("starting gRPC server on %s", *addr)
		server.Serve(lis)
	}()

	go func() {
		log.Infof("serving scrapable metrics on %s", *metricsAddr)
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(*metricsAddr, nil)
	}()

	<-stop

	log.Infof("shutting down gRPC server on %s", *addr)
	server.GracefulStop()
}
