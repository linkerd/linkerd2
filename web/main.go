package main

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/linkerd/linkerd2/controller/api/public"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/flags"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/web/srv"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", ":8084", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9994", "address to serve scrapable metrics on")
	apiAddr := flag.String("api-addr", "127.0.0.1:8085", "address of the linkerd-controller-api service")
	grafanaAddr := flag.String("grafana-addr", "127.0.0.1:3000", "address of the linkerd-grafana service")
	templateDir := flag.String("template-dir", "templates", "directory to search for template files")
	staticDir := flag.String("static-dir", "app/dist", "directory to search for static files")
	reload := flag.Bool("reload", true, "reloading set to true or false")
	controllerNamespace := flag.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	flags.ConfigureAndParse()

	_, _, err := net.SplitHostPort(*apiAddr) // Verify apiAddr is of the form host:port.
	if err != nil {
		log.Fatalf("failed to parse API server address: %s", *apiAddr)
	}
	client, err := public.NewInternalClient(*controllerNamespace, *apiAddr)
	if err != nil {
		log.Fatalf("failed to construct client for API server URL %s", *apiAddr)
	}

	installConfig, err := config.Install(pkgK8s.MountPathInstallConfig)
	if err != nil {
		log.Warnf("failed to load uuid from install config: %s", err)
	}
	uuid := installConfig.GetUuid()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	server := srv.NewServer(*addr, *grafanaAddr, *templateDir, *staticDir, uuid, *controllerNamespace, *reload, client)

	go func() {
		log.Infof("starting HTTP server on %+v", *addr)
		server.ListenAndServe()
	}()

	go admin.StartServer(*metricsAddr)

	<-stop

	log.Infof("shutting down HTTP server on %+v", *addr)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}
