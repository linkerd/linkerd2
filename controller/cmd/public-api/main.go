package publicapi

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/controller/api/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/trace"
	log "github.com/sirupsen/logrus"
)

// Main executes the public-api subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("public-api", flag.ExitOnError)

	addr := cmd.String("addr", ":8085", "address to serve on")
	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	metricsAddr := cmd.String("metrics-addr", ":9995", "address to serve scrapable metrics on")
	controllerNamespace := cmd.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	clusterDomain := cmd.String("cluster-domain", "cluster.local", "kubernetes cluster domain")

	traceCollector := flags.AddTraceFlags(cmd)

	flags.ConfigureAndParse(cmd, args)
	ctx := context.Background()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.InitializeAPI(
		ctx,
		*kubeConfigPath,
		true,
		k8s.CJ, k8s.DS, k8s.Deploy, k8s.Job, k8s.NS, k8s.Pod, k8s.RC, k8s.RS, k8s.Svc, k8s.SS, k8s.SP, k8s.TS,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	log.Info("Using cluster domain: ", *clusterDomain)

	if *traceCollector != "" {
		if err := trace.InitializeTracing("linkerd-public-api", *traceCollector); err != nil {
			log.Warnf("failed to initialize tracing: %s", err)
		}
	}

	server := public.NewServer(
		*addr,
		k8sAPI,
		*controllerNamespace,
		*clusterDomain,
	)

	k8sAPI.Sync(nil) // blocks until caches are synced

	go func() {
		log.Infof("starting HTTP server on %+v", *addr)
		server.ListenAndServe()
	}()

	go admin.StartServer(*metricsAddr)

	<-stop

	log.Infof("shutting down HTTP server on %+v", *addr)
	server.Shutdown(ctx)
}
