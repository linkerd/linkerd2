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

	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/trace"
	"github.com/linkerd/linkerd2/viz/metrics-api/client"
	"github.com/linkerd/linkerd2/web/srv"
	log "github.com/sirupsen/logrus"
)

func main() {
	cmd := flag.NewFlagSet("public-api", flag.ExitOnError)

	addr := cmd.String("addr", ":8084", "address to serve on")
	metricsAddr := cmd.String("metrics-addr", ":9994", "address to serve scrapable metrics on")
	vizAPIAddr := cmd.String("linkerd-metrics-api-addr", "127.0.0.1:8085", "address of the linkerd-metrics-api service")
	grafanaAddr := cmd.String("grafana-addr", "", "address of the linkerd-grafana service")
	jaegerAddr := cmd.String("jaeger-addr", "", "address of the jaeger service")
	templateDir := cmd.String("template-dir", "templates", "directory to search for template files")
	staticDir := cmd.String("static-dir", "app/dist", "directory to search for static files")
	reload := cmd.Bool("reload", true, "reloading set to true or false")
	controllerNamespace := cmd.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	vizNamespace := cmd.String("viz-namespace", "linkerd", "namespace in which Linkerd viz is installed")
	enforcedHost := cmd.String("enforced-host", "", "regexp describing the allowed values for the Host header; protects from DNS-rebinding attacks")
	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	clusterDomain := cmd.String("cluster-domain", "", "kubernetes cluster domain")

	traceCollector := flags.AddTraceFlags(cmd)

	flags.ConfigureAndParse(cmd, os.Args[1:])
	ctx := context.Background()

	_, _, err := net.SplitHostPort(*vizAPIAddr) // Verify vizAPIAddr is of the form host:port.
	if err != nil {
		log.Fatalf("failed to parse metrics API server address: %s", *vizAPIAddr)
	}
	client, err := client.NewInternalClient(*vizNamespace, *vizAPIAddr)
	if err != nil {
		log.Fatalf("failed to construct client for viz API server URL %s", *vizAPIAddr)
	}

	if *clusterDomain == "" {
		*clusterDomain = "cluster.local"
		log.Warnf("expected cluster domain through args (falling back to %s)", *clusterDomain)
	}

	k8sAPI, err := k8s.NewAPI(*kubeConfigPath, "", "", []string{}, 0)
	if err != nil {
		log.Fatalf("failed to construct Kubernetes API client: [%s]", err)
	}

	// Setup health checker
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.KubernetesVersionChecks,
		healthcheck.LinkerdConfigChecks,
		healthcheck.LinkerdControlPlaneExistenceChecks,
		healthcheck.LinkerdVersionChecks,
		healthcheck.LinkerdControlPlaneVersionChecks,
	}
	hc := healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		ControlPlaneNamespace: *controllerNamespace,
		KubeConfig:            *kubeConfigPath,
	})

	uuid, version := getUUIDAndVersion(ctx, k8sAPI, *controllerNamespace)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	if *traceCollector != "" {
		if err := trace.InitializeTracing("web", *traceCollector); err != nil {
			log.Warnf("failed to initialize tracing: %s", err)
		}
	}

	reHost, err := regexp.Compile(*enforcedHost)
	if err != nil {
		log.Fatalf("invalid --enforced-host parameter: %s", err)
	}

	server := srv.NewServer(*addr, *grafanaAddr, *jaegerAddr, *templateDir, *staticDir, uuid, version,
		*controllerNamespace, *clusterDomain, *reload, reHost, client, k8sAPI, hc)

	go func() {
		log.Infof("starting HTTP server on %+v", *addr)
		server.ListenAndServe()
	}()

	go admin.StartServer(*metricsAddr)

	<-stop

	log.Infof("shutting down HTTP server on %+v", *addr)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}

func getUUIDAndVersion(ctx context.Context, k8sAPI *k8s.KubernetesAPI, controllerNamespace string) (string, string) {
	var uuid string
	var version string

	cm, _, err := healthcheck.FetchLinkerdConfigMap(ctx, k8sAPI, controllerNamespace)
	if err != nil {
		log.Errorf("Failed to fetch linkerd-config: %s", err)
	} else {
		uuid = string(cm.GetUID())

		values, err := linkerd2.ValuesFromConfigMap(cm)
		if err != nil {
			log.Errorf("failed to load values from linkerd-config: %s", err)
		} else {
			version = values.LinkerdVersion
		}
	}

	return uuid, version
}
