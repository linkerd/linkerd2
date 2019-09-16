package publicapi

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/linkerd/linkerd2/controller/api/destination"
	"github.com/linkerd/linkerd2/controller/api/discovery"
	"github.com/linkerd/linkerd2/controller/api/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/flags"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
	promApi "github.com/prometheus/client_golang/api"
	log "github.com/sirupsen/logrus"
)

// Main executes the public-api subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("public-api", flag.ExitOnError)

	addr := cmd.String("addr", ":8085", "address to serve on")
	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	prometheusURL := cmd.String("prometheus-url", "http://127.0.0.1:9090", "prometheus url")
	metricsAddr := cmd.String("metrics-addr", ":9995", "address to serve scrapable metrics on")
	destinationAPIAddr := cmd.String("destination-addr", "127.0.0.1:8086", "address of destination service")
	controllerNamespace := cmd.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	ignoredNamespaces := cmd.String("ignore-namespaces", "kube-system", "comma separated list of namespaces to not list pods from")

	flags.ConfigureAndParse(cmd, args)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	discoveryClient, discoveryConn, err := discovery.NewClient(*destinationAPIAddr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer discoveryConn.Close()

	destinationClient, destinationConn, err := destination.NewClient(*destinationAPIAddr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer destinationConn.Close()

	k8sAPI, err := k8s.InitializeAPI(
		*kubeConfigPath,
		k8s.DS, k8s.Deploy, k8s.Job, k8s.NS, k8s.Pod, k8s.RC, k8s.RS, k8s.Svc, k8s.SS, k8s.SP, k8s.TS,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	prometheusClient, err := promApi.NewClient(promApi.Config{Address: *prometheusURL})
	if err != nil {
		log.Fatal(err.Error())
	}

	globalConfig, err := config.Global(pkgK8s.MountPathGlobalConfig)
	if err != nil {
		log.Fatal(err)
	}
	clusterDomain := globalConfig.GetClusterDomain()
	if clusterDomain == "" {
		clusterDomain = "cluster.local"
	}
	log.Info("Using cluster domain: ", clusterDomain)

	util.SetExporter("public")
	server := public.NewServer(
		*addr,
		prometheusClient,
		discoveryClient,
		destinationClient,
		k8sAPI,
		*controllerNamespace,
		clusterDomain,
		strings.Split(*ignoredNamespaces, ","),
	)

	k8sAPI.Sync() // blocks until caches are synced

	go func() {
		log.Infof("starting HTTP server on %+v", *addr)
		server.ListenAndServe()
	}()

	go admin.StartServer(*metricsAddr)

	<-stop

	log.Infof("shutting down HTTP server on %+v", *addr)
	server.Shutdown(context.Background())
}
