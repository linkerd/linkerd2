package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/runconduit/conduit/controller/api/public"
	"github.com/runconduit/conduit/controller/tap"
	"github.com/runconduit/conduit/controller/telemetry"
	"github.com/runconduit/conduit/pkg/version"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", ":8085", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9995", "address to serve scrapable metrics on")
	telemetryAddr := flag.String("telemetry-addr", ":8087", "address of telemetry service")
	tapAddr := flag.String("tap-addr", ":8088", "address of tap service")
	controllerNamespace := flag.String("controller-namespace", "conduit", "namespace in which Conduit is installed")
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

	telemetryClient, telemetryConn, err := telemetry.NewClient(*telemetryAddr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer telemetryConn.Close()

	tapClient, tapConn, err := tap.NewClient(*tapAddr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer tapConn.Close()

	server := public.NewServer(*addr, telemetryClient, tapClient, *controllerNamespace)

	go func() {
		log.Infof("starting HTTP server on %+v", *addr)
		server.ListenAndServe()
	}()

	go func() {
		log.Infof("serving scrapable metrics on %+v", *metricsAddr)
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(*metricsAddr, nil)
	}()

	<-stop

	log.Infof("shutting down HTTP server on %+v", *addr)
	server.Shutdown(context.Background())
}
