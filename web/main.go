package main

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/runconduit/conduit/controller/api/public"
	"github.com/runconduit/conduit/pkg/admin"
	"github.com/runconduit/conduit/pkg/version"
	"github.com/runconduit/conduit/web/srv"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", ":8084", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9994", "address to serve scrapable metrics on")
	kubernetesApiHost := flag.String("api-addr", ":8085", "host address of kubernetes public api")
	templateDir := flag.String("template-dir", "templates", "directory to search for template files")
	staticDir := flag.String("static-dir", "app/dist", "directory to search for static files")
	uuid := flag.String("uuid", "", "unqiue Conduit install id")
	reload := flag.Bool("reload", true, "reloading set to true or false")
	webpackDevServer := flag.String("webpack-dev-server", "", "use webpack to serve static assets; frontend will use this instead of static-dir")
	logLevel := flag.String("log-level", log.InfoLevel.String(), "log level, must be one of: panic, fatal, error, warn, info, debug")
	controllerNamespace := flag.String("controller-namespace", "", "the k8s namespace in which Conduit is running")
	printVersion := version.VersionFlag()
	flag.Parse()

	// set global log level
	level, err := log.ParseLevel(*logLevel)
	if err != nil {
		log.Fatalf("invalid log-level: %s", *logLevel)
	}
	log.SetLevel(level)

	version.MaybePrintVersionAndExit(*printVersion)

	_, _, err = net.SplitHostPort(*kubernetesApiHost) // Verify kubernetesApiHost is of the form host:port.
	if err != nil {
		log.Fatalf("failed to parse API server address: %s", *kubernetesApiHost)
	}
	client, err := public.NewInternalClient(*kubernetesApiHost)
	if err != nil {
		log.Fatalf("failed to construct client for API server URL %s", *kubernetesApiHost)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	server := srv.NewServer(*addr, *templateDir, *staticDir, *uuid, *controllerNamespace, *webpackDevServer, *reload, client)

	go func() {
		log.Infof("starting HTTP server on %+v", *addr)
		server.ListenAndServe()
	}()

	go admin.StartServer(*metricsAddr, nil)

	<-stop

	log.Infof("shutting down HTTP server on %+v", *addr)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}
