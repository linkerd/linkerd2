package main

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/linkerd/linkerd2/controller/api/public"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/trace"
	"github.com/linkerd/linkerd2/web/srv"
	log "github.com/sirupsen/logrus"
)

func main() {
	cmd := flag.NewFlagSet("public-api", flag.ExitOnError)

	addr := cmd.String("addr", ":8084", "address to serve on")
	metricsAddr := cmd.String("metrics-addr", ":9994", "address to serve scrapable metrics on")
	apiAddr := cmd.String("api-addr", "127.0.0.1:8085", "address of the linkerd-controller-api service")
	grafanaAddr := cmd.String("grafana-addr", "127.0.0.1:3000", "address of the linkerd-grafana service")
	templateDir := cmd.String("template-dir", "templates", "directory to search for template files")
	staticDir := cmd.String("static-dir", "app/dist", "directory to search for static files")
	reload := cmd.Bool("reload", true, "reloading set to true or false")
	controllerNamespace := cmd.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	enforcedHost := cmd.String("enforced-host", "", "regexp describing the allowed values for the Host header; protects from DNS-rebinding attacks")
	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")

	traceCollector := flags.AddTraceFlags(cmd)

	flags.ConfigureAndParse(cmd, os.Args[1:])

	_, _, err := net.SplitHostPort(*apiAddr) // Verify apiAddr is of the form host:port.
	if err != nil {
		log.Fatalf("failed to parse API server address: %s", *apiAddr)
	}
	client, err := public.NewInternalClient(*controllerNamespace, *apiAddr)
	if err != nil {
		log.Fatalf("failed to construct client for API server URL %s", *apiAddr)
	}

	globalConfig, err := config.Global(pkgK8s.MountPathGlobalConfig)
	clusterDomain := globalConfig.GetClusterDomain()
	if err != nil || clusterDomain == "" {
		clusterDomain = "cluster.local"
		log.Warnf("failed to load cluster domain from global config: [%s] (falling back to %s)", err, clusterDomain)
	}

	k8sAPI, err := k8s.NewAPI(*kubeConfigPath, "", "", 0)
	if err != nil {
		log.Fatalf("failed to construct Kubernetes API client: [%s]", err)
	}

	installConfig, err := config.Install(pkgK8s.MountPathInstallConfig)
	if err != nil {
		log.Warnf("failed to load uuid from install config: [%s] (disregard warning if running in development mode)", err)
	}
	uuid := installConfig.GetUuid()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	if *traceCollector != "" {
		if err := trace.InitializeTracing("linkerd-web", *traceCollector); err != nil {
			log.Warnf("failed to initialize tracing: %s", err)
		}
	}

	reHost, err := regexp.Compile(*enforcedHost)
	if err != nil {
		log.Fatalf("invalid --enforced-host parameter: %s", err)
	}

	server := srv.NewServer(*addr, *grafanaAddr, *templateDir, *staticDir, uuid,
		*controllerNamespace, clusterDomain, *reload, reHost, client, k8sAPI)

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
