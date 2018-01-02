package main

import (
	"context"
	"flag"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/runconduit/conduit/pkg/conduit"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/runconduit/conduit/web/srv"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", ":8084", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9994", "address to serve scrapable metrics on")
	apiAddr := flag.String("api-addr", ":8085", "address of public api")
	templateDir := flag.String("template-dir", "templates", "directory to search for template files")
	staticDir := flag.String("static-dir", "app/dist", "directory to search for static files")
	uuid := flag.String("uuid", "", "unqiue Conduit install id")
	reload := flag.Bool("reload", true, "reloading set to true or false")
	logLevel := flag.String("log-level", "info", "log level, must be one of: panic, fatal, error, warn, info, debug")
	webpackDevServer := flag.String("webpack-dev-server", "", "use webpack to serve static assets; frontend will use this instead of static-dir")

	flag.Parse()

	// set global log level
	level, err := log.ParseLevel(*logLevel)
	if err != nil {
		log.Fatalf("invalid log-level: %s", *logLevel)
	}
	log.SetLevel(level)

	_, _, err = net.SplitHostPort(*apiAddr) // Verify apiAddr is of the form host:port.
	if err != nil {
		log.Fatalf("failed to parse API server address: %s", apiAddr)
	}
	apiURL := &url.URL{
		Scheme: "http",
		Host:   *apiAddr,
		Path:   "/",
	}
	apiConfig := conduit.Config{
		ServerURL: apiURL,
	}
	client, err := conduit.NewInternalClient(&apiConfig)
	if err != nil {
		log.Fatalf("failed to construct client for API server URL %s", apiURL)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	server := srv.NewServer(*addr, *templateDir, *staticDir, *uuid, *webpackDevServer, *reload, client)

	go func() {
		log.Info("starting HTTP server on", *addr)
		server.ListenAndServe()
	}()

	go func() {
		log.Info("serving scrapable metrics on", *metricsAddr)
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(*metricsAddr, nil)
	}()

	<-stop

	log.Info("shutting down HTTP server on", *addr)
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	server.Shutdown(ctx)
}
